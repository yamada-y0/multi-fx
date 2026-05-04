package runner

import (
	"context"
	"fmt"
	"log"

	"github.com/shopspring/decimal"
	"github.com/yamada/multi-fx/internal/agent"
	"github.com/yamada/multi-fx/internal/commander"
	intmarket "github.com/yamada/multi-fx/internal/market"
	"github.com/yamada/multi-fx/internal/order"
	"github.com/yamada/multi-fx/internal/pool"
	"github.com/yamada/multi-fx/internal/rule"
	"github.com/yamada/multi-fx/pkg/currency"
)

// AgentEntry は Runner が管理する SubPool + Strategy のペア
type AgentEntry struct {
	SubPool  pool.SubPool
	Strategy agent.Strategy
}

// Runner は毎ティックの決定論的な処理フローを実行する
// Commander とは独立しており、Directive チャネル経由で疎結合に連携する
type Runner struct {
	agents      []AgentEntry
	agg         order.Aggregator
	engine      *rule.RuleEngine
	provider    *intmarket.Provider
	pair        currency.Pair
	wakeupStore WakeupStore
	directives  <-chan commander.Directive // nil でも動く（バックテスト時など）
}

// New は Runner を生成する。directives は nil 可（Commander なしで動く）
func New(
	agents []AgentEntry,
	agg order.Aggregator,
	engine *rule.RuleEngine,
	provider *intmarket.Provider,
	pair currency.Pair,
	wakeupStore WakeupStore,
	directives <-chan commander.Directive,
) *Runner {
	return &Runner{
		agents:      agents,
		agg:         agg,
		engine:      engine,
		provider:    provider,
		pair:        pair,
		wakeupStore: wakeupStore,
		directives:  directives,
	}
}

// Tick は1ティック分の処理を実行する。
// FetchRate → Directive適用 → RuleEngine評価 → WakeupCondition評価 → OnTick → SubmitOrder → SyncFills
// 戻り値が false のとき、全 SubPool が Active でなくなったことを示す（ループ終了の目安）
func (r *Runner) Tick(ctx context.Context, rate currency.Rate) (bool, error) {
	r.applyDirectives(ctx)

	rates := map[currency.Pair]decimal.Decimal{rate.Pair: rate.Bid}

	for _, e := range r.agents {
		e.SubPool.OnRate(rate)

		snap := e.SubPool.Snapshot()
		if snap.State != pool.StateActive {
			continue
		}

		// フロアルール評価
		if r.engine.Evaluate(snap) == rule.ActionSuspend {
			log.Printf("[runner] フロアルール発動: subpool=%s equity=%s < initial=%s",
				snap.ID, snap.EquityBalance().StringFixed(3), snap.InitialBalance.StringFixed(3))
			if err := e.SubPool.Suspend(); err != nil {
				return false, fmt.Errorf("runner: suspend subpool %s: %w", snap.ID, err)
			}
			continue
		}

		// ウェイクアップ条件評価: 条件が残っていてまだ満たされていなければ OnTick をスキップ
		skip, err := r.shouldSkip(ctx, e.SubPool.ID(), rate, rates)
		if err != nil {
			return false, fmt.Errorf("runner: wakeup check: %w", err)
		}
		if skip {
			continue
		}

		// MarketContext 取得
		mkt, err := r.provider.Fetch(r.pair)
		if err != nil {
			return false, fmt.Errorf("runner: fetch market: %w", err)
		}

		// OnTick → 発注
		result, err := e.Strategy.OnTick(ctx, snap, mkt)
		if err != nil {
			return false, fmt.Errorf("runner: strategy %s ontick: %w", e.Strategy.Name(), err)
		}

		for _, req := range result.Orders {
			if err := r.agg.SubmitOrder(ctx, req); err != nil {
				log.Printf("[runner] SubmitOrder error: subpool=%s err=%v", snap.ID, err)
			}
		}

		// ウェイクアップ条件を更新
		if err := r.updateWakeup(ctx, e.SubPool.ID(), result.Wakeup); err != nil {
			return false, fmt.Errorf("runner: update wakeup: %w", err)
		}
	}

	if err := r.agg.SyncFills(ctx); err != nil {
		return false, fmt.Errorf("runner: sync fills: %w", err)
	}

	for _, e := range r.agents {
		if e.SubPool.Snapshot().State == pool.StateActive {
			return true, nil
		}
	}
	return false, nil
}

// shouldSkip はウェイクアップ条件が残っていてまだ満たされていなければ true を返す
// 条件を満たした場合はストアから削除して false を返す（= 今回は起動する）
func (r *Runner) shouldSkip(ctx context.Context, id pool.SubPoolID, rate currency.Rate, rates map[currency.Pair]decimal.Decimal) (bool, error) {
	cond, ok, err := r.wakeupStore.Load(ctx, id)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	if cond.IsMet(rate.Timestamp, rates) {
		return false, r.wakeupStore.Delete(ctx, id)
	}
	return true, nil
}

// updateWakeup はウェイクアップ条件を保存または削除する
func (r *Runner) updateWakeup(ctx context.Context, id pool.SubPoolID, cond *agent.WakeupCondition) error {
	if cond == nil {
		return r.wakeupStore.Delete(ctx, id)
	}
	return r.wakeupStore.Save(ctx, id, *cond)
}

// applyDirectives は Directive チャネルから溜まっている指示を全て処理する（ノンブロッキング）
func (r *Runner) applyDirectives(ctx context.Context) {
	if r.directives == nil {
		return
	}
	for {
		select {
		case d := <-r.directives:
			r.applyDirective(ctx, d)
		default:
			return
		}
	}
}

func (r *Runner) applyDirective(ctx context.Context, d commander.Directive) {
	for _, e := range r.agents {
		if e.SubPool.ID() != d.TargetID {
			continue
		}
		switch d.Action {
		case commander.ActionSuspendSubPool:
			if err := e.SubPool.Suspend(); err != nil {
				log.Printf("[runner] directive suspend error: %v", err)
			}
		case commander.ActionSendInstruction:
			if err := e.Strategy.OnInstruction(ctx, d.Instruction); err != nil {
				log.Printf("[runner] directive instruction error: %v", err)
			}
		}
	}
}
