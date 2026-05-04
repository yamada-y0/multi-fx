package agent

import (
	"context"
	"fmt"

	"github.com/shopspring/decimal"
	"github.com/yamada/multi-fx/internal/pool"
	"github.com/yamada/multi-fx/pkg/currency"
	"github.com/yamada/multi-fx/pkg/market"
)

// WakeupStore は Agent ごとのウェイクアップ条件を永続化する
// Runner ではなく Agent が直接保持することで、起動判断の責務を Agent に閉じる
type WakeupStore interface {
	Save(ctx context.Context, id pool.SubPoolID, cond WakeupCondition) error
	Load(ctx context.Context, id pool.SubPoolID) (WakeupCondition, bool, error)
	Delete(ctx context.Context, id pool.SubPoolID) error
}

type agentImpl struct {
	subPool     pool.SubPool
	strategy    Strategy
	wakeupStore WakeupStore
}

// NewAgent は Agent を生成する
func NewAgent(subPool pool.SubPool, strategy Strategy, wakeupStore WakeupStore) Agent {
	return &agentImpl{
		subPool:     subPool,
		strategy:    strategy,
		wakeupStore: wakeupStore,
	}
}

func (a *agentImpl) ID() pool.SubPoolID { return a.subPool.ID() }

func (a *agentImpl) SubPool() pool.SubPool { return a.subPool }

// ShouldWakeup はウェイクアップ条件を評価する
// 条件がなければ常に true（毎ティック起動）
// 条件を満たした場合はストアから削除して true を返す
func (a *agentImpl) ShouldWakeup(ctx context.Context, rate currency.Rate) (bool, error) {
	cond, ok, err := a.wakeupStore.Load(ctx, a.subPool.ID())
	if err != nil {
		return false, fmt.Errorf("agent: load wakeup: %w", err)
	}
	if !ok {
		return true, nil
	}

	rates := map[currency.Pair]decimal.Decimal{rate.Pair: rate.Bid}
	if cond.IsMet(rate.Timestamp, rates) {
		if err := a.wakeupStore.Delete(ctx, a.subPool.ID()); err != nil {
			return false, fmt.Errorf("agent: delete wakeup: %w", err)
		}
		return true, nil
	}
	return false, nil
}

// Tick は Strategy.OnTick を呼び出し、ウェイクアップ条件を更新する
func (a *agentImpl) Tick(ctx context.Context, mkt market.MarketContext) ([]pool.OrderRequest, error) {
	snap := a.subPool.Snapshot()
	result, err := a.strategy.OnTick(ctx, snap, mkt)
	if err != nil {
		return nil, fmt.Errorf("agent %s: ontick: %w", a.strategy.Name(), err)
	}

	if err := a.setWakeup(ctx, result.Wakeup); err != nil {
		return nil, fmt.Errorf("agent %s: set wakeup: %w", a.strategy.Name(), err)
	}

	return result.Orders, nil
}

func (a *agentImpl) OnInstruction(ctx context.Context, instruction string) error {
	return a.strategy.OnInstruction(ctx, instruction)
}

func (a *agentImpl) setWakeup(ctx context.Context, cond *WakeupCondition) error {
	if cond == nil {
		return a.wakeupStore.Delete(ctx, a.subPool.ID())
	}
	return a.wakeupStore.Save(ctx, a.subPool.ID(), *cond)
}
