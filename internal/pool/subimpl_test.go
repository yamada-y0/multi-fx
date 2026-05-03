package pool_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/yamada/multi-fx/internal/pool"
	"github.com/yamada/multi-fx/pkg/currency"
	pkgorder "github.com/yamada/multi-fx/pkg/order"
)

func d(f float64) decimal.Decimal { return decimal.NewFromFloat(f) }

var t0 = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

func newTestPool(balance float64) pool.SubPool {
	return pool.NewSubPool("pool-a", d(balance), "test-strategy", t0)
}

func openFill(posID string, side pkgorder.Side, lots, price float64) pool.Fill {
	return pool.Fill{
		BrokerOrderID: posID,
		SubPoolID:     "pool-a",
		Pair:          currency.USDJPY,
		Side:          side,
		Lots:          d(lots),
		FilledPrice:   d(price),
		FilledAt:      t0,
		Intent:        pool.OrderIntentOpen,
	}
}

func closeFill(posID string, side pkgorder.Side, lots, price float64) pool.Fill {
	return pool.Fill{
		BrokerOrderID:   "close-order",
		SubPoolID:       "pool-a",
		Pair:            currency.USDJPY,
		Side:            side,
		Lots:            d(lots),
		FilledPrice:     d(price),
		FilledAt:        t0,
		Intent:          pool.OrderIntentClose,
		ClosePositionID: posID,
	}
}

// --- Snapshot ---

func TestSubPool_Snapshot_InitialState(t *testing.T) {
	sp := newTestPool(100000)
	snap := sp.Snapshot()

	if snap.ID != "pool-a" {
		t.Errorf("ID = %v, want pool-a", snap.ID)
	}
	if snap.State != pool.StateActive {
		t.Errorf("State = %v, want Active", snap.State)
	}
	if !snap.CurrentBalance.Equal(d(100000)) {
		t.Errorf("CurrentBalance = %v, want 100000", snap.CurrentBalance)
	}
	if !snap.InitialBalance.Equal(d(100000)) {
		t.Errorf("InitialBalance = %v, want 100000", snap.InitialBalance)
	}
	if len(snap.Positions) != 0 {
		t.Errorf("Positions = %d, want 0", len(snap.Positions))
	}
}

// --- OnFill（新規）---

func TestSubPool_OnFill_Open_AddsPosition(t *testing.T) {
	sp := newTestPool(100000)
	sp.OnFill(openFill("pos-1", pkgorder.Long, 0.1, 140.00))

	snap := sp.Snapshot()
	if len(snap.Positions) != 1 {
		t.Fatalf("positions = %d, want 1", len(snap.Positions))
	}
	if snap.Positions[0].ID != "pos-1" {
		t.Errorf("PositionID = %v, want pos-1", snap.Positions[0].ID)
	}
}

func TestSubPool_OnFill_Open_DoesNotChangeBalance(t *testing.T) {
	sp := newTestPool(100000)
	sp.OnFill(openFill("pos-1", pkgorder.Long, 0.1, 140.00))

	// 新規建てでは残高は変わらない（証拠金管理は初期スコープ外）
	if !sp.Snapshot().CurrentBalance.Equal(d(100000)) {
		t.Errorf("CurrentBalance = %v, want 100000", sp.Snapshot().CurrentBalance)
	}
}

// --- OnFill（決済）---

func TestSubPool_OnFill_Close_Long_Profit(t *testing.T) {
	sp := newTestPool(100000)
	sp.OnFill(openFill("pos-1", pkgorder.Long, 0.1, 140.00))
	sp.OnFill(closeFill("pos-1", pkgorder.Short, 0.1, 141.00))

	snap := sp.Snapshot()
	if len(snap.Positions) != 0 {
		t.Errorf("positions = %d, want 0", len(snap.Positions))
	}
	// 利益: (141.00 - 140.00) × 0.1 = 0.1
	if !snap.RealizedPnL.Equal(d(0.1)) {
		t.Errorf("RealizedPnL = %v, want 0.1", snap.RealizedPnL)
	}
	if !snap.CurrentBalance.Equal(d(100000.1)) {
		t.Errorf("CurrentBalance = %v, want 100000.1", snap.CurrentBalance)
	}
}

func TestSubPool_OnFill_Close_Long_Loss(t *testing.T) {
	sp := newTestPool(100000)
	sp.OnFill(openFill("pos-1", pkgorder.Long, 0.1, 140.00))
	sp.OnFill(closeFill("pos-1", pkgorder.Short, 0.1, 139.00))

	snap := sp.Snapshot()
	// 損失: (139.00 - 140.00) × 0.1 = -0.1
	if !snap.RealizedPnL.Equal(d(-0.1)) {
		t.Errorf("RealizedPnL = %v, want -0.1", snap.RealizedPnL)
	}
	if !snap.CurrentBalance.Equal(d(99999.9)) {
		t.Errorf("CurrentBalance = %v, want 99999.9", snap.CurrentBalance)
	}
}

func TestSubPool_OnFill_Close_Short_Profit(t *testing.T) {
	sp := newTestPool(100000)
	sp.OnFill(openFill("pos-1", pkgorder.Short, 0.1, 140.00))
	sp.OnFill(closeFill("pos-1", pkgorder.Long, 0.1, 139.00))

	snap := sp.Snapshot()
	// 利益: (140.00 - 139.00) × 0.1 = 0.1
	if !snap.RealizedPnL.Equal(d(0.1)) {
		t.Errorf("RealizedPnL = %v, want 0.1", snap.RealizedPnL)
	}
}

func TestSubPool_OnFill_Close_InvalidPositionID_IsIgnored(t *testing.T) {
	sp := newTestPool(100000)
	sp.OnFill(closeFill("nonexistent", pkgorder.Short, 0.1, 141.00))

	// 存在しないPositionIDはpanicせず無視する
	snap := sp.Snapshot()
	if !snap.CurrentBalance.Equal(d(100000)) {
		t.Errorf("CurrentBalance = %v, want 100000", snap.CurrentBalance)
	}
}

// --- OnRate ---

func TestSubPool_OnRate_Long_UnrealizedPnL(t *testing.T) {
	sp := newTestPool(100000)
	sp.OnFill(openFill("pos-1", pkgorder.Long, 0.1, 140.00))

	sp.OnRate(currency.Rate{Pair: currency.USDJPY, Bid: d(141.00), Ask: d(141.00)})

	snap := sp.Snapshot()
	// 含み益: (141.00 - 140.00) × 0.1 = 0.1
	if !snap.UnrealizedPnL.Equal(d(0.1)) {
		t.Errorf("UnrealizedPnL = %v, want 0.1", snap.UnrealizedPnL)
	}
}

func TestSubPool_OnRate_Short_UnrealizedPnL(t *testing.T) {
	sp := newTestPool(100000)
	sp.OnFill(openFill("pos-1", pkgorder.Short, 0.1, 140.00))

	sp.OnRate(currency.Rate{Pair: currency.USDJPY, Bid: d(139.00), Ask: d(139.00)})

	snap := sp.Snapshot()
	// 含み益: (140.00 - 139.00) × 0.1 = 0.1
	if !snap.UnrealizedPnL.Equal(d(0.1)) {
		t.Errorf("UnrealizedPnL = %v, want 0.1", snap.UnrealizedPnL)
	}
}

func TestSubPool_OnRate_DifferentPair_IsIgnored(t *testing.T) {
	sp := newTestPool(100000)
	sp.OnFill(openFill("pos-1", pkgorder.Long, 0.1, 140.00))

	sp.OnRate(currency.Rate{Pair: currency.EURUSD, Bid: d(1.10), Ask: d(1.10)})

	snap := sp.Snapshot()
	if !snap.UnrealizedPnL.Equal(decimal.Zero) {
		t.Errorf("UnrealizedPnL = %v, want 0 (different pair)", snap.UnrealizedPnL)
	}
}

func TestSubPool_EquityBalance(t *testing.T) {
	sp := newTestPool(100000)
	sp.OnFill(openFill("pos-1", pkgorder.Long, 0.1, 140.00))
	sp.OnRate(currency.Rate{Pair: currency.USDJPY, Bid: d(141.00), Ask: d(141.00)})

	snap := sp.Snapshot()
	// EquityBalance = 100000 + 0.1 = 100000.1
	if !snap.EquityBalance().Equal(d(100000.1)) {
		t.Errorf("EquityBalance = %v, want 100000.1", snap.EquityBalance())
	}
}

// --- Suspend / Terminate ---

func TestSubPool_Suspend(t *testing.T) {
	sp := newTestPool(100000)
	if err := sp.Suspend(); err != nil {
		t.Fatalf("Suspend: %v", err)
	}
	if sp.Snapshot().State != pool.StateSuspended {
		t.Errorf("State = %v, want Suspended", sp.Snapshot().State)
	}
}

func TestSubPool_Suspend_AlreadySuspended(t *testing.T) {
	sp := newTestPool(100000)
	sp.Suspend()
	if err := sp.Suspend(); err == nil {
		t.Error("Suspend from Suspended should return error")
	}
}

func TestSubPool_Terminate(t *testing.T) {
	sp := newTestPool(100000)
	sp.Suspend()

	amount, err := sp.Terminate()
	if err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	if !amount.Equal(d(100000)) {
		t.Errorf("returnAmount = %v, want 100000", amount)
	}
	if sp.Snapshot().State != pool.StateTerminated {
		t.Errorf("State = %v, want Terminated", sp.Snapshot().State)
	}
	if !sp.Snapshot().CurrentBalance.Equal(decimal.Zero) {
		t.Errorf("CurrentBalance after terminate = %v, want 0", sp.Snapshot().CurrentBalance)
	}
}

func TestSubPool_Terminate_WithoutSuspend(t *testing.T) {
	sp := newTestPool(100000)
	_, err := sp.Terminate()
	if err == nil {
		t.Error("Terminate from Active should return error")
	}
}

func TestSubPool_Terminate_WithOpenPositions(t *testing.T) {
	sp := newTestPool(100000)
	sp.OnFill(openFill("pos-1", pkgorder.Long, 0.1, 140.00))
	sp.Suspend()

	_, err := sp.Terminate()
	if err == nil {
		t.Error("Terminate with open positions should return error")
	}
}

func TestSubPool_FloorRule_Breached(t *testing.T) {
	sp := newTestPool(100000)
	sp.OnFill(openFill("pos-1", pkgorder.Long, 0.1, 140.00))
	// 含み損でEquityBalanceがInitialBalanceを下回る
	sp.OnRate(currency.Rate{Pair: currency.USDJPY, Bid: d(100.00), Ask: d(100.00)})

	if !sp.Snapshot().IsFloorRuleBreached() {
		t.Error("IsFloorRuleBreached should be true")
	}
}
