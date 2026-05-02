package currency

import "time"

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
	Bid       float64
	Ask       float64
	Timestamp time.Time
}

// Mid は仲値（Bid と Ask の中間）を返す
func (r Rate) Mid() float64 {
	return (r.Bid + r.Ask) / 2
}
