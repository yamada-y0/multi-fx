package broker

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/yamada/multi-fx/internal/pool"
	"github.com/yamada/multi-fx/pkg/currency"
	"github.com/yamada/multi-fx/pkg/market"
)

type pendingOrder struct {
	id  OrderID
	req pool.OrderRequest
}

type historicalBroker struct {
	pair      currency.Pair
	rows      []market.Candle
	cursor    int
	pending   []pendingOrder
	fills     []pool.Fill
	positions map[string]pool.Position // key: PositionID
}

// NewHistoricalBroker は Dukascopy 形式の CSV を読み込んで HistoricalBroker を返す
//
// CSV フォーマット（dukascopy-node 出力）:
//
//	timestamp,open,high,low,close,volume
//	1704146400000,140.843,140.961,140.839,140.875,577.88
//
// timestamp は Unix ミリ秒。price-type は bid 固定を想定。
func NewHistoricalBroker(pair currency.Pair, csvPath string) (HistoricalBroker, error) {
	f, err := os.Open(csvPath)
	if err != nil {
		return nil, fmt.Errorf("broker: open csv: %w", err)
	}
	defer f.Close()

	rows, err := parseCSV(pair, f)
	if err != nil {
		return nil, fmt.Errorf("broker: parse csv: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("broker: csv is empty: %s", csvPath)
	}

	return newHistoricalBroker(pair, rows), nil
}

// NewHistoricalBrokerFromRows は market.Candle のスライスから HistoricalBroker を返す（テスト用）
func NewHistoricalBrokerFromRows(pair currency.Pair, rows []market.Candle) (HistoricalBroker, error) {
	if len(rows) == 0 {
		return nil, fmt.Errorf("broker: rows is empty")
	}
	return newHistoricalBroker(pair, rows), nil
}

func newHistoricalBroker(pair currency.Pair, rows []market.Candle) *historicalBroker {
	return &historicalBroker{pair: pair, rows: rows, positions: make(map[string]pool.Position)}
}

func (b *historicalBroker) Name() string { return "historical" }

func (b *historicalBroker) CurrentTime() time.Time {
	return b.rows[b.cursor].Timestamp
}

// Advance は次のティックへ進め、PENDING オーダーの約定判定を行う
func (b *historicalBroker) Advance() bool {
	if b.cursor+1 >= len(b.rows) {
		return false
	}
	b.cursor++
	b.evaluatePending()
	return true
}

// evaluatePending は現在ティックの High/Low で PENDING オーダーを約定判定する
func (b *historicalBroker) evaluatePending() {
	row := b.rows[b.cursor]
	remaining := b.pending[:0]

	for _, o := range b.pending {
		if b.isFilled(o.req, row) {
			price := b.fillPrice(o.req)
			b.recordFill(o.id, o.req, price, row.Timestamp)
		} else {
			remaining = append(remaining, o)
		}
	}
	b.pending = remaining
}

// isFilled はオーダーがこのティックで約定するかを OrderType ごとに判定する
func (b *historicalBroker) isFilled(req pool.OrderRequest, row market.Candle) bool {
	switch req.OrderType {
	case pool.OrderTypeStop:
		switch req.Side {
		case pool.Long:
			return row.Low.LessThanOrEqual(req.StopLoss)
		case pool.Short:
			return row.High.GreaterThanOrEqual(req.StopLoss)
		}
	case pool.OrderTypeLimit:
		switch req.Side {
		case pool.Long:
			// 指値買い: 価格が LimitPrice 以下に下がったら約定
			return row.Low.LessThanOrEqual(req.LimitPrice)
		case pool.Short:
			// 指値売り: 価格が LimitPrice 以上に上がったら約定
			return row.High.GreaterThanOrEqual(req.LimitPrice)
		}
	}
	return false
}

// fillPrice は OrderType に応じた約定価格を返す
func (b *historicalBroker) fillPrice(req pool.OrderRequest) decimal.Decimal {
	switch req.OrderType {
	case pool.OrderTypeStop:
		return req.StopLoss
	case pool.OrderTypeLimit:
		return req.LimitPrice
	}
	return b.rows[b.cursor].Close
}

// recordFill は約定 Fill を記録し、新規/決済に応じてポジションを追加/削除する
func (b *historicalBroker) recordFill(id OrderID, req pool.OrderRequest, price decimal.Decimal, ts time.Time) {
	fill := pool.Fill{
		RequestID:       string(id),
		Pair:            req.Pair,
		Side:            req.Side,
		Lots:            req.Lots,
		FilledPrice:     price,
		FilledAt:        ts,
		Intent:          req.OrderIntent,
		ClosePositionID: req.ClosePositionID,
	}
	b.fills = append(b.fills, fill)

	switch req.OrderIntent {
	case pool.OrderIntentOpen:
		pos := pool.Position{
			ID:        uuid.New().String(),
			Pair:      req.Pair,
			Side:      req.Side,
			Lots:      req.Lots,
			OpenPrice: price,
			OpenedAt:  ts,
		}
		b.positions[pos.ID] = pos
	case pool.OrderIntentClose:
		delete(b.positions, req.ClosePositionID)
	}
}

func (b *historicalBroker) SubmitOrder(_ context.Context, req pool.OrderRequest) (OrderID, error) {
	if req.OrderIntent == pool.OrderIntentClose {
		if err := b.validateCloseRequest(req); err != nil {
			return "", err
		}
	}

	id := OrderID(uuid.New().String())

	// 成行は即時約定
	if req.OrderType == pool.OrderTypeMarket {
		row := b.rows[b.cursor]
		b.recordFill(id, req, row.Close, row.Timestamp)
		return id, nil
	}

	b.pending = append(b.pending, pendingOrder{id: id, req: req})
	return id, nil
}

// validateCloseRequest は決済注文の事前検証（ClosePositionID が存在するか）
func (b *historicalBroker) validateCloseRequest(req pool.OrderRequest) error {
	if _, ok := b.positions[req.ClosePositionID]; !ok {
		return fmt.Errorf("broker: position not found: %s", req.ClosePositionID)
	}
	return nil
}

func (b *historicalBroker) FetchFills(_ context.Context) ([]pool.Fill, error) {
	fills := b.fills
	b.fills = nil
	return fills, nil
}

func (b *historicalBroker) CancelOrder(_ context.Context, id OrderID) error {
	for i, o := range b.pending {
		if o.id == id {
			b.pending = append(b.pending[:i], b.pending[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("broker: order not found: %s", id)
}

func (b *historicalBroker) FetchPositions(_ context.Context) ([]pool.Position, error) {
	result := make([]pool.Position, 0, len(b.positions))
	for _, p := range b.positions {
		result = append(result, p)
	}
	return result, nil
}

func (b *historicalBroker) FetchRate(_ context.Context, pair currency.Pair) (currency.Rate, error) {
	if pair != b.pair {
		return currency.Rate{}, fmt.Errorf("broker: unsupported pair: %s", pair)
	}
	row := b.rows[b.cursor]
	return currency.Rate{
		Pair:      b.pair,
		Bid:       row.Close,
		Ask:       row.Close,
		Timestamp: row.Timestamp,
	}, nil
}

func parseCSV(pair currency.Pair, r io.Reader) ([]market.Candle, error) {
	cr := csv.NewReader(r)

	// ヘッダー行をスキップ
	if _, err := cr.Read(); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	var rows []market.Candle
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}
		row, err := parseRow(pair, rec)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func parseRow(pair currency.Pair, rec []string) (market.Candle, error) {
	if len(rec) < 6 {
		return market.Candle{}, fmt.Errorf("unexpected columns: %d", len(rec))
	}

	tsMs, err := strconv.ParseInt(rec[0], 10, 64)
	if err != nil {
		return market.Candle{}, fmt.Errorf("parse timestamp %q: %w", rec[0], err)
	}

	parse := func(s string) (decimal.Decimal, error) {
		d, err := decimal.NewFromString(s)
		if err != nil {
			return decimal.Zero, fmt.Errorf("parse decimal %q: %w", s, err)
		}
		return d, nil
	}

	open, err := parse(rec[1])
	if err != nil {
		return market.Candle{}, err
	}
	high, err := parse(rec[2])
	if err != nil {
		return market.Candle{}, err
	}
	low, err := parse(rec[3])
	if err != nil {
		return market.Candle{}, err
	}
	close, err := parse(rec[4])
	if err != nil {
		return market.Candle{}, err
	}
	volume, err := parse(rec[5])
	if err != nil {
		return market.Candle{}, err
	}

	return market.Candle{
		Timestamp: time.UnixMilli(tsMs).UTC(),
		Pair:      pair,
		Open:      open,
		High:      high,
		Low:       low,
		Close:     close,
		Volume:    volume,
	}, nil
}
