package market

import (
	"time"

	"github.com/shopspring/decimal"
	"github.com/yamada/multi-fx/pkg/currency"
)

// Candle はOHLCVの1本の足データ
type Candle struct {
	Timestamp time.Time
	Pair      currency.Pair
	Open      decimal.Decimal
	High      decimal.Decimal
	Low       decimal.Decimal
	Close     decimal.Decimal
	Volume    decimal.Decimal
}

// MarketContext はAgentが発注判断に使う市場情報
type MarketContext struct {
	Timestamp time.Time
	Candles   map[currency.Pair][]Candle // 直近N本（新しい順）
}

// Latest は指定ペアの最新足を返す。存在しない場合は false。
func (m MarketContext) Latest(pair currency.Pair) (Candle, bool) {
	candles, ok := m.Candles[pair]
	if !ok || len(candles) == 0 {
		return Candle{}, false
	}
	return candles[0], true
}
