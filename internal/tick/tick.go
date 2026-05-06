package tick

import (
	"context"
	"fmt"

	"github.com/shopspring/decimal"
	"github.com/yamada/fxd/internal/agent"
	"github.com/yamada/fxd/internal/order"
	"github.com/yamada/fxd/internal/pool"
	"github.com/yamada/fxd/internal/rule"
	"github.com/yamada/fxd/internal/store"
	"github.com/yamada/fxd/pkg/currency"
)

// Result は1ティック分の処理結果
type Result struct {
	// ShouldWakeup は Claude を起動すべきかどうか
	ShouldWakeup bool
	// Done はデータ終端などでこれ以上ティックを進められないことを示す
	Done bool
	// FillCount はこのティックで約定した件数
	FillCount int
}

// Ticker は1ティック分のサイクルを実行する
type Ticker struct {
	agg         order.Aggregator
	subPool     pool.SubPool
	wakeupStore agent.WakeupStore
	engine      *rule.RuleEngine
	store       store.StateStore
}

func New(
	agg order.Aggregator,
	subPool pool.SubPool,
	wakeupStore agent.WakeupStore,
	engine *rule.RuleEngine,
	st store.StateStore,
) *Ticker {
	return &Ticker{
		agg:         agg,
		subPool:     subPool,
		wakeupStore: wakeupStore,
		engine:      engine,
		store:       st,
	}
}

// Tick は1ティック分の処理を実行して結果を返す
// 処理順: レート更新 → SyncFills → フロアルール評価 → WakeupCondition評価 → SubPool保存
func (t *Ticker) Tick(ctx context.Context, rate currency.Rate) (Result, error) {
	// レート更新（含み損益の再計算）
	t.subPool.OnRate(rate)

	// 約定同期
	fillsBefore := t.subPool.Snapshot().Positions
	if err := t.agg.SyncFills(ctx); err != nil {
		return Result{}, fmt.Errorf("tick: sync fills: %w", err)
	}
	fillCount := len(t.subPool.Snapshot().Positions) - len(fillsBefore)
	if fillCount < 0 {
		// 決済によってポジションが減った場合も約定とみなす
		fillCount = -fillCount
	}

	// フロアルール評価
	snap := t.subPool.Snapshot()
	if action := t.engine.Evaluate(snap); action >= rule.ActionSuspend {
		if err := t.subPool.Suspend(); err == nil {
			if err := t.store.SaveSubPool(ctx, t.subPool.Snapshot()); err != nil {
				return Result{}, fmt.Errorf("tick: save subpool after suspend: %w", err)
			}
			return Result{Done: true}, nil
		}
	}

	// WakeupCondition評価
	cond, ok, err := t.wakeupStore.Load(ctx, t.subPool.ID())
	if err != nil {
		return Result{}, fmt.Errorf("tick: load wakeup: %w", err)
	}

	shouldWakeup := true // 条件なし → 毎ティック起動
	if ok {
		rates := map[currency.Pair]decimal.Decimal{rate.Pair: rate.Bid}
		filled := fillCount > 0
		if cond.IsMet(rate.Timestamp, rates, filled) {
			if err := t.wakeupStore.Delete(ctx, t.subPool.ID()); err != nil {
				return Result{}, fmt.Errorf("tick: delete wakeup: %w", err)
			}
		} else {
			shouldWakeup = false
		}
	}

	// SubPool保存
	if err := t.store.SaveSubPool(ctx, t.subPool.Snapshot()); err != nil {
		return Result{}, fmt.Errorf("tick: save subpool: %w", err)
	}

	return Result{
		ShouldWakeup: shouldWakeup,
		FillCount:    fillCount,
	}, nil
}
