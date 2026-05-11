package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/yamada/fxd/internal/broker"
	"github.com/yamada/fxd/internal/store"
	"github.com/yamada/fxd/pkg/currency"
	pkgorder "github.com/yamada/fxd/pkg/order"
)

func runSubmitOrder(args []string) {
	fs := flag.NewFlagSet("submit-order", flag.ExitOnError)
	stateDir := fs.String("state-dir", "", "エージェントディレクトリパス（必須）")
	csvPath := fs.String("data", "", "historical モード: Dukascopy CSVファイルパス")
	pair := fs.String("pair", "USDJPY", "通貨ペア")
	side := fs.String("side", "", "long or short（必須）")
	lots := fs.Float64("lots", 0, "ロット数（必須）")
	stopLoss := fs.Float64("stop-loss", 0, "ストップロス価格（必須）")
	limitPrice := fs.Float64("limit-price", 0, "指値価格（limit注文時のみ）")
	orderType := fs.String("order-type", "market", "market / limit")
	closePositionID := fs.String("close-position-id", "", "決済対象PositionID（決済時のみ）")
	fs.Parse(args)

	if *stateDir == "" || *side == "" || *lots == 0 || *stopLoss == 0 {
		fmt.Fprintln(os.Stderr, "Usage: fxd submit-order --state-dir <dir> --side <long|short> --lots <n> --stop-loss <price> [--data <csv>]")
		os.Exit(1)
	}

	ctx := context.Background()
	pairVal := currency.Pair(*pair)

	tb, _, hb, err := setupBrokers(*stateDir, *csvPath, pairVal)
	if err != nil {
		log.Fatalf("setup broker: %v", err)
	}

	var orderSide pkgorder.Side
	switch strings.ToLower(*side) {
	case "long":
		orderSide = pkgorder.Long
	case "short":
		orderSide = pkgorder.Short
	default:
		log.Fatalf("invalid side: %s", *side)
	}

	var ot pkgorder.OrderType
	switch strings.ToLower(*orderType) {
	case "limit":
		ot = pkgorder.OrderTypeLimit
	default:
		ot = pkgorder.OrderTypeMarket
	}

	intent := pkgorder.OrderIntentOpen
	if *closePositionID != "" {
		intent = pkgorder.OrderIntentClose
	}

	o := pkgorder.Order{
		Pair:            pairVal,
		Side:            orderSide,
		Lots:            mustDecimal(*lots),
		OrderType:       ot,
		Intent:          intent,
		StopLoss:        mustDecimal(*stopLoss),
		LimitPrice:      mustDecimal(*limitPrice),
		ClosePositionID: *closePositionID,
	}

	orderID, err := tb.SubmitOrder(ctx, o)
	if err != nil {
		log.Fatalf("submit order: %v", err)
	}

	// Brokerスナップショット保存（historicalモード）
	if hb != nil {
		snapPath := filepath.Join(*stateDir, "broker_snapshot.json")
		if err := broker.SaveHistoricalBrokerSnapshot(snapPath, hb.Snapshot()); err != nil {
			log.Fatalf("save broker snapshot: %v", err)
		}
	}

	// lastFillEventIDを更新（約定済みの場合）
	st, err := store.NewJSONStore(*stateDir)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	lastID, _ := st.LoadLastFillEventID(ctx)
	events, err := tb.FetchFillEvents(ctx, lastID)
	if err != nil {
		log.Fatalf("fetch fill events: %v", err)
	}
	if len(events) > 0 {
		if err := st.SaveLastFillEventID(ctx, events[len(events)-1].ID); err != nil {
			log.Fatalf("save last fill event id: %v", err)
		}
	}

	printJSON(map[string]string{"order_id": string(orderID)})
}

func runCancelOrder(args []string) {
	fs := flag.NewFlagSet("cancel-order", flag.ExitOnError)
	stateDir := fs.String("state-dir", "", "エージェントディレクトリパス（必須）")
	orderID := fs.String("order-id", "", "キャンセル対象のOrderID（必須）")
	fs.Parse(args)

	if *stateDir == "" || *orderID == "" {
		fmt.Fprintln(os.Stderr, "Usage: fxd cancel-order --state-dir <dir> --order-id <id>")
		os.Exit(1)
	}

	ctx := context.Background()
	tb, _, _, err := setupBrokers(*stateDir, "", currency.Pair("USDJPY"))
	if err != nil {
		log.Fatalf("setup broker: %v", err)
	}

	if err := tb.CancelOrder(ctx, broker.OrderID(*orderID)); err != nil {
		log.Fatalf("cancel order: %v", err)
	}

	printJSON(map[string]string{"cancelled_order_id": *orderID})
}
