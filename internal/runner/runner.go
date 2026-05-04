package runner

import (
	"context"
	"fmt"
	"log"

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
	agents     []AgentEntry
	agg        order.Aggregator
	engine     *rule.RuleEngine
	provider   *intmarket.Provider
	pair       currency.Pair
	directives <-chan commander.Directive // nil でも動く（バックテスト時など）
}

// New は Runner を生成する。directives は nil 可（Commander なしで動く）
func New(
	agents []AgentEntry,
	agg order.Aggregator,
	engine *rule.RuleEngine,
	provider *intmarket.Provider,
	pair currency.Pair,
	directives <-chan commander.Directive,
) *Runner {
	return &Runner{
		agents:     agents,
		agg:        agg,
		engine:     engine,
		provider:   provider,
		pair:       pair,
		directives: directives,
	}
}

// Tick は1ティック分の処理を実行する。
// FetchRate → Directive適用 → RuleEngine評価 → OnTick → SubmitOrder → SyncFills
// 戻り値が false のとき、全 SubPool が Active でなくなったことを示す（ループ終了の目安）
func (r *Runner) Tick(ctx context.Context, rate currency.Rate) (bool, error) {
	// Commander からの Directive があれば先に適用
	r.applyDirectives(ctx)

	// 各 AgentEntry を処理
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

		// MarketContext 取得
		mkt, err := r.provider.Fetch(r.pair)
		if err != nil {
			return false, fmt.Errorf("runner: fetch market: %w", err)
		}

		// OnTick → 発注
		reqs, err := e.Strategy.OnTick(ctx, snap, mkt)
		if err != nil {
			return false, fmt.Errorf("runner: strategy %s ontick: %w", e.Strategy.Name(), err)
		}
		for _, req := range reqs {
			if err := r.agg.SubmitOrder(ctx, req); err != nil {
				log.Printf("[runner] SubmitOrder error: subpool=%s err=%v", snap.ID, err)
			}
		}
	}

	// 全 Agent の発注後にまとめて SyncFills
	if err := r.agg.SyncFills(ctx); err != nil {
		return false, fmt.Errorf("runner: sync fills: %w", err)
	}

	// 全 SubPool が Active でなければ false
	for _, e := range r.agents {
		if e.SubPool.Snapshot().State == pool.StateActive {
			return true, nil
		}
	}
	return false, nil
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
