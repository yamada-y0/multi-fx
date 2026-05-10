package tick

import (
	"context"
	"fmt"

	"github.com/shopspring/decimal"
	"github.com/yamada/fxd/internal/agent"
	"github.com/yamada/fxd/internal/broker"
	"github.com/yamada/fxd/internal/store"
	"github.com/yamada/fxd/pkg/currency"
)

// Result は1ティック分の処理結果
type Result struct {
	ShouldWakeup bool
	FillCount    int
}

// Ticker は1ティック分のサイクルを実行する
type Ticker struct {
	broker      broker.TradingBroker
	wakeupStore agent.WakeupStore
	store       store.StateStore
}

func New(b broker.TradingBroker, wakeupStore agent.WakeupStore, st store.StateStore) *Ticker {
	return &Ticker{broker: b, wakeupStore: wakeupStore, store: st}
}

// Tick は1ティック分の処理を実行して結果を返す
// 処理順: FetchFillEvents → WakeupCondition評価
func (t *Ticker) Tick(ctx context.Context, rate currency.Rate) (Result, error) {
	// 約定イベント同期
	lastID, _ := t.store.LoadLastFillEventID(ctx)
	events, err := t.broker.FetchFillEvents(ctx, lastID)
	if err != nil {
		return Result{}, fmt.Errorf("tick: fetch fill events: %w", err)
	}
	fillCount := len(events)
	if fillCount > 0 {
		newLastID := events[len(events)-1].ID
		if err := t.store.SaveLastFillEventID(ctx, newLastID); err != nil {
			return Result{}, fmt.Errorf("tick: save last fill event id: %w", err)
		}
	}

	// WakeupCondition評価
	cond, ok, err := t.wakeupStore.Load(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("tick: load wakeup: %w", err)
	}

	shouldWakeup := true // 条件なし → 毎ティック起動
	if ok {
		rates := map[currency.Pair]decimal.Decimal{rate.Pair: rate.Bid}
		if cond.IsMet(rate.Timestamp, rates, fillCount > 0) {
			if err := t.wakeupStore.Delete(ctx); err != nil {
				return Result{}, fmt.Errorf("tick: delete wakeup: %w", err)
			}
		} else {
			shouldWakeup = false
		}
	}

	return Result{ShouldWakeup: shouldWakeup, FillCount: fillCount}, nil
}
