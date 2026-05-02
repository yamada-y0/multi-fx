package currency

import (
	"time"

	"github.com/shopspring/decimal"
)

// Pair は通貨ペアを表す文字列型
type Pair string

const (
	USDJPY Pair = "USDJPY"
	EURUSD Pair = "EURUSD"
	GBPJPY Pair = "GBPJPY"
	EURJPY Pair = "EURJPY"
	AUDUSD Pair = "AUDUSD"
)

// Rate はある時点での通貨ペアのレート
type Rate struct {
	Pair      Pair
	Bid       decimal.Decimal
	Ask       decimal.Decimal
	Timestamp time.Time
}

// Mid は仲値（Bid と Ask の中間）を返す
func (r Rate) Mid() decimal.Decimal {
	return r.Bid.Add(r.Ask).Div(decimal.NewFromInt(2))
}
