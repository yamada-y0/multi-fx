package broker_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/yamada/multi-fx/internal/broker"
	"github.com/yamada/multi-fx/internal/pool"
	"github.com/yamada/multi-fx/pkg/currency"
)

// d は decimal.NewFromFloat の短縮ヘルパー
func d(f float64) decimal.Decimal { return decimal.NewFromFloat(f) }

var t0 = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// testRows は値が明確な固定OHLCVデータ
//
//	tick0: O=140.00 H=141.00 L=139.00 C=140.50
//	tick1: O=140.50 H=142.00 L=138.00 C=141.00
//	tick2: O=141.00 H=143.00 L=137.00 C=142.00
var testRows = []broker.OHLCVRow{
	{Timestamp: t0, Open: d(140.00), High: d(141.00), Low: d(139.00), Close: d(140.50)},
	{Timestamp: t0.Add(time.Hour), Open: d(140.50), High: d(142.00), Low: d(138.00), Close: d(141.00)},
	{Timestamp: t0.Add(2 * time.Hour), Open: d(141.00), High: d(143.00), Low: d(137.00), Close: d(142.00)},
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

	_, err := b.SubmitOrder(context.Background(), pool.OrderRequest{
		SubPoolID: "pool-a",
		Pair:      currency.USDJPY,
		Side:      pool.Long,
		Lots:      d(0.1),
		OrderType: pool.OrderTypeStop,
		StopLoss:  d(138.50), // tick1 Low=138.00 <= 138.50 → 約定
	})
	if err != nil {
		t.Fatalf("SubmitOrder: %v", err)
	}

	b.Advance()

	fills, err := b.FetchFills(context.Background())
	if err != nil {
		t.Fatalf("FetchFills: %v", err)
	}
	if len(fills) != 1 {
		t.Fatalf("fills = %d, want 1", len(fills))
	}
	if !fills[0].FilledPrice.Equal(d(138.50)) {
		t.Errorf("FilledPrice = %v, want 138.50", fills[0].FilledPrice)
	}
}

func TestHistoricalBroker_Stop_Long_NotFilled(t *testing.T) {
	b := newTestBroker(t)

	_, err := b.SubmitOrder(context.Background(), pool.OrderRequest{
		SubPoolID: "pool-a",
		Pair:      currency.USDJPY,
		Side:      pool.Long,
		Lots:      d(0.1),
		OrderType: pool.OrderTypeStop,
		StopLoss:  d(137.00), // tick1 Low=138.00 > 137.00 → 約定しない
	})
	if err != nil {
		t.Fatalf("SubmitOrder: %v", err)
	}

	b.Advance()

	fills, _ := b.FetchFills(context.Background())
	if len(fills) != 0 {
		t.Errorf("fills = %d, want 0", len(fills))
	}
}

func TestHistoricalBroker_Stop_Short_Filled(t *testing.T) {
	b := newTestBroker(t)

	_, err := b.SubmitOrder(context.Background(), pool.OrderRequest{
		SubPoolID: "pool-a",
		Pair:      currency.USDJPY,
		Side:      pool.Short,
		Lots:      d(0.1),
		OrderType: pool.OrderTypeStop,
		StopLoss:  d(141.50), // tick1 High=142.00 >= 141.50 → 約定
	})
	if err != nil {
		t.Fatalf("SubmitOrder: %v", err)
	}

	b.Advance()

	fills, _ := b.FetchFills(context.Background())
	if len(fills) != 1 {
		t.Fatalf("fills = %d, want 1", len(fills))
	}
	if !fills[0].FilledPrice.Equal(d(141.50)) {
		t.Errorf("FilledPrice = %v, want 141.50", fills[0].FilledPrice)
	}
}

// --- OrderTypeMarket（成行）---

func TestHistoricalBroker_Market_Long_FilledImmediately(t *testing.T) {
	b := newTestBroker(t)

	_, err := b.SubmitOrder(context.Background(), pool.OrderRequest{
		SubPoolID: "pool-a",
		Pair:      currency.USDJPY,
		Side:      pool.Long,
		Lots:      d(0.1),
		OrderType: pool.OrderTypeMarket,
		StopLoss:  d(135.00),
	})
	if err != nil {
		t.Fatalf("SubmitOrder: %v", err)
	}

	// Advance() 前に取得できる（即時約定）
	fills, err := b.FetchFills(context.Background())
	if err != nil {
		t.Fatalf("FetchFills: %v", err)
	}
	if len(fills) != 1 {
		t.Fatalf("fills = %d, want 1", len(fills))
	}
	// tick0 の Close=140.50 で約定
	if !fills[0].FilledPrice.Equal(d(140.50)) {
		t.Errorf("FilledPrice = %v, want 140.50", fills[0].FilledPrice)
	}
}

func TestHistoricalBroker_Market_Short_FilledImmediately(t *testing.T) {
	b := newTestBroker(t)

	_, err := b.SubmitOrder(context.Background(), pool.OrderRequest{
		SubPoolID: "pool-a",
		Pair:      currency.USDJPY,
		Side:      pool.Short,
		Lots:      d(0.1),
		OrderType: pool.OrderTypeMarket,
		StopLoss:  d(145.00),
	})
	if err != nil {
		t.Fatalf("SubmitOrder: %v", err)
	}

	fills, _ := b.FetchFills(context.Background())
	if len(fills) != 1 {
		t.Fatalf("fills = %d, want 1", len(fills))
	}
	if !fills[0].FilledPrice.Equal(d(140.50)) {
		t.Errorf("FilledPrice = %v, want 140.50", fills[0].FilledPrice)
	}
}

// --- OrderTypeLimit（指値）---

func TestHistoricalBroker_Limit_Long_Filled(t *testing.T) {
	b := newTestBroker(t)

	_, err := b.SubmitOrder(context.Background(), pool.OrderRequest{
		SubPoolID:  "pool-a",
		Pair:       currency.USDJPY,
		Side:       pool.Long,
		Lots:       d(0.1),
		OrderType:  pool.OrderTypeLimit,
		LimitPrice: d(139.50), // tick1 Low=138.00 <= 139.50 → 約定
		StopLoss:   d(135.00),
	})
	if err != nil {
		t.Fatalf("SubmitOrder: %v", err)
	}

	b.Advance()

	fills, _ := b.FetchFills(context.Background())
	if len(fills) != 1 {
		t.Fatalf("fills = %d, want 1", len(fills))
	}
	if !fills[0].FilledPrice.Equal(d(139.50)) {
		t.Errorf("FilledPrice = %v, want 139.50", fills[0].FilledPrice)
	}
}

func TestHistoricalBroker_Limit_Long_NotFilled(t *testing.T) {
	b := newTestBroker(t)

	_, err := b.SubmitOrder(context.Background(), pool.OrderRequest{
		SubPoolID:  "pool-a",
		Pair:       currency.USDJPY,
		Side:       pool.Long,
		Lots:       d(0.1),
		OrderType:  pool.OrderTypeLimit,
		LimitPrice: d(137.50), // tick1 Low=138.00 > 137.50 → 約定しない
		StopLoss:   d(135.00),
	})
	if err != nil {
		t.Fatalf("SubmitOrder: %v", err)
	}

	b.Advance()

	fills, _ := b.FetchFills(context.Background())
	if len(fills) != 0 {
		t.Errorf("fills = %d, want 0", len(fills))
	}
}

func TestHistoricalBroker_Limit_Long_ExactPrice(t *testing.T) {
	b := newTestBroker(t)

	_, err := b.SubmitOrder(context.Background(), pool.OrderRequest{
		SubPoolID:  "pool-a",
		Pair:       currency.USDJPY,
		Side:       pool.Long,
		Lots:       d(0.1),
		OrderType:  pool.OrderTypeLimit,
		LimitPrice: d(138.00), // tick1 Low=138.00 と一致 → 約定する
		StopLoss:   d(135.00),
	})
	if err != nil {
		t.Fatalf("SubmitOrder: %v", err)
	}

	b.Advance()

	fills, _ := b.FetchFills(context.Background())
	if len(fills) != 1 {
		t.Fatalf("fills = %d, want 1 (exact price should fill)", len(fills))
	}
}

func TestHistoricalBroker_Limit_Short_Filled(t *testing.T) {
	b := newTestBroker(t)

	_, err := b.SubmitOrder(context.Background(), pool.OrderRequest{
		SubPoolID:  "pool-a",
		Pair:       currency.USDJPY,
		Side:       pool.Short,
		Lots:       d(0.1),
		OrderType:  pool.OrderTypeLimit,
		LimitPrice: d(141.50), // tick1 High=142.00 >= 141.50 → 約定
		StopLoss:   d(145.00),
	})
	if err != nil {
		t.Fatalf("SubmitOrder: %v", err)
	}

	b.Advance()

	fills, _ := b.FetchFills(context.Background())
	if len(fills) != 1 {
		t.Fatalf("fills = %d, want 1", len(fills))
	}
	if !fills[0].FilledPrice.Equal(d(141.50)) {
		t.Errorf("FilledPrice = %v, want 141.50", fills[0].FilledPrice)
	}
}

func TestHistoricalBroker_Limit_Short_NotFilled(t *testing.T) {
	b := newTestBroker(t)

	_, err := b.SubmitOrder(context.Background(), pool.OrderRequest{
		SubPoolID:  "pool-a",
		Pair:       currency.USDJPY,
		Side:       pool.Short,
		Lots:       d(0.1),
		OrderType:  pool.OrderTypeLimit,
		LimitPrice: d(142.50), // tick1 High=142.00 < 142.50 → 約定しない
		StopLoss:   d(145.00),
	})
	if err != nil {
		t.Fatalf("SubmitOrder: %v", err)
	}

	b.Advance()

	fills, _ := b.FetchFills(context.Background())
	if len(fills) != 0 {
		t.Errorf("fills = %d, want 0", len(fills))
	}
}

func TestHistoricalBroker_Limit_CancelBeforeFill(t *testing.T) {
	b := newTestBroker(t)

	id, err := b.SubmitOrder(context.Background(), pool.OrderRequest{
		SubPoolID:  "pool-a",
		Pair:       currency.USDJPY,
		Side:       pool.Long,
		Lots:       d(0.1),
		OrderType:  pool.OrderTypeLimit,
		LimitPrice: d(139.50), // 刺さるはずだがキャンセルする
		StopLoss:   d(135.00),
	})
	if err != nil {
		t.Fatalf("SubmitOrder: %v", err)
	}

	if err := b.CancelOrder(context.Background(), id); err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}

	b.Advance()

	fills, _ := b.FetchFills(context.Background())
	if len(fills) != 0 {
		t.Errorf("fills = %d, want 0 after cancel", len(fills))
	}
}

// --- FetchFills の既読クリア ---

func TestHistoricalBroker_FetchFills_ClearsAfterRead(t *testing.T) {
	b := newTestBroker(t)

	b.SubmitOrder(context.Background(), pool.OrderRequest{
		SubPoolID: "pool-a",
		Pair:      currency.USDJPY,
		Side:      pool.Long,
		Lots:      d(0.1),
		OrderType: pool.OrderTypeStop,
		StopLoss:  d(138.50),
	})
	b.Advance()

	b.FetchFills(context.Background())                 // 1回目で取得
	fills, _ := b.FetchFills(context.Background()) // 2回目は空のはず
	if len(fills) != 0 {
		t.Errorf("fills = %d, want 0 on second fetch", len(fills))
	}
}

// --- FetchPositions（建玉管理）---

func TestHistoricalBroker_Position_CreatedOnMarketFill(t *testing.T) {
	b := newTestBroker(t)

	b.SubmitOrder(context.Background(), pool.OrderRequest{
		SubPoolID: "pool-a",
		Pair:      currency.USDJPY,
		Side:      pool.Long,
		Lots:      d(0.1),
		OrderType: pool.OrderTypeMarket,
		StopLoss:  d(135.00),
	})

	positions, err := b.FetchPositions(context.Background())
	if err != nil {
		t.Fatalf("FetchPositions: %v", err)
	}
	if len(positions) != 1 {
		t.Fatalf("positions = %d, want 1", len(positions))
	}
	if positions[0].Side != pool.Long {
		t.Errorf("Side = %v, want Long", positions[0].Side)
	}
	if !positions[0].OpenPrice.Equal(d(140.50)) {
		t.Errorf("OpenPrice = %v, want 140.50", positions[0].OpenPrice)
	}
}

func TestHistoricalBroker_Position_CreatedOnLimitFill(t *testing.T) {
	b := newTestBroker(t)

	b.SubmitOrder(context.Background(), pool.OrderRequest{
		SubPoolID:  "pool-a",
		Pair:       currency.USDJPY,
		Side:       pool.Long,
		Lots:       d(0.1),
		OrderType:  pool.OrderTypeLimit,
		LimitPrice: d(139.50),
		StopLoss:   d(135.00),
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

func TestHistoricalBroker_Position_Accumulates(t *testing.T) {
	b := newTestBroker(t)

	for range 3 {
		b.SubmitOrder(context.Background(), pool.OrderRequest{
			SubPoolID: "pool-a",
			Pair:      currency.USDJPY,
			Side:      pool.Long,
			Lots:      d(0.1),
			OrderType: pool.OrderTypeMarket,
			StopLoss:  d(135.00),
		})
	}

	positions, _ := b.FetchPositions(context.Background())
	if len(positions) != 3 {
		t.Errorf("positions = %d, want 3", len(positions))
	}
}

func TestHistoricalBroker_Position_NotClearedOnReRead(t *testing.T) {
	b := newTestBroker(t)

	b.SubmitOrder(context.Background(), pool.OrderRequest{
		SubPoolID: "pool-a",
		Pair:      currency.USDJPY,
		Side:      pool.Long,
		Lots:      d(0.1),
		OrderType: pool.OrderTypeMarket,
		StopLoss:  d(135.00),
	})

	b.FetchPositions(context.Background()) // 1回目
	positions, _ := b.FetchPositions(context.Background()) // 2回目もクリアされない
	if len(positions) != 1 {
		t.Errorf("positions = %d, want 1 on second fetch", len(positions))
	}
}

func TestHistoricalBroker_Position_IDUnique(t *testing.T) {
	b := newTestBroker(t)

	for range 2 {
		b.SubmitOrder(context.Background(), pool.OrderRequest{
			SubPoolID: "pool-a",
			Pair:      currency.USDJPY,
			Side:      pool.Long,
			Lots:      d(0.1),
			OrderType: pool.OrderTypeMarket,
			StopLoss:  d(135.00),
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

	// 新規建て
	_, err := b.SubmitOrder(context.Background(), pool.OrderRequest{
		SubPoolID:   "pool-a",
		Pair:        currency.USDJPY,
		Side:        pool.Long,
		Lots:        d(0.1),
		OrderType:   pool.OrderTypeMarket,
		OrderIntent: pool.OrderIntentOpen,
		StopLoss:    d(135.00),
	})
	if err != nil {
		t.Fatalf("SubmitOrder (open): %v", err)
	}
	b.FetchFills(context.Background())

	positions, _ := b.FetchPositions(context.Background())
	if len(positions) != 1 {
		t.Fatalf("positions after open = %d, want 1", len(positions))
	}
	posID := positions[0].ID

	// 決済
	_, err = b.SubmitOrder(context.Background(), pool.OrderRequest{
		SubPoolID:       "pool-a",
		Pair:            currency.USDJPY,
		Side:            pool.Short, // Long を閉じるので逆方向
		Lots:            d(0.1),
		OrderType:       pool.OrderTypeMarket,
		OrderIntent:     pool.OrderIntentClose,
		StopLoss:        d(145.00),
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

func TestHistoricalBroker_Close_Fill_HasCloseIntent(t *testing.T) {
	b := newTestBroker(t)

	b.SubmitOrder(context.Background(), pool.OrderRequest{
		SubPoolID:   "pool-a",
		Pair:        currency.USDJPY,
		Side:        pool.Long,
		Lots:        d(0.1),
		OrderType:   pool.OrderTypeMarket,
		OrderIntent: pool.OrderIntentOpen,
		StopLoss:    d(135.00),
	})
	b.FetchFills(context.Background())

	positions, _ := b.FetchPositions(context.Background())
	posID := positions[0].ID

	b.SubmitOrder(context.Background(), pool.OrderRequest{
		SubPoolID:       "pool-a",
		Pair:            currency.USDJPY,
		Side:            pool.Short,
		Lots:            d(0.1),
		OrderType:       pool.OrderTypeMarket,
		OrderIntent:     pool.OrderIntentClose,
		StopLoss:        d(145.00),
		ClosePositionID: posID,
	})

	fills, _ := b.FetchFills(context.Background())
	if len(fills) != 1 {
		t.Fatalf("fills = %d, want 1", len(fills))
	}
	if fills[0].Intent != pool.OrderIntentClose {
		t.Errorf("Intent = %v, want Close", fills[0].Intent)
	}
	if fills[0].ClosePositionID != posID {
		t.Errorf("ClosePositionID = %q, want %q", fills[0].ClosePositionID, posID)
	}
}

func TestHistoricalBroker_Close_InvalidPositionID(t *testing.T) {
	b := newTestBroker(t)

	_, err := b.SubmitOrder(context.Background(), pool.OrderRequest{
		SubPoolID:       "pool-a",
		Pair:            currency.USDJPY,
		Side:            pool.Short,
		Lots:            d(0.1),
		OrderType:       pool.OrderTypeMarket,
		OrderIntent:     pool.OrderIntentClose,
		StopLoss:        d(145.00),
		ClosePositionID: "nonexistent-id",
	})
	if err == nil {
		t.Error("SubmitOrder with invalid ClosePositionID should return error")
	}
}
