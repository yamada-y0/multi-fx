package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/shopspring/decimal"
	"github.com/yamada/multi-fx/internal/agent"
	"github.com/yamada/multi-fx/internal/broker"
	intmarket "github.com/yamada/multi-fx/internal/market"
	"github.com/yamada/multi-fx/internal/order"
	"github.com/yamada/multi-fx/internal/pool"
	"github.com/yamada/multi-fx/internal/rule"
	"github.com/yamada/multi-fx/internal/runner"
	"github.com/yamada/multi-fx/internal/store"
	"github.com/yamada/multi-fx/pkg/currency"
)

func main() {
	csvPath := flag.String("data", "", "Dukascopy CSV ファイルのパス（必須）")
	initialBalance := flag.Float64("balance", 100000, "初期残高（JPY）")
	lots := flag.Float64("lots", 0.1, "1注文のロット数")
	stopLoss := flag.Float64("stoploss", 139.0, "ストップロス価格")
	flag.Parse()

	if *csvPath == "" {
		fmt.Fprintln(os.Stderr, "Usage: dev -data <csv>")
		flag.PrintDefaults()
		os.Exit(1)
	}

	ctx := context.Background()
	pair := currency.USDJPY

	b, err := broker.NewHistoricalBroker(pair, *csvPath)
	if err != nil {
		log.Fatalf("broker: %v", err)
	}

	st := store.NewMemoryStore(decimal.NewFromFloat(*initialBalance))
	sp := pool.NewSubPool("sp-1", decimal.NewFromFloat(*initialBalance), "dummy", b.CurrentTime())
	subPools := map[pool.SubPoolID]pool.SubPool{"sp-1": sp}
	agg := order.NewAggregator(b, subPools, order.NewIdentityMapper(), st)

	strategy := agent.NewDummyStrategy(pair, decimal.NewFromFloat(*lots), decimal.NewFromFloat(*stopLoss))
	a := agent.NewAgent(sp, strategy, agent.NewMemoryWakeupStore())

	engine := rule.NewRuleEngine()
	engine.Register(rule.FloorRule{ThresholdRatio: 0.8})

	provider := intmarket.NewProvider(b, 20)

	r := runner.New(
		[]agent.Agent{a},
		agg,
		engine,
		provider,
		pair,
		nil, // Commander なし
	)

	tickCount := 0
	for {
		rate, err := b.FetchRate(ctx, pair)
		if err != nil {
			log.Fatalf("FetchRate: %v", err)
		}

		active, err := r.Tick(ctx, rate)
		if err != nil {
			log.Fatalf("Tick: %v", err)
		}
		tickCount++

		if !active || !b.Advance() {
			break
		}
	}

	// 結果サマリ
	final := sp.Snapshot()
	fills, _ := st.ListFills(ctx, "sp-1")

	fmt.Printf("\n=== バックテスト結果 ===\n")
	fmt.Printf("ティック数     : %d\n", tickCount)
	fmt.Printf("約定数         : %d\n", len(fills))
	fmt.Printf("初期残高       : %s\n", final.InitialBalance.StringFixed(3))
	fmt.Printf("最終残高       : %s\n", final.CurrentBalance.StringFixed(3))
	fmt.Printf("含み損益       : %s\n", final.UnrealizedPnL.StringFixed(3))
	fmt.Printf("実質残高       : %s\n", final.EquityBalance().StringFixed(3))
	fmt.Printf("実現損益合計   : %s\n", final.RealizedPnL.StringFixed(3))
	fmt.Printf("保有ポジション : %d\n", len(final.Positions))
	fmt.Printf("終了状態       : %s\n", final.State)
}
