package market

import (
	"fmt"

	"github.com/yamada/multi-fx/pkg/currency"
	pkgmarket "github.com/yamada/multi-fx/pkg/market"
)

// CandleFetcher は直近N本の足データを取得できるソースを抽象化する
// HistoricalBroker や本番APIラッパーが実装する
type CandleFetcher interface {
	FetchCandles(pair currency.Pair, n int) ([]pkgmarket.Candle, error)
}

// Provider は Agent に MarketContext を提供する
type Provider struct {
	fetcher CandleFetcher
	n       int // 取得する足の本数
}

func NewProvider(fetcher CandleFetcher, n int) *Provider {
	return &Provider{fetcher: fetcher, n: n}
}

// Fetch は指定ペアの MarketContext を組み立てて返す
func (p *Provider) Fetch(pair currency.Pair) (pkgmarket.MarketContext, error) {
	candles, err := p.fetcher.FetchCandles(pair, p.n)
	if err != nil {
		return pkgmarket.MarketContext{}, fmt.Errorf("market provider: %w", err)
	}
	ctx := pkgmarket.MarketContext{
		Candles: map[currency.Pair][]pkgmarket.Candle{
			pair: candles,
		},
	}
	if len(candles) > 0 {
		ctx.Timestamp = candles[0].Timestamp
	}
	return ctx, nil
}
