package broker

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/shopspring/decimal"
	"github.com/yamada/multi-fx/internal/order"
	"github.com/yamada/multi-fx/internal/pool"
	"github.com/yamada/multi-fx/pkg/currency"
)

// OHLCVRow は Dukascopy CSV の1行
type OHLCVRow struct {
	Timestamp time.Time
	Open      decimal.Decimal
	High      decimal.Decimal
	Low       decimal.Decimal
	Close     decimal.Decimal
	Volume    decimal.Decimal
}

type historicalBroker struct {
	pair   currency.Pair
	rows   []OHLCVRow
	cursor int // 現在再生中のインデックス
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

	rows, err := parseCSV(f)
	if err != nil {
		return nil, fmt.Errorf("broker: parse csv: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("broker: csv is empty: %s", csvPath)
	}

	return &historicalBroker{pair: pair, rows: rows}, nil
}

func (b *historicalBroker) Name() string { return "historical" }

func (b *historicalBroker) CurrentTime() time.Time {
	return b.rows[b.cursor].Timestamp
}

// Advance は次のティックへ進める。終端に達したら false を返す。
func (b *historicalBroker) Advance() bool {
	if b.cursor+1 >= len(b.rows) {
		return false
	}
	b.cursor++
	return true
}

// FetchRate は現在ティックの Bid/Ask を返す（Dukascopy は bid のみなので Bid=Ask）
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

// PlaceOrder は現在ティックの Close 価格で即時約定する
func (b *historicalBroker) PlaceOrder(_ context.Context, o order.NetOrder) (pool.Fill, error) {
	row := b.rows[b.cursor]
	return pool.Fill{
		Pair:        o.Pair,
		Side:        o.Side,
		Lots:        o.NetLots,
		FilledPrice: row.Close,
		FilledAt:    row.Timestamp,
	}, nil
}

func parseCSV(r io.Reader) ([]OHLCVRow, error) {
	cr := csv.NewReader(r)

	// ヘッダー行をスキップ
	if _, err := cr.Read(); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	var rows []OHLCVRow
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}
		row, err := parseRow(rec)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func parseRow(rec []string) (OHLCVRow, error) {
	if len(rec) < 6 {
		return OHLCVRow{}, fmt.Errorf("unexpected columns: %d", len(rec))
	}

	tsMs, err := strconv.ParseInt(rec[0], 10, 64)
	if err != nil {
		return OHLCVRow{}, fmt.Errorf("parse timestamp %q: %w", rec[0], err)
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
		return OHLCVRow{}, err
	}
	high, err := parse(rec[2])
	if err != nil {
		return OHLCVRow{}, err
	}
	low, err := parse(rec[3])
	if err != nil {
		return OHLCVRow{}, err
	}
	close, err := parse(rec[4])
	if err != nil {
		return OHLCVRow{}, err
	}
	volume, err := parse(rec[5])
	if err != nil {
		return OHLCVRow{}, err
	}

	return OHLCVRow{
		Timestamp: time.UnixMilli(tsMs).UTC(),
		Open:      open,
		High:      high,
		Low:       low,
		Close:     close,
		Volume:    volume,
	}, nil
}
