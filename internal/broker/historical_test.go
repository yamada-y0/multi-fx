package broker_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/yamada/fxd/internal/broker"
	"github.com/yamada/fxd/pkg/currency"
	"github.com/yamada/fxd/pkg/market"
	pkgorder "github.com/yamada/fxd/pkg/order"
)

// d は decimal.NewFromFloat の短縮ヘルパー
func d(f float64) decimal.Decimal { return decimal.NewFromFloat(f) }

var t0 = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// testRows は値が明確な固定OHLCVデータ
//
//	tick0: O=140.00 H=141.00 L=139.00 C=140.50
//	tick1: O=140.50 H=142.00 L=138.00 C=141.00
//	tick2: O=141.00 H=143.00 L=137.00 C=142.00
var testRows = []market.Candle{
	{Timestamp: t0, Pair: currency.USDJPY, Open: d(140.00), High: d(141.00), Low: d(139.00), Close: d(140.50)},
	{Timestamp: t0.Add(time.Hour), Pair: currency.USDJPY, Open: d(140.50), High: d(142.00), Low: d(138.00), Close: d(141.00)},
	{Timestamp: t0.Add(2 * time.Hour), Pair: currency.USDJPY, Open: d(141.00), High: d(143.00), Low: d(137.00), Close: d(142.00)},
}

func newTestBroker(t *testing.T) broker.HistoricalBroker {
	t.Helper()
	b, err := broker.NewHistoricalBrokerFromRows(currency.USDJPY, testRows)
	if err != nil {
		t.Fatalf("NewHistoricalBrokerFromRows: %v", err)
	}
	return b
}

// --- Advance / FetchRate ---

func TestHistoricalBroker_Advance(t *testing.T) {
	b := newTestBroker(t)

	if b.CurrentTime() != t0 {
		t.Errorf("initial time = %v, want %v", b.CurrentTime(), t0)
	}
	if !b.Advance() {
		t.Fatal("Advance() should return true")
	}
	if b.CurrentTime() != t0.Add(time.Hour) {
		t.Errorf("time after advance = %v, want %v", b.CurrentTime(), t0.Add(time.Hour))
	}
}

func TestHistoricalBroker_AdvanceToEnd(t *testing.T) {
	b := newTestBroker(t)

	b.Advance()
	b.Advance()

	if b.Advance() {
		t.Error("Advance() at end should return false")
	}
}

func TestHistoricalBroker_FetchRate(t *testing.T) {
	b := newTestBroker(t)

	rate, err := b.FetchRate(context.Background(), currency.USDJPY)
	if err != nil {
		t.Fatalf("FetchRate: %v", err)
	}
	if !rate.Bid.Equal(d(140.50)) {
		t.Errorf("Bid = %v, want 140.50", rate.Bid)
	}
	if !rate.Bid.Equal(rate.Ask) {
		t.Errorf("Bid(%v) != Ask(%v)", rate.Bid, rate.Ask)
	}
}

func TestHistoricalBroker_FetchRate_UnsupportedPair(t *testing.T) {
	b := newTestBroker(t)

	_, err := b.FetchRate(context.Background(), currency.EURUSD)
	if err == nil {
		t.Error("FetchRate with unsupported pair should return error")
	}
}

// --- OrderTypeStop（逆指値）---

func TestHistoricalBroker_Stop_Long_Filled(t *testing.T) {
	b := newTestBroker(t)

	b.SubmitOrder(context.Background(), pkgorder.Order{
		Pair:      currency.USDJPY,
		Side:      pkgorder.Long,
		Lots:      d(0.1),
		OrderType: pkgorder.OrderTypeStop,
		Intent:    pkgorder.OrderIntentOpen,
		StopLoss:  d(138.50), // tick1 Low=138.00 <= 138.50 → 約定
	})

	b.Advance()

	positions, err := b.FetchPositions(context.Background())
	if err != nil {
		t.Fatalf("FetchPositions: %v", err)
	}
	if len(positions) != 1 {
		t.Fatalf("positions = %d, want 1", len(positions))
	}
	if !positions[0].OpenPrice.Equal(d(138.50)) {
		t.Errorf("OpenPrice = %v, want 138.50", positions[0].OpenPrice)
	}
}

func TestHistoricalBroker_Stop_Long_NotFilled(t *testing.T) {
	b := newTestBroker(t)

	b.SubmitOrder(context.Background(), pkgorder.Order{
		Pair:      currency.USDJPY,
		Side:      pkgorder.Long,
		Lots:      d(0.1),
		OrderType: pkgorder.OrderTypeStop,
		Intent:    pkgorder.OrderIntentOpen,
		StopLoss:  d(137.00), // tick1 Low=138.00 > 137.00 → 約定しない
	})

	b.Advance()

	positions, _ := b.FetchPositions(context.Background())
	if len(positions) != 0 {
		t.Errorf("positions = %d, want 0", len(positions))
	}
}

func TestHistoricalBroker_Stop_Short_Filled(t *testing.T) {
	b := newTestBroker(t)

	b.SubmitOrder(context.Background(), pkgorder.Order{
		Pair:      currency.USDJPY,
		Side:      pkgorder.Short,
		Lots:      d(0.1),
		OrderType: pkgorder.OrderTypeStop,
		Intent:    pkgorder.OrderIntentOpen,
		StopLoss:  d(141.50), // tick1 High=142.00 >= 141.50 → 約定
	})

	b.Advance()

	positions, _ := b.FetchPositions(context.Background())
	if len(positions) != 1 {
		t.Fatalf("positions = %d, want 1", len(positions))
	}
	if !positions[0].OpenPrice.Equal(d(141.50)) {
		t.Errorf("OpenPrice = %v, want 141.50", positions[0].OpenPrice)
	}
}

// --- OrderTypeMarket（成行）---

func TestHistoricalBroker_Market_Long_FilledImmediately(t *testing.T) {
	b := newTestBroker(t)

	b.SubmitOrder(context.Background(), pkgorder.Order{
		Pair:      currency.USDJPY,
		Side:      pkgorder.Long,
		Lots:      d(0.1),
		OrderType: pkgorder.OrderTypeMarket,
		Intent:    pkgorder.OrderIntentOpen,
	})

	// Advance() 前でも即時約定でポジションが存在する
	positions, err := b.FetchPositions(context.Background())
	if err != nil {
		t.Fatalf("FetchPositions: %v", err)
	}
	if len(positions) != 1 {
		t.Fatalf("positions = %d, want 1", len(positions))
	}
	if !positions[0].OpenPrice.Equal(d(140.50)) {
		t.Errorf("OpenPrice = %v, want 140.50", positions[0].OpenPrice)
	}
}

func TestHistoricalBroker_Market_Short_FilledImmediately(t *testing.T) {
	b := newTestBroker(t)

	b.SubmitOrder(context.Background(), pkgorder.Order{
		Pair:      currency.USDJPY,
		Side:      pkgorder.Short,
		Lots:      d(0.1),
		OrderType: pkgorder.OrderTypeMarket,
		Intent:    pkgorder.OrderIntentOpen,
	})

	positions, _ := b.FetchPositions(context.Background())
	if len(positions) != 1 {
		t.Fatalf("positions = %d, want 1", len(positions))
	}
	if !positions[0].OpenPrice.Equal(d(140.50)) {
		t.Errorf("OpenPrice = %v, want 140.50", positions[0].OpenPrice)
	}
}

// --- OrderTypeLimit（指値）---

func TestHistoricalBroker_Limit_Long_Filled(t *testing.T) {
	b := newTestBroker(t)

	b.SubmitOrder(context.Background(), pkgorder.Order{
		Pair:       currency.USDJPY,
		Side:       pkgorder.Long,
		Lots:       d(0.1),
		OrderType:  pkgorder.OrderTypeLimit,
		Intent:     pkgorder.OrderIntentOpen,
		LimitPrice: d(139.50), // tick1 Low=138.00 <= 139.50 → 約定
	})

	b.Advance()

	positions, _ := b.FetchPositions(context.Background())
	if len(positions) != 1 {
		t.Fatalf("positions = %d, want 1", len(positions))
	}
	if !positions[0].OpenPrice.Equal(d(139.50)) {
		t.Errorf("OpenPrice = %v, want 139.50", positions[0].OpenPrice)
	}
}

func TestHistoricalBroker_Limit_Long_NotFilled(t *testing.T) {
	b := newTestBroker(t)

	b.SubmitOrder(context.Background(), pkgorder.Order{
		Pair:       currency.USDJPY,
		Side:       pkgorder.Long,
		Lots:       d(0.1),
		OrderType:  pkgorder.OrderTypeLimit,
		Intent:     pkgorder.OrderIntentOpen,
		LimitPrice: d(137.50), // tick1 Low=138.00 > 137.50 → 約定しない
	})

	b.Advance()

	positions, _ := b.FetchPositions(context.Background())
	if len(positions) != 0 {
		t.Errorf("positions = %d, want 0", len(positions))
	}
}

func TestHistoricalBroker_Limit_Long_ExactPrice(t *testing.T) {
	b := newTestBroker(t)

	b.SubmitOrder(context.Background(), pkgorder.Order{
		Pair:       currency.USDJPY,
		Side:       pkgorder.Long,
		Lots:       d(0.1),
		OrderType:  pkgorder.OrderTypeLimit,
		Intent:     pkgorder.OrderIntentOpen,
		LimitPrice: d(138.00), // tick1 Low=138.00 と一致 → 約定する
	})

	b.Advance()

	positions, _ := b.FetchPositions(context.Background())
	if len(positions) != 1 {
		t.Fatalf("positions = %d, want 1 (exact price should fill)", len(positions))
	}
}

func TestHistoricalBroker_Limit_Short_Filled(t *testing.T) {
	b := newTestBroker(t)

	b.SubmitOrder(context.Background(), pkgorder.Order{
		Pair:       currency.USDJPY,
		Side:       pkgorder.Short,
		Lots:       d(0.1),
		OrderType:  pkgorder.OrderTypeLimit,
		Intent:     pkgorder.OrderIntentOpen,
		LimitPrice: d(141.50), // tick1 High=142.00 >= 141.50 → 約定
	})

	b.Advance()

	positions, _ := b.FetchPositions(context.Background())
	if len(positions) != 1 {
		t.Fatalf("positions = %d, want 1", len(positions))
	}
	if !positions[0].OpenPrice.Equal(d(141.50)) {
		t.Errorf("OpenPrice = %v, want 141.50", positions[0].OpenPrice)
	}
}

func TestHistoricalBroker_Limit_Short_NotFilled(t *testing.T) {
	b := newTestBroker(t)

	b.SubmitOrder(context.Background(), pkgorder.Order{
		Pair:       currency.USDJPY,
		Side:       pkgorder.Short,
		Lots:       d(0.1),
		OrderType:  pkgorder.OrderTypeLimit,
		Intent:     pkgorder.OrderIntentOpen,
		LimitPrice: d(142.50), // tick1 High=142.00 < 142.50 → 約定しない
	})

	b.Advance()

	positions, _ := b.FetchPositions(context.Background())
	if len(positions) != 0 {
		t.Errorf("positions = %d, want 0", len(positions))
	}
}

func TestHistoricalBroker_Limit_CancelBeforeFill(t *testing.T) {
	b := newTestBroker(t)

	id, err := b.SubmitOrder(context.Background(), pkgorder.Order{
		Pair:       currency.USDJPY,
		Side:       pkgorder.Long,
		Lots:       d(0.1),
		OrderType:  pkgorder.OrderTypeLimit,
		Intent:     pkgorder.OrderIntentOpen,
		LimitPrice: d(139.50),
	})
	if err != nil {
		t.Fatalf("SubmitOrder: %v", err)
	}

	if err := b.CancelOrder(context.Background(), id); err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}

	b.Advance()

	positions, _ := b.FetchPositions(context.Background())
	if len(positions) != 0 {
		t.Errorf("positions = %d, want 0 after cancel", len(positions))
	}
}

// --- FetchOrders ---

func TestHistoricalBroker_FetchOrders_ReturnsPending(t *testing.T) {
	b := newTestBroker(t)

	b.SubmitOrder(context.Background(), pkgorder.Order{
		Pair:       currency.USDJPY,
		Side:       pkgorder.Long,
		Lots:       d(0.1),
		OrderType:  pkgorder.OrderTypeLimit,
		Intent:     pkgorder.OrderIntentOpen,
		LimitPrice: d(137.00), // 約定しない価格
	})

	orders, err := b.FetchOrders(context.Background())
	if err != nil {
		t.Fatalf("FetchOrders: %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("orders = %d, want 1", len(orders))
	}
}

func TestHistoricalBroker_FetchOrders_EmptyAfterFill(t *testing.T) {
	b := newTestBroker(t)

	b.SubmitOrder(context.Background(), pkgorder.Order{
		Pair:       currency.USDJPY,
		Side:       pkgorder.Long,
		Lots:       d(0.1),
		OrderType:  pkgorder.OrderTypeLimit,
		Intent:     pkgorder.OrderIntentOpen,
		LimitPrice: d(139.50), // tick1 で約定する
	})

	b.Advance()

	orders, _ := b.FetchOrders(context.Background())
	if len(orders) != 0 {
		t.Errorf("orders = %d, want 0 after fill", len(orders))
	}
}

func TestHistoricalBroker_FetchOrders_MarketOrderNotPending(t *testing.T) {
	b := newTestBroker(t)

	b.SubmitOrder(context.Background(), pkgorder.Order{
		Pair:      currency.USDJPY,
		Side:      pkgorder.Long,
		Lots:      d(0.1),
		OrderType: pkgorder.OrderTypeMarket,
		Intent:    pkgorder.OrderIntentOpen,
	})

	// 成行は即時約定なので pending には残らない
	orders, _ := b.FetchOrders(context.Background())
	if len(orders) != 0 {
		t.Errorf("orders = %d, want 0 (market order fills immediately)", len(orders))
	}
}

// --- FetchPositions（建玉管理）---

func TestHistoricalBroker_Position_NotClearedOnReRead(t *testing.T) {
	b := newTestBroker(t)

	b.SubmitOrder(context.Background(), pkgorder.Order{
		Pair:      currency.USDJPY,
		Side:      pkgorder.Long,
		Lots:      d(0.1),
		OrderType: pkgorder.OrderTypeMarket,
		Intent:    pkgorder.OrderIntentOpen,
	})

	b.FetchPositions(context.Background())
	positions, _ := b.FetchPositions(context.Background())
	if len(positions) != 1 {
		t.Errorf("positions = %d, want 1 on second fetch", len(positions))
	}
}

func TestHistoricalBroker_Position_Accumulates(t *testing.T) {
	b := newTestBroker(t)

	for range 3 {
		b.SubmitOrder(context.Background(), pkgorder.Order{
			Pair:      currency.USDJPY,
			Side:      pkgorder.Long,
			Lots:      d(0.1),
			OrderType: pkgorder.OrderTypeMarket,
			Intent:    pkgorder.OrderIntentOpen,
		})
	}

	positions, _ := b.FetchPositions(context.Background())
	if len(positions) != 3 {
		t.Errorf("positions = %d, want 3", len(positions))
	}
}

func TestHistoricalBroker_Position_IDUnique(t *testing.T) {
	b := newTestBroker(t)

	for range 2 {
		b.SubmitOrder(context.Background(), pkgorder.Order{
			Pair:      currency.USDJPY,
			Side:      pkgorder.Long,
			Lots:      d(0.1),
			OrderType: pkgorder.OrderTypeMarket,
			Intent:    pkgorder.OrderIntentOpen,
		})
	}

	positions, _ := b.FetchPositions(context.Background())
	if len(positions) != 2 {
		t.Fatalf("positions = %d, want 2", len(positions))
	}
	if positions[0].ID == positions[1].ID {
		t.Errorf("Position IDs should be unique, both = %q", positions[0].ID)
	}
}

// --- 決済（OrderIntentClose）---

func TestHistoricalBroker_Close_Market_RemovesPosition(t *testing.T) {
	b := newTestBroker(t)

	b.SubmitOrder(context.Background(), pkgorder.Order{
		Pair:      currency.USDJPY,
		Side:      pkgorder.Long,
		Lots:      d(0.1),
		OrderType: pkgorder.OrderTypeMarket,
		Intent:    pkgorder.OrderIntentOpen,
	})

	positions, _ := b.FetchPositions(context.Background())
	if len(positions) != 1 {
		t.Fatalf("positions after open = %d, want 1", len(positions))
	}
	posID := positions[0].ID

	_, err := b.SubmitOrder(context.Background(), pkgorder.Order{
		Pair:            currency.USDJPY,
		Side:            pkgorder.Short,
		Lots:            d(0.1),
		OrderType:       pkgorder.OrderTypeMarket,
		Intent:          pkgorder.OrderIntentClose,
		ClosePositionID: posID,
	})
	if err != nil {
		t.Fatalf("SubmitOrder (close): %v", err)
	}

	positions, _ = b.FetchPositions(context.Background())
	if len(positions) != 0 {
		t.Errorf("positions after close = %d, want 0", len(positions))
	}
}

func TestHistoricalBroker_Close_InvalidPositionID(t *testing.T) {
	b := newTestBroker(t)

	_, err := b.SubmitOrder(context.Background(), pkgorder.Order{
		Pair:            currency.USDJPY,
		Side:            pkgorder.Short,
		Lots:            d(0.1),
		OrderType:       pkgorder.OrderTypeMarket,
		Intent:          pkgorder.OrderIntentClose,
		ClosePositionID: "nonexistent-id",
	})
	if err == nil {
		t.Error("SubmitOrder with invalid ClosePositionID should return error")
	}
}

// --- FetchCandles ---

func TestHistoricalBroker_FetchCandles_ReturnsNewestFirst(t *testing.T) {
	b := newTestBroker(t)
	b.Advance() // tick1へ

	candles, err := b.FetchCandles(context.Background(), currency.USDJPY, "M1", 2)
	if err != nil {
		t.Fatalf("FetchCandles: %v", err)
	}
	if len(candles) != 2 {
		t.Fatalf("len = %d, want 2", len(candles))
	}
	if !candles[0].Close.Equal(d(141.00)) {
		t.Errorf("candles[0].Close = %v, want 141.00 (tick1)", candles[0].Close)
	}
	if !candles[1].Close.Equal(d(140.50)) {
		t.Errorf("candles[1].Close = %v, want 140.50 (tick0)", candles[1].Close)
	}
}

func TestHistoricalBroker_FetchCandles_CapAtAvailable(t *testing.T) {
	b := newTestBroker(t)

	candles, err := b.FetchCandles(context.Background(), currency.USDJPY, "M1", 10)
	if err != nil {
		t.Fatalf("FetchCandles: %v", err)
	}
	if len(candles) != 1 {
		t.Errorf("len = %d, want 1", len(candles))
	}
}

func TestHistoricalBroker_FetchCandles_UnsupportedPair(t *testing.T) {
	b := newTestBroker(t)
	_, err := b.FetchCandles(context.Background(), currency.EURUSD, "M1", 5)
	if err == nil {
		t.Error("FetchCandles with unsupported pair should return error")
	}
}
