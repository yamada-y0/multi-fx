package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/yamada/fxd/cmd/common"
	"github.com/yamada/fxd/internal/broker"
	"github.com/yamada/fxd/pkg/currency"

	"github.com/shopspring/decimal"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: fxd <command> [options]")
		fmt.Fprintln(os.Stderr, "Commands: snapshot, market, submit-order, cancel-order, set-wakeup, note, notify, init-agent")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "snapshot":
		runSnapshot(os.Args[2:])
	case "market":
		runMarket(os.Args[2:])
	case "submit-order":
		runSubmitOrder(os.Args[2:])
	case "cancel-order":
		runCancelOrder(os.Args[2:])
	case "set-wakeup":
		runSetWakeup(os.Args[2:])
	case "note":
		runNote(os.Args[2:])
	case "notify":
		runNotify(os.Args[2:])
	case "init-agent":
		runInitAgent(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

func setupBrokers(stateDir, csvPath string, pair currency.Pair) (broker.TradingBroker, broker.MarketBroker, broker.HistoricalBroker, error) {
	if csvPath != "" {
		hb, err := common.RestoreHistoricalBroker(stateDir, csvPath, pair)
		if err != nil {
			return nil, nil, nil, err
		}
		return hb, hb, hb, nil
	}
	tb, mb, err := common.SetupOandaBrokers()
	if err != nil {
		return nil, nil, nil, err
	}
	return tb, mb, nil, nil
}

func parsePairPrice(s string) (currency.Pair, decimal.Decimal, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return "", decimal.Zero, fmt.Errorf("expected PAIR:PRICE format, got %q", s)
	}
	price, err := decimal.NewFromString(parts[1])
	if err != nil {
		return "", decimal.Zero, fmt.Errorf("invalid price %q: %w", parts[1], err)
	}
	return currency.Pair(parts[0]), price, nil
}

func mustDecimal(f float64) decimal.Decimal {
	return decimal.NewFromFloat(f)
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		log.Fatalf("json encode: %v", err)
	}
}
