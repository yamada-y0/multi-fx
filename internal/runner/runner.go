package runner

import (
	"context"
	"fmt"
	"log"

	"github.com/yamada/multi-fx/internal/agent"
	intmarket "github.com/yamada/multi-fx/internal/market"
	"github.com/yamada/multi-fx/internal/order"
	"github.com/yamada/multi-fx/internal/pool"
	"github.com/yamada/multi-fx/internal/rule"
	"github.com/yamada/multi-fx/pkg/currency"
)

// Runner は毎ティックの決定論的な処理フローを実行する
// Commander とは独立しており、Instruction チャネル経由で疎結合に連携する
type Runner struct {
	agents     []agent.Agent
	agg        order.Aggregator
	engine     *rule.RuleEngine
	provider   *intmarket.Provider
	pair       currency.Pair
	instructions <-chan agent.Instruction // nil でも動く（バックテスト時など）
}

// New は Runner を生成する。instructions は nil 可（Commander なしで動く）
func New(
	agents []agent.Agent,
	agg order.Aggregator,
	engine *rule.RuleEngine,
	provider *intmarket.Provider,
	pair currency.Pair,
	instructions <-chan agent.Instruction,
) *Runner {
	return &Runner{
		agents:       agents,
		agg:          agg,
		engine:       engine,
		provider:     provider,
		pair:         pair,
		instructions: instructions,
	}
}

// Tick は1ティック分の処理を実行する。
// Instruction適用 → OnRate → RuleEngine評価 → ShouldWakeup → Tick → SyncFills
// 戻り値が false のとき、全 SubPool が Active でなくなったことを示す（ループ終了の目安）
func (r *Runner) Tick(ctx context.Context, rate currency.Rate) (bool, error) {
	r.applyInstructions(ctx)

	for _, a := range r.agents {
		sp := a.SubPool()
		sp.OnRate(rate)

		snap := sp.Snapshot()
		if snap.State != pool.StateActive {
			continue
		}

		// フロアルール評価
		if r.engine.Evaluate(snap) == rule.ActionSuspend {
			log.Printf("[runner] フロアルール発動: subpool=%s equity=%s < initial=%s",
				snap.ID, snap.EquityBalance().StringFixed(3), snap.InitialBalance.StringFixed(3))
			if err := sp.Suspend(); err != nil {
				return false, fmt.Errorf("runner: suspend subpool %s: %w", snap.ID, err)
			}
			continue
		}

		// 起動判断は Agent に委譲
		wakeup, err := a.ShouldWakeup(ctx, rate)
		if err != nil {
			return false, fmt.Errorf("runner: should wakeup %s: %w", snap.ID, err)
		}
		if !wakeup {
			continue
		}

		mkt, err := r.provider.Fetch(r.pair)
		if err != nil {
			return false, fmt.Errorf("runner: fetch market: %w", err)
		}

		reqs, err := a.Tick(ctx, mkt)
		if err != nil {
			return false, fmt.Errorf("runner: agent tick %s: %w", snap.ID, err)
		}

		for _, req := range reqs {
			if err := r.agg.SubmitOrder(ctx, req); err != nil {
				log.Printf("[runner] SubmitOrder error: subpool=%s err=%v", snap.ID, err)
			}
		}
	}

	if err := r.agg.SyncFills(ctx); err != nil {
		return false, fmt.Errorf("runner: sync fills: %w", err)
	}

	for _, a := range r.agents {
		if a.SubPool().Snapshot().State == pool.StateActive {
			return true, nil
		}
	}
	return false, nil
}

// applyInstructions は Instruction チャネルから溜まっている指令を全て処理する（ノンブロッキング）
func (r *Runner) applyInstructions(ctx context.Context) {
	if r.instructions == nil {
		return
	}
	for {
		select {
		case inst := <-r.instructions:
			r.applyInstruction(ctx, inst)
		default:
			return
		}
	}
}

func (r *Runner) applyInstruction(ctx context.Context, inst agent.Instruction) {
	for _, a := range r.agents {
		if err := a.ApplyInstruction(ctx, inst); err != nil {
			log.Printf("[runner] instruction error: %v", err)
		}
	}
}
