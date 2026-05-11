package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/shopspring/decimal"
	"github.com/yamada/fxd/pkg/currency"
	"github.com/yamada/fxd/pkg/indicator"
	pkgmarket "github.com/yamada/fxd/pkg/market"
	pkgorder "github.com/yamada/fxd/pkg/order"
)

func runMarket(args []string) {
	fs := flag.NewFlagSet("market", flag.ExitOnError)
	stateDir := fs.String("state-dir", "", "エージェントディレクトリパス（必須）")
	csvPath := fs.String("data", "", "historical モード: Dukascopy CSVファイルパス")
	pair := fs.String("pair", "USDJPY", "通貨ペア")
	n := fs.Int("n", 20, "取得するローソク足の本数")
	granularity := fs.String("granularity", "M1", "ローソク足の粒度（OANDA形式: M1/M5/H1 等）")
	fs.Parse(args)

	if *stateDir == "" {
		fmt.Fprintln(os.Stderr, "Usage: fxd market --state-dir <dir> [--data <csv>] [--pair <pair>] [--n <count>] [--granularity <M1|M5|H1>]")
		os.Exit(1)
	}

	ctx := context.Background()
	pairVal := currency.Pair(*pair)

	_, mb, hb, err := setupBrokers(*stateDir, *csvPath, pairVal)
	if err != nil {
		log.Fatalf("setup broker: %v", err)
	}

	candles, err := mb.FetchCandles(ctx, pairVal, *granularity, *n)
	if err != nil {
		log.Fatalf("fetch candles: %v", err)
	}

	rate, err := mb.FetchRate(ctx, pairVal)
	if err != nil {
		log.Fatalf("fetch rate: %v", err)
	}

	jst := time.FixedZone("JST", 9*60*60)
	var currentTime time.Time
	if hb != nil {
		currentTime = hb.CurrentTime().In(jst)
	} else {
		currentTime = time.Now().In(jst)
	}

	type marketOutput struct {
		CurrentTime time.Time                `json:"CurrentTime"`
		Bid         decimal.Decimal          `json:"Bid"`
		Ask         decimal.Decimal          `json:"Ask"`
		Candles     []pkgmarket.Candle       `json:"Candles"`
		Indicators  indicator.Result         `json:"Indicators"`
		Calendar    []pkgorder.CalendarEvent `json:"Calendar,omitempty"`
	}

	out := marketOutput{
		CurrentTime: currentTime,
		Bid:         rate.Bid,
		Ask:         rate.Ask,
		Candles:     candles,
		Indicators:  indicator.Calc(candles),
	}

	// historicalモードではカレンダーは取得しない
	if hb == nil {
		if cal, err := mb.FetchCalendar(ctx, []string{"USD", "JPY"}); err == nil {
			out.Calendar = cal
		}
	}

	printJSON(out)
}
