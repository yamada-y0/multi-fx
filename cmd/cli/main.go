package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"github.com/yamada/multi-fx/internal/agent"
	"github.com/yamada/multi-fx/internal/broker"
	"github.com/yamada/multi-fx/internal/order"
	"github.com/yamada/multi-fx/internal/pool"
	"github.com/yamada/multi-fx/internal/store"
	"github.com/yamada/multi-fx/pkg/currency"
	pkgorder "github.com/yamada/multi-fx/pkg/order"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: multi-fx <command> [options]")
		fmt.Fprintln(os.Stderr, "Commands: snapshot, submit-order, set-wakeup")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "snapshot":
		runSnapshot(os.Args[2:])
	case "submit-order":
		runSubmitOrder(os.Args[2:])
	case "set-wakeup":
		runSetWakeup(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

func commonFlags(args []string) (stateDir string, subPoolID string, fs *flag.FlagSet) {
	fs = flag.NewFlagSet("", flag.ExitOnError)
	fs.StringVar(&stateDir, "state-dir", "", "JSONStoreのディレクトリパス（必須）")
	fs.StringVar(&subPoolID, "subpool", "", "対象SubPoolID（必須）")
	fs.Parse(args)
	return
}

func runSnapshot(args []string) {
	stateDir, subPoolID, _ := commonFlags(args)
	if stateDir == "" || subPoolID == "" {
		fmt.Fprintln(os.Stderr, "Usage: multi-fx snapshot --state-dir <dir> --subpool <id>")
		os.Exit(1)
	}

	ctx := context.Background()
	st, err := store.NewJSONStore(stateDir)
	if err != nil {
		log.Fatalf("store: %v", err)
	}

	snap, err := st.LoadSubPool(ctx, pool.SubPoolID(subPoolID))
	if err != nil {
		log.Fatalf("load subpool: %v", err)
	}

	printJSON(snap)
}

func runSubmitOrder(args []string) {
	fs := flag.NewFlagSet("submit-order", flag.ExitOnError)
	stateDir := fs.String("state-dir", "", "JSONStoreのディレクトリパス（必須）")
	subPoolID := fs.String("subpool", "", "対象SubPoolID（必須）")
	pair := fs.String("pair", "USDJPY", "通貨ペア")
	side := fs.String("side", "", "long or short（必須）")
	lots := fs.Float64("lots", 0, "ロット数（必須）")
	stopLoss := fs.Float64("stop-loss", 0, "ストップロス価格（必須）")
	limitPrice := fs.Float64("limit-price", 0, "指値価格（limit注文時のみ）")
	orderType := fs.String("order-type", "market", "market / limit")
	closePositionID := fs.String("close-position-id", "", "決済対象PositionID（決済時のみ）")
	fs.Parse(args)

	if *stateDir == "" || *subPoolID == "" || *side == "" || *lots == 0 || *stopLoss == 0 {
		fmt.Fprintln(os.Stderr, "Usage: multi-fx submit-order --state-dir <dir> --subpool <id> --side <long|short> --lots <n> --stop-loss <price>")
		os.Exit(1)
	}

	ctx := context.Background()
	st, err := store.NewJSONStore(*stateDir)
	if err != nil {
		log.Fatalf("store: %v", err)
	}

	snap, err := st.LoadSubPool(ctx, pool.SubPoolID(*subPoolID))
	if err != nil {
		log.Fatalf("load subpool: %v", err)
	}
	sp := pool.RestoreSubPool(snap)
	subPools := map[pool.SubPoolID]pool.SubPool{sp.ID(): sp}

	// スタブBroker: 実際のAPIは未実装のためHistoricalBrokerを代替に使えないのでstubを使う
	b := &stubBroker{}
	agg := order.NewAggregator(b, subPools, order.NewIdentityMapper(), st)

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

	intent := pool.OrderIntentOpen
	if *closePositionID != "" {
		intent = pool.OrderIntentClose
	}

	req := pool.OrderRequest{
		SubPoolID:       pool.SubPoolID(*subPoolID),
		Pair:            currency.USDJPY, // TODO: pairフラグから変換
		Side:            orderSide,
		Lots:            decimal.NewFromFloat(*lots),
		OrderType:       ot,
		OrderIntent:     intent,
		StopLoss:        decimal.NewFromFloat(*stopLoss),
		LimitPrice:      decimal.NewFromFloat(*limitPrice),
		ClosePositionID: *closePositionID,
		RequestedAt:     time.Now(),
	}
	_ = pair // TODO: 通貨ペア変換

	if err := agg.SubmitOrder(ctx, req); err != nil {
		log.Fatalf("submit order: %v", err)
	}
	if err := agg.SyncFills(ctx); err != nil {
		log.Fatalf("sync fills: %v", err)
	}

	// SubPoolの最新状態を保存
	if err := st.SaveSubPool(ctx, sp.Snapshot()); err != nil {
		log.Fatalf("save subpool: %v", err)
	}

	printJSON(sp.Snapshot())
}

func runSetWakeup(args []string) {
	fs := flag.NewFlagSet("set-wakeup", flag.ExitOnError)
	stateDir := fs.String("state-dir", "", "JSONStoreのディレクトリパス（必須）")
	subPoolID := fs.String("subpool", "", "対象SubPoolID（必須）")
	after := fs.String("after", "", "この時刻以降に起動（RFC3339形式）")
	priceGTE := fs.String("price-gte", "", "レートがこの価格以上になったら起動（例: USDJPY:150.0）")
	priceLTE := fs.String("price-lte", "", "レートがこの価格以下になったら起動（例: USDJPY:148.0）")
	fs.Parse(args)

	if *stateDir == "" || *subPoolID == "" {
		fmt.Fprintln(os.Stderr, "Usage: multi-fx set-wakeup --state-dir <dir> --subpool <id> [--after <time>] [--price-gte <pair:price>] [--price-lte <pair:price>]")
		os.Exit(1)
	}

	cond := agent.WakeupCondition{}

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

	if cond.After == nil && cond.PriceGTE == nil && cond.PriceLTE == nil {
		log.Fatalf("at least one wakeup condition must be specified")
	}

	ctx := context.Background()
	wakeupStore := agent.NewJSONWakeupStore(filepath.Join(*stateDir, "wakeup.json"))
	if err := wakeupStore.Save(ctx, pool.SubPoolID(*subPoolID), cond); err != nil {
		log.Fatalf("save wakeup: %v", err)
	}

	printJSON(cond)
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

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		log.Fatalf("json encode: %v", err)
	}
}

// stubBroker は CLI用のスタブBroker。RealApiBroker実装までの代替。
type stubBroker struct{}

func (b *stubBroker) SubmitOrder(_ context.Context, o pkgorder.Order) (broker.OrderID, error) {
	return broker.OrderID("stub-" + string(o.Pair)), nil
}
func (b *stubBroker) FetchFills(_ context.Context) ([]pkgorder.Fill, error) { return nil, nil }
func (b *stubBroker) CancelOrder(_ context.Context, _ broker.OrderID) error  { return nil }
func (b *stubBroker) FetchPositions(_ context.Context) ([]pkgorder.Position, error) {
	return nil, nil
}
func (b *stubBroker) FetchRate(_ context.Context, pair currency.Pair) (currency.Rate, error) {
	return currency.Rate{Pair: pair, Bid: decimal.NewFromFloat(150.0), Ask: decimal.NewFromFloat(150.0)}, nil
}
func (b *stubBroker) Name() string { return "stub" }
