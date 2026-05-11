package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/shopspring/decimal"
	"github.com/yamada/fxd/internal/agent"
	"github.com/yamada/fxd/pkg/currency"
)

func runSetWakeup(args []string) {
	fs := flag.NewFlagSet("set-wakeup", flag.ExitOnError)
	stateDir := fs.String("state-dir", "", "エージェントディレクトリパス（必須）")
	after := fs.String("after", "", "この時刻以降に起動（RFC3339形式）")
	priceGTE := fs.String("price-gte", "", "レートがこの価格以上になったら起動（例: USDJPY:150.0）")
	priceLTE := fs.String("price-lte", "", "レートがこの価格以下になったら起動（例: USDJPY:148.0）")
	anyFill := fs.Bool("any-fill", false, "未約定注文が約定したら起動")
	fs.Parse(args)

	if *stateDir == "" {
		fmt.Fprintln(os.Stderr, "Usage: fxd set-wakeup --state-dir <dir> [--after <time>] [--price-gte <pair:price>] [--price-lte <pair:price>] [--any-fill]")
		os.Exit(1)
	}

	cond := agent.WakeupCondition{AnyFill: *anyFill}

	if *after != "" {
		t, err := time.Parse(time.RFC3339, *after)
		if err != nil {
			log.Fatalf("invalid --after: %v", err)
		}
		cond.After = &t
	}
	if *priceGTE != "" {
		pair, price, err := parsePairPrice(*priceGTE)
		if err != nil {
			log.Fatalf("invalid --price-gte: %v", err)
		}
		cond.PriceGTE = map[currency.Pair]decimal.Decimal{pair: price}
	}
	if *priceLTE != "" {
		pair, price, err := parsePairPrice(*priceLTE)
		if err != nil {
			log.Fatalf("invalid --price-lte: %v", err)
		}
		cond.PriceLTE = map[currency.Pair]decimal.Decimal{pair: price}
	}

	if cond.After == nil && cond.PriceGTE == nil && cond.PriceLTE == nil && !cond.AnyFill {
		log.Fatalf("at least one wakeup condition must be specified")
	}

	ctx := context.Background()
	wakeupStore := agent.NewJSONWakeupStore(filepath.Join(*stateDir, "wakeup.json"))
	if err := wakeupStore.Save(ctx, cond); err != nil {
		log.Fatalf("save wakeup: %v", err)
	}

	printJSON(cond)
}
