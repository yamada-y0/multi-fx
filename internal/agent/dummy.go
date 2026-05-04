package agent

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
	"github.com/yamada/multi-fx/internal/pool"
	"github.com/yamada/multi-fx/pkg/currency"
	"github.com/yamada/multi-fx/pkg/market"
	pkgorder "github.com/yamada/multi-fx/pkg/order"
)

// DummyAgent はバックテスト検証用のルールベースAgent。
// ポジションがなければ成行ロング、あれば成行決済する。
type DummyAgent struct {
	baseAgent
	pair     currency.Pair
	lots     decimal.Decimal
	stopLoss decimal.Decimal
}

func NewDummyAgent(subPool pool.SubPool, wakeupStore WakeupStore, pair currency.Pair, lots decimal.Decimal, stopLoss decimal.Decimal) Agent {
	return &DummyAgent{
		baseAgent: baseAgent{subPool: subPool, wakeupStore: wakeupStore},
		pair:      pair,
		lots:      lots,
		stopLoss:  stopLoss,
	}
}

func (a *DummyAgent) Tick(ctx context.Context, mkt market.MarketContext) ([]pool.OrderRequest, error) {
	snap := a.subPool.Snapshot()
	now := mkt.Timestamp
	if now.IsZero() {
		now = time.Now()
	}

	if len(snap.Positions) == 0 && len(snap.PendingOrders) == 0 {
		return []pool.OrderRequest{{
			SubPoolID:   snap.ID,
			Pair:        a.pair,
			Side:        pkgorder.Long,
			Lots:        a.lots,
			OrderType:   pkgorder.OrderTypeMarket,
			OrderIntent: pool.OrderIntentOpen,
			StopLoss:    a.stopLoss,
			RequestedAt: now,
		}}, nil
	}

	if len(snap.Positions) > 0 && len(snap.PendingOrders) == 0 {
		reqs := make([]pool.OrderRequest, 0, len(snap.Positions))
		for _, pos := range snap.Positions {
			reqs = append(reqs, pool.OrderRequest{
				SubPoolID:       snap.ID,
				Pair:            a.pair,
				Side:            pkgorder.Short,
				Lots:            pos.Lots,
				OrderType:       pkgorder.OrderTypeMarket,
				OrderIntent:     pool.OrderIntentClose,
				StopLoss:        a.stopLoss,
				ClosePositionID: pos.ID,
				RequestedAt:     now,
			})
		}
		return reqs, nil
	}

	return nil, nil
}

func (a *DummyAgent) ApplyInstruction(_ context.Context, inst Instruction) error {
	switch inst.Type {
	case InstructionSuspend:
		return a.subPool.Suspend()
	default:
		return nil
	}
}
