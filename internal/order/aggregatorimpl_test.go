package order_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/yamada/multi-fx/internal/broker"
	"github.com/yamada/multi-fx/internal/order"
	"github.com/yamada/multi-fx/internal/pool"
	"github.com/yamada/multi-fx/pkg/currency"
	"github.com/yamada/multi-fx/pkg/market"
	pkgorder "github.com/yamada/multi-fx/pkg/order"
)

func d(f float64) decimal.Decimal { return decimal.NewFromFloat(f) }

var t0 = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

var testRows = []market.Candle{
	{Timestamp: t0, Pair: currency.USDJPY, Open: d(140.00), High: d(141.00), Low: d(139.00), Close: d(140.50)},
	{Timestamp: t0.Add(time.Hour), Pair: currency.USDJPY, Open: d(140.50), High: d(142.00), Low: d(138.00), Close: d(141.00)},
}

func setup(t *testing.T) (order.Aggregator, pool.SubPool, broker.HistoricalBroker) {
	t.Helper()
	b, _ := broker.NewHistoricalBrokerFromRows(currency.USDJPY, testRows)
	sp := pool.NewSubPool("pool-a", d(100000), "test", t0)
	subPools := map[pool.SubPoolID]pool.SubPool{"pool-a": sp}
	agg := order.NewAggregator(b, subPools, order.NewIdentityMapper())
	return agg, sp, b
}

func TestAggregator_SubmitAndSyncFills_Market(t *testing.T) {
	agg, sp, _ := setup(t)

	err := agg.SubmitOrder(context.Background(), pool.OrderRequest{
		SubPoolID:   "pool-a",
		Pair:        currency.USDJPY,
		Side:        pkgorder.Long,
		Lots:        d(0.1),
		OrderType:   pkgorder.OrderTypeMarket,
		OrderIntent: pool.OrderIntentOpen,
	})
	if err != nil {
		t.Fatalf("SubmitOrder: %v", err)
	}

	// 成行は即時約定済みなのでSyncFillsで届く
	if err := agg.SyncFills(context.Background()); err != nil {
		t.Fatalf("SyncFills: %v", err)
	}

	snap := sp.Snapshot()
	if len(snap.Positions) != 1 {
		t.Errorf("positions = %d, want 1", len(snap.Positions))
	}
	if !snap.Positions[0].OpenPrice.Equal(d(140.50)) {
		t.Errorf("OpenPrice = %v, want 140.50", snap.Positions[0].OpenPrice)
	}
}

func TestAggregator_SyncFills_ClearsActiveOrders(t *testing.T) {
	agg, _, _ := setup(t)

	agg.SubmitOrder(context.Background(), pool.OrderRequest{
		SubPoolID:   "pool-a",
		Pair:        currency.USDJPY,
		Side:        pkgorder.Long,
		Lots:        d(0.1),
		OrderType:   pkgorder.OrderTypeMarket,
		OrderIntent: pool.OrderIntentOpen,
	})

	if len(agg.ActiveOrders()) != 1 {
		t.Fatalf("ActiveOrders before sync = %d, want 1", len(agg.ActiveOrders()))
	}

	agg.SyncFills(context.Background())

	if len(agg.ActiveOrders()) != 0 {
		t.Errorf("ActiveOrders after sync = %d, want 0", len(agg.ActiveOrders()))
	}
}

func TestAggregator_SubmitAndSyncFills_Limit(t *testing.T) {
	agg, sp, b := setup(t)

	agg.SubmitOrder(context.Background(), pool.OrderRequest{
		SubPoolID:   "pool-a",
		Pair:        currency.USDJPY,
		Side:        pkgorder.Long,
		Lots:        d(0.1),
		OrderType:   pkgorder.OrderTypeLimit,
		OrderIntent: pool.OrderIntentOpen,
		LimitPrice:  d(139.50), // tick1 Low=138.00 <= 139.50 → 約定
	})

	// 指値はAdvance前は約定しない
	agg.SyncFills(context.Background())
	if len(sp.Snapshot().Positions) != 0 {
		t.Fatalf("positions before advance = %d, want 0", len(sp.Snapshot().Positions))
	}

	b.Advance()
	agg.SyncFills(context.Background())

	if len(sp.Snapshot().Positions) != 1 {
		t.Errorf("positions after advance = %d, want 1", len(sp.Snapshot().Positions))
	}
}

func TestAggregator_CancelOrder(t *testing.T) {
	agg, sp, b := setup(t)

	agg.SubmitOrder(context.Background(), pool.OrderRequest{
		SubPoolID:   "pool-a",
		Pair:        currency.USDJPY,
		Side:        pkgorder.Long,
		Lots:        d(0.1),
		OrderType:   pkgorder.OrderTypeLimit,
		OrderIntent: pool.OrderIntentOpen,
		LimitPrice:  d(139.50),
	})

	orders := agg.ActiveOrders()
	if len(orders) != 1 {
		t.Fatalf("ActiveOrders = %d, want 1", len(orders))
	}

	err := agg.CancelOrder(context.Background(), "pool-a", orders[0].BrokerOrderID)
	if err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}

	b.Advance()
	agg.SyncFills(context.Background())

	if len(sp.Snapshot().Positions) != 0 {
		t.Errorf("positions after cancel = %d, want 0", len(sp.Snapshot().Positions))
	}
}

func TestAggregator_CancelOrder_WrongSubPool(t *testing.T) {
	agg, _, _ := setup(t)

	agg.SubmitOrder(context.Background(), pool.OrderRequest{
		SubPoolID:   "pool-a",
		Pair:        currency.USDJPY,
		Side:        pkgorder.Long,
		Lots:        d(0.1),
		OrderType:   pkgorder.OrderTypeLimit,
		OrderIntent: pool.OrderIntentOpen,
		LimitPrice:  d(139.50),
	})

	orders := agg.ActiveOrders()
	err := agg.CancelOrder(context.Background(), "pool-b", orders[0].BrokerOrderID)
	if err == nil {
		t.Error("CancelOrder with wrong subPoolID should return error")
	}
}

func TestAggregator_OpenAndClose(t *testing.T) {
	agg, sp, _ := setup(t)

	// 新規建て
	agg.SubmitOrder(context.Background(), pool.OrderRequest{
		SubPoolID:   "pool-a",
		Pair:        currency.USDJPY,
		Side:        pkgorder.Long,
		Lots:        d(0.1),
		OrderType:   pkgorder.OrderTypeMarket,
		OrderIntent: pool.OrderIntentOpen,
	})
	agg.SyncFills(context.Background())

	positions := sp.Snapshot().Positions
	if len(positions) != 1 {
		t.Fatalf("positions after open = %d, want 1", len(positions))
	}
	posID := positions[0].ID

	// 決済
	agg.SubmitOrder(context.Background(), pool.OrderRequest{
		SubPoolID:       "pool-a",
		Pair:            currency.USDJPY,
		Side:            pkgorder.Short,
		Lots:            d(0.1),
		OrderType:       pkgorder.OrderTypeMarket,
		OrderIntent:     pool.OrderIntentClose,
		ClosePositionID: posID,
	})
	agg.SyncFills(context.Background())

	if len(sp.Snapshot().Positions) != 0 {
		t.Errorf("positions after close = %d, want 0", len(sp.Snapshot().Positions))
	}
}
