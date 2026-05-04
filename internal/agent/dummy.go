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

// DummyStrategy はバックテスト検証用のシンプルな戦略。
// ポジションがなければ成行ロング、あれば成行決済する。
type DummyStrategy struct {
	pair     currency.Pair
	lots     decimal.Decimal
	stopLoss decimal.Decimal
}

func NewDummyStrategy(pair currency.Pair, lots decimal.Decimal, stopLoss decimal.Decimal) *DummyStrategy {
	return &DummyStrategy{pair: pair, lots: lots, stopLoss: stopLoss}
}

func (s *DummyStrategy) Name() string { return "dummy" }

func (s *DummyStrategy) OnTick(ctx context.Context, snap pool.SubPoolSnapshot, mkt market.MarketContext) (TickResult, error) {
	now := mkt.Timestamp
	if now.IsZero() {
		now = time.Now()
	}

	if len(snap.Positions) == 0 && len(snap.PendingOrders) == 0 {
		return TickResult{Orders: []pool.OrderRequest{{
			SubPoolID:   snap.ID,
			Pair:        s.pair,
			Side:        pkgorder.Long,
			Lots:        s.lots,
			OrderType:   pkgorder.OrderTypeMarket,
			OrderIntent: pool.OrderIntentOpen,
			StopLoss:    s.stopLoss,
			RequestedAt: now,
		}}}, nil
	}

	if len(snap.Positions) > 0 && len(snap.PendingOrders) == 0 {
		reqs := make([]pool.OrderRequest, 0, len(snap.Positions))
		for _, pos := range snap.Positions {
			reqs = append(reqs, pool.OrderRequest{
				SubPoolID:       snap.ID,
				Pair:            s.pair,
				Side:            pkgorder.Short,
				Lots:            pos.Lots,
				OrderType:       pkgorder.OrderTypeMarket,
				OrderIntent:     pool.OrderIntentClose,
				StopLoss:        s.stopLoss,
				ClosePositionID: pos.ID,
				RequestedAt:     now,
			})
		}
		return TickResult{Orders: reqs}, nil
	}

	return TickResult{}, nil
}

func (s *DummyStrategy) OnInstruction(_ context.Context, _ string) error { return nil }
