package common

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/yamada/fxd/internal/broker"
	"github.com/yamada/fxd/pkg/currency"
)

// SetupOandaBrokers は取引用 TradingBroker と市場データ用 MarketBroker を返す。
// OANDA_MARKET_API_TOKEN / OANDA_MARKET_PRACTICE が設定されていればlive環境のMarketBrokerを使う。
func SetupOandaBrokers() (broker.TradingBroker, broker.MarketBroker, error) {
	token := os.Getenv("OANDA_API_TOKEN")
	accountID := os.Getenv("OANDA_ACCOUNT_ID")
	if token == "" {
		return nil, nil, fmt.Errorf("OANDA_API_TOKEN is not set")
	}
	if accountID == "" {
		return nil, nil, fmt.Errorf("OANDA_ACCOUNT_ID is not set")
	}
	practice := os.Getenv("OANDA_PRACTICE") != "false"
	tb := broker.NewOandaTradingBroker(token, accountID, practice)

	marketToken := os.Getenv("OANDA_MARKET_API_TOKEN")
	marketPractice := os.Getenv("OANDA_MARKET_PRACTICE") != "false"
	var mb broker.MarketBroker
	if marketToken != "" {
		mb = broker.NewOandaMarketBroker(marketToken, marketPractice)
	} else {
		mb = broker.NewOandaMarketBroker(token, practice)
	}
	return tb, mb, nil
}

// RestoreHistoricalBroker は CSV から HistoricalBroker を復元する。
func RestoreHistoricalBroker(stateDir, csvPath string, pair currency.Pair) (broker.HistoricalBroker, error) {
	b, err := broker.NewHistoricalBroker(pair, csvPath)
	if err != nil {
		return nil, fmt.Errorf("historical broker: %w", err)
	}
	snapPath := filepath.Join(stateDir, "broker_snapshot.json")
	snap, err := broker.LoadHistoricalBrokerSnapshot(snapPath)
	if err != nil {
		return nil, fmt.Errorf("load broker snapshot: %w", err)
	}
	b.Restore(snap)
	return b, nil
}
