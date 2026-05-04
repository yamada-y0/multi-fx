package agent

import (
	"context"
	"fmt"

	"github.com/shopspring/decimal"
	"github.com/yamada/multi-fx/internal/pool"
	"github.com/yamada/multi-fx/pkg/currency"
)

// WakeupStore は Agent ごとのウェイクアップ条件を永続化する
type WakeupStore interface {
	Save(ctx context.Context, id pool.SubPoolID, cond WakeupCondition) error
	Load(ctx context.Context, id pool.SubPoolID) (WakeupCondition, bool, error)
	Delete(ctx context.Context, id pool.SubPoolID) error
}

// baseAgent は ShouldWakeup・ApplyInstruction など Agent 実装間で共通の振る舞いを提供する
// LLMAgent・DummyAgent はこれを埋め込んで使う
type baseAgent struct {
	subPool     pool.SubPool
	wakeupStore WakeupStore
}

func (a *baseAgent) ID() pool.SubPoolID    { return a.subPool.ID() }
func (a *baseAgent) SubPool() pool.SubPool { return a.subPool }

// ShouldWakeup はウェイクアップ条件を評価する
// 条件がなければ常に true（毎ティック起動）
// 条件を満たした場合はストアから削除して true を返す
func (a *baseAgent) ShouldWakeup(ctx context.Context, rate currency.Rate) (bool, error) {
	cond, ok, err := a.wakeupStore.Load(ctx, a.subPool.ID())
	if err != nil {
		return false, fmt.Errorf("agent: load wakeup: %w", err)
	}
	if !ok {
		return true, nil
	}
	rates := map[currency.Pair]decimal.Decimal{rate.Pair: rate.Bid}
	if cond.IsMet(rate.Timestamp, rates) {
		return true, a.wakeupStore.Delete(ctx, a.subPool.ID())
	}
	return false, nil
}

// SetWakeup はウェイクアップ条件を保存または削除する
func (a *baseAgent) SetWakeup(ctx context.Context, cond *WakeupCondition) error {
	if cond == nil {
		return a.wakeupStore.Delete(ctx, a.subPool.ID())
	}
	return a.wakeupStore.Save(ctx, a.subPool.ID(), *cond)
}
