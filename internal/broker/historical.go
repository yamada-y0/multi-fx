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
	"github.com/yamada/fxd/pkg/currency"
	"github.com/yamada/fxd/pkg/market"
	pkgorder "github.com/yamada/fxd/pkg/order"
)

type pendingOrder struct {
	id    OrderID
	order pkgorder.Order
}

type historicalBroker struct {
	pair        currency.Pair
	rows        []market.Candle
	cursor      int
	pending     []pendingOrder
	positions   map[string]pkgorder.Position // key: PositionID
	fillEvents  []pkgorder.FillEvent         // 約定イベントログ（追記のみ）
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
	return &historicalBroker{pair: pair, rows: rows, positions: make(map[string]pkgorder.Position)}
}

func (b *historicalBroker) Name() string { return "historical" }

func (b *historicalBroker) Snapshot() HistoricalBrokerSnapshot {
	pending := make([]PendingOrderSnapshot, len(b.pending))
	for i, p := range b.pending {
		pending[i] = PendingOrderSnapshot{ID: string(p.id), Order: p.order}
	}
	positions := make([]pkgorder.Position, 0, len(b.positions))
	for _, pos := range b.positions {
		positions = append(positions, pos)
	}
	lastID := ""
	if len(b.fillEvents) > 0 {
		lastID = b.fillEvents[len(b.fillEvents)-1].ID
	}
	return HistoricalBrokerSnapshot{
		Cursor:          b.cursor,
		Pending:         pending,
		Positions:       positions,
		LastFillEventID: lastID,
	}
}

func (b *historicalBroker) Restore(snap HistoricalBrokerSnapshot) {
	b.cursor = snap.Cursor
	b.pending = make([]pendingOrder, len(snap.Pending))
	for i, p := range snap.Pending {
		b.pending[i] = pendingOrder{id: OrderID(p.ID), order: p.Order}
	}
	b.positions = make(map[string]pkgorder.Position, len(snap.Positions))
	for _, pos := range snap.Positions {
		b.positions[pos.ID] = pos
	}
	// fillEvents はプロセス内メモリにのみ存在する。
	// 再起動後は LastFillEventID を sinceID として FetchFillEvents を呼ぶことで
	// 取りこぼしなく再開できる（Aggregator が sinceID を別途永続化する）。
}

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
		if b.isFilled(o.order, row) {
			price := b.fillPrice(o.order)
			b.recordFill(o.id, o.order, price, row.Timestamp)
		} else {
			remaining = append(remaining, o)
		}
	}
	b.pending = remaining
}

// isFilled はオーダーがこのティックで約定するかを OrderType ごとに判定する
func (b *historicalBroker) isFilled(o pkgorder.Order, row market.Candle) bool {
	switch o.OrderType {
	case pkgorder.OrderTypeStop:
		switch o.Side {
		case pkgorder.Long:
			return row.Low.LessThanOrEqual(o.StopLoss)
		case pkgorder.Short:
			return row.High.GreaterThanOrEqual(o.StopLoss)
		}
	case pkgorder.OrderTypeLimit:
		switch o.Side {
		case pkgorder.Long:
			return row.Low.LessThanOrEqual(o.LimitPrice)
		case pkgorder.Short:
			return row.High.GreaterThanOrEqual(o.LimitPrice)
		}
	}
	return false
}

// fillPrice は OrderType に応じた約定価格を返す
func (b *historicalBroker) fillPrice(o pkgorder.Order) decimal.Decimal {
	switch o.OrderType {
	case pkgorder.OrderTypeStop:
		return o.StopLoss
	case pkgorder.OrderTypeLimit:
		return o.LimitPrice
	}
	return b.rows[b.cursor].Close
}

// recordFill はポジションを更新し FillEvent をログに追記する
func (b *historicalBroker) recordFill(id OrderID, o pkgorder.Order, price decimal.Decimal, ts time.Time) {
	eventID := fmt.Sprintf("%d", len(b.fillEvents)+1)
	positionID := o.ClosePositionID

	switch o.Intent {
	case pkgorder.OrderIntentOpen:
		positionID = uuid.New().String()
		b.positions[positionID] = pkgorder.Position{
			ID:        positionID,
			Pair:      o.Pair,
			Side:      o.Side,
			Lots:      o.Lots,
			OpenPrice: price,
			OpenedAt:  ts,
		}
	case pkgorder.OrderIntentClose:
		delete(b.positions, o.ClosePositionID)
	}

	b.fillEvents = append(b.fillEvents, pkgorder.FillEvent{
		ID:          eventID,
		OrderID:     string(id),
		PositionID:  positionID,
		Intent:      o.Intent,
		Pair:        o.Pair,
		Side:        o.Side,
		Lots:        o.Lots,
		FilledPrice: price,
		FilledAt:    ts,
	})
}

func (b *historicalBroker) SubmitOrder(_ context.Context, o pkgorder.Order) (OrderID, error) {
	if o.Intent == pkgorder.OrderIntentClose {
		if _, ok := b.positions[o.ClosePositionID]; !ok {
			return "", fmt.Errorf("broker: position not found: %s", o.ClosePositionID)
		}
	}

	id := OrderID(uuid.New().String())

	// 成行は即時約定
	if o.OrderType == pkgorder.OrderTypeMarket {
		row := b.rows[b.cursor]
		b.recordFill(id, o, row.Close, row.Timestamp)
		return id, nil
	}

	b.pending = append(b.pending, pendingOrder{id: id, order: o})
	return id, nil
}

func (b *historicalBroker) FetchOrders(_ context.Context) ([]pkgorder.PendingOrder, error) {
	result := make([]pkgorder.PendingOrder, len(b.pending))
	for i, p := range b.pending {
		result[i] = pkgorder.PendingOrder{ID: string(p.id), Order: p.order}
	}
	return result, nil
}

// FetchFillEvents は sinceID より新しい約定イベントを古い順で返す
func (b *historicalBroker) FetchFillEvents(_ context.Context, sinceID string) ([]pkgorder.FillEvent, error) {
	if sinceID == "" {
		return append([]pkgorder.FillEvent{}, b.fillEvents...), nil
	}
	for i, e := range b.fillEvents {
		if e.ID == sinceID {
			return append([]pkgorder.FillEvent{}, b.fillEvents[i+1:]...), nil
		}
	}
	// sinceID が見つからない場合は全件返す
	return append([]pkgorder.FillEvent{}, b.fillEvents...), nil
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

func (b *historicalBroker) FetchPositions(_ context.Context) ([]pkgorder.Position, error) {
	result := make([]pkgorder.Position, 0, len(b.positions))
	for _, p := range b.positions {
		result = append(result, p)
	}
	return result, nil
}

// FetchCandles は直近 count 本のローソク足を新しい順で返す。
// granularity はHistoricalBrokerでは無視される（CSVの粒度固定）。
func (b *historicalBroker) FetchCandles(_ context.Context, pair currency.Pair, _ string, count int) ([]market.Candle, error) {
	if pair != b.pair {
		return nil, fmt.Errorf("broker: unsupported pair: %s", pair)
	}
	start := b.cursor - count + 1
	if start < 0 {
		start = 0
	}
	src := b.rows[start : b.cursor+1]
	result := make([]market.Candle, len(src))
	for i, c := range src {
		result[len(src)-1-i] = c // 新しい順に並び替え
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
