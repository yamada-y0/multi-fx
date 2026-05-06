package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/yamada/multi-fx/internal/agent"
	"github.com/yamada/multi-fx/internal/broker"
	"github.com/yamada/multi-fx/internal/order"
	"github.com/yamada/multi-fx/internal/pool"
	"github.com/yamada/multi-fx/internal/store"
	"github.com/yamada/multi-fx/pkg/currency"
	pkgmarket "github.com/yamada/multi-fx/pkg/market"
	pkgorder "github.com/yamada/multi-fx/pkg/order"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: multi-fx <command> [options]")
		fmt.Fprintln(os.Stderr, "Commands: snapshot, submit-order, set-wakeup, market, init-subpool")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "snapshot":
		runSnapshot(os.Args[2:])
	case "submit-order":
		runSubmitOrder(os.Args[2:])
	case "set-wakeup":
		runSetWakeup(os.Args[2:])
	case "market":
		runMarket(os.Args[2:])
	case "init-subpool":
		runInitSubPool(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

// subPoolIDFromDir はstate-dirのベース名をSubPoolIDとして返す
func subPoolIDFromDir(stateDir string) pool.SubPoolID {
	return pool.SubPoolID(filepath.Base(stateDir))
}

func runSnapshot(args []string) {
	fs := flag.NewFlagSet("snapshot", flag.ExitOnError)
	stateDir := fs.String("state-dir", "", "エージェントディレクトリパス（必須）")
	fs.Parse(args)

	if *stateDir == "" {
		fmt.Fprintln(os.Stderr, "Usage: multi-fx snapshot --state-dir <dir>")
		os.Exit(1)
	}

	ctx := context.Background()
	st, err := store.NewJSONStore(*stateDir)
	if err != nil {
		log.Fatalf("store: %v", err)
	}

	snap, err := st.LoadSubPool(ctx, subPoolIDFromDir(*stateDir))
	if err != nil {
		log.Fatalf("load subpool: %v", err)
	}

	printJSON(snap)
}

func runInitSubPool(args []string) {
	fs := flag.NewFlagSet("init-subpool", flag.ExitOnError)
	cwd, _ := os.Getwd()
	baseDir := fs.String("base-dir", filepath.Join(cwd, "agents"), "エージェントを配置するベースディレクトリ")
	balance := fs.Float64("balance", 0, "初期残高（必須）")
	strategyName := fs.String("strategy", "", "戦略名")
	fs.Parse(args)

	if *balance == 0 {
		fmt.Fprintln(os.Stderr, "Usage: multi-fx init-subpool --balance <amount> [--base-dir <dir>] [--strategy <name>]")
		os.Exit(1)
	}

	id := uuid.New().String()
	stateDir := filepath.Join(*baseDir, id)
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		log.Fatalf("mkdir: %v", err)
	}

	ctx := context.Background()
	st, err := store.NewJSONStore(stateDir)
	if err != nil {
		log.Fatalf("store: %v", err)
	}

	sp := pool.NewSubPool(pool.SubPoolID(id), decimal.NewFromFloat(*balance), *strategyName, time.Now())
	if err := st.SaveSubPool(ctx, sp.Snapshot()); err != nil {
		log.Fatalf("save subpool: %v", err)
	}

	// CLAUDE.md を生成する（multi-fxバイナリのパスを埋め込む）
	mfxPath, err := resolveMultiFXPath()
	if err != nil {
		log.Fatalf("multi-fx path: %v", err)
	}
	claudeMDPath := filepath.Join(stateDir, "CLAUDE.md")
	if err := writeClaudeMD(claudeMDPath, stateDir, mfxPath); err != nil {
		log.Fatalf("write CLAUDE.md: %v", err)
	}

	fmt.Fprintf(os.Stderr, "エージェントを作成しました: %s\n%s の戦略方針を記入してください。\n", stateDir, claudeMDPath)
	printJSON(sp.Snapshot())
}

// resolveMultiFXPath は自分自身（multi-fxバイナリ）のフルパスを返す
func resolveMultiFXPath() (string, error) {
	if exe, err := os.Executable(); err == nil {
		return exe, nil
	}
	if p, err := exec.LookPath("multi-fx"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("multi-fx binary not found")
}

func writeClaudeMD(path, stateDir, mfxPath string) error {
	mfx := mfxPath
	sd := stateDir
	content := "# FX取引エージェント\n\n" +
		"## 戦略方針\n\n" +
		"<!-- ここに戦略方針を記入してください -->\n\n" +
		"## 行動手順\n\n" +
		"起動するたびに以下の順序で行動してください。\n\n" +
		"1. 現在の口座状態を確認する（snapshot）\n" +
		"2. 直近の市場データを確認する（market）\n" +
		"3. 保有ポジション・未約定注文の状況と市場データを踏まえて判断する\n" +
		"4. 発注・決済・何もしない、のいずれかを実行する\n" +
		"5. 次の起動条件（wakeup条件）を設定して終了する（set-wakeup）\n\n" +
		"必ず最後にset-wakeupを呼ぶこと。設定しないと次回起動されません。\n" +
		"wakeup条件は状況に応じて自分で判断すること（時刻・価格トリガー・約定トリガーを組み合わせ可能）。\n" +
		"ポジションを保有している場合は、毎ティック起動せず価格トリガーで寝かせることを検討すること。\n" +
		"各ステップで必ずコマンドを実行し、その結果のみを判断材料にすること。過去の会話の記憶や前回の数値は使わないこと。\n\n" +
		"## コマンドリファレンス\n\n" +
		"- `multi-fx`コマンドのパス: `" + mfx + "`\n" +
		"- state-dir: `" + sd + "`\n\n" +
		"### snapshot — 口座状態の確認\n\n" +
		"```bash\n" +
		mfx + " snapshot --state-dir " + sd + "\n" +
		"```\n\n" +
		"### market — 市場データの取得（新しい順）\n\n" +
		"```bash\n" +
		mfx + " market --state-dir " + sd + " --data <csv-path> --pair USDJPY --n 20\n" +
		"```\n\n" +
		"### submit-order — 発注（新規）\n\n" +
		"```bash\n" +
		mfx + " submit-order \\\n" +
		"  --state-dir " + sd + " \\\n" +
		"  --pair USDJPY \\\n" +
		"  --data <csv-path> \\\n" +
		"  --side <long|short> \\\n" +
		"  --lots <ロット数> \\\n" +
		"  --stop-loss <ストップロス価格> \\\n" +
		"  --order-type <market|limit>\n" +
		"```\n\n" +
		"### submit-order — 決済\n\n" +
		"決済時の--sideはポジションの方向（ロングポジションならlong、ショートポジションならshort）を指定する。\n" +
		"**--data は決済時も必須。** PositionIDはsnapshotのPositions[].IDから取得すること。\n\n" +
		"```bash\n" +
		mfx + " submit-order \\\n" +
		"  --state-dir " + sd + " \\\n" +
		"  --pair USDJPY \\\n" +
		"  --data <csv-path> \\\n" +
		"  --side <long|short> \\\n" +
		"  --lots <ロット数> \\\n" +
		"  --stop-loss <ストップロス価格> \\\n" +
		"  --close-position-id <PositionID>\n" +
		"```\n\n" +
		"### set-wakeup — 次回起動条件の設定\n\n" +
		"複数回呼ぶとOR評価になる（どれか1つが満たされたら起動）。\n\n" +
		"```bash\n" +
		"# 指定時刻以降に起動\n" +
		mfx + " set-wakeup --state-dir " + sd + " --after <RFC3339形式>\n\n" +
		"# レートが価格以上になったら起動\n" +
		mfx + " set-wakeup --state-dir " + sd + " --price-gte USDJPY:<価格>\n\n" +
		"# レートが価格以下になったら起動\n" +
		mfx + " set-wakeup --state-dir " + sd + " --price-lte USDJPY:<価格>\n" +
		"```\n\n" +
		"**ポジション保有中の設定例（ロング、SL=141.0、TP=143.0）:**\n\n" +
		"```bash\n" +
		mfx + " set-wakeup --state-dir " + sd + " --price-lte USDJPY:141.0\n" +
		mfx + " set-wakeup --state-dir " + sd + " --price-gte USDJPY:143.0\n" +
		"```\n\n" +
		"この2行でSL/TPどちらかに到達したとき起動。毎ティック起動は不要。\n\n" +
		"## 注意事項\n\n" +
		"- StopLossは必ず指定すること（リスク管理上必須）\n" +
		"- PositionIDはsnapshotのPositions[].IDから取得すること\n" +
		"- ローソク足は新しい順（インデックス0が最新）で返される\n"
	return os.WriteFile(path, []byte(content), 0644)
}

func runMarket(args []string) {
	fs := flag.NewFlagSet("market", flag.ExitOnError)
	stateDir := fs.String("state-dir", "", "エージェントディレクトリパス（必須）")
	csvPath := fs.String("data", "", "historical モード: Dukascopy CSVファイルパス")
	pair := fs.String("pair", "USDJPY", "通貨ペア")
	n := fs.Int("n", 20, "取得するローソク足の本数")
	fs.Parse(args)

	if *stateDir == "" {
		fmt.Fprintln(os.Stderr, "Usage: multi-fx market --state-dir <dir> [--data <csv>] [--pair <pair>] [--n <count>]")
		os.Exit(1)
	}

	candles, err := fetchCandles(*stateDir, *csvPath, *pair, *n)
	if err != nil {
		log.Fatalf("fetch candles: %v", err)
	}

	printJSON(candles)
}

func fetchCandles(stateDir, csvPath, pairStr string, n int) ([]pkgmarket.Candle, error) {
	pair := currency.Pair(pairStr)
	if csvPath == "" {
		return []pkgmarket.Candle{}, nil
	}
	b, err := restoreHistoricalBroker(stateDir, csvPath, pair)
	if err != nil {
		return nil, err
	}
	return b.FetchCandles(pair, n)
}

func restoreHistoricalBroker(stateDir, csvPath string, pair currency.Pair) (broker.HistoricalBroker, error) {
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
		fmt.Fprintln(os.Stderr, "Usage: multi-fx submit-order --state-dir <dir> --side <long|short> --lots <n> --stop-loss <price> [--data <csv>]")
		os.Exit(1)
	}
	if *csvPath == "" && *closePositionID != "" {
		fmt.Fprintln(os.Stderr, "error: --data is required when closing a position (--close-position-id)")
		os.Exit(1)
	}

	ctx := context.Background()
	id := subPoolIDFromDir(*stateDir)

	st, err := store.NewJSONStore(*stateDir)
	if err != nil {
		log.Fatalf("store: %v", err)
	}

	snap, err := st.LoadSubPool(ctx, id)
	if err != nil {
		log.Fatalf("load subpool: %v", err)
	}
	sp := pool.RestoreSubPool(snap)
	subPools := map[pool.SubPoolID]pool.SubPool{sp.ID(): sp}

	var b broker.Broker
	var hb broker.HistoricalBroker
	if *csvPath != "" {
		hb, err = restoreHistoricalBroker(*stateDir, *csvPath, currency.Pair(*pair))
		if err != nil {
			log.Fatalf("restore broker: %v", err)
		}
		b = hb
	} else {
		b = &stubBroker{}
	}

	agg := order.RestoreAggregator(b, subPools, order.NewIdentityMapper(), st)

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
		SubPoolID:       id,
		Pair:            currency.Pair(*pair),
		Side:            orderSide,
		Lots:            decimal.NewFromFloat(*lots),
		OrderType:       ot,
		OrderIntent:     intent,
		StopLoss:        decimal.NewFromFloat(*stopLoss),
		LimitPrice:      decimal.NewFromFloat(*limitPrice),
		ClosePositionID: *closePositionID,
		RequestedAt:     time.Now(),
	}

	if err := agg.SubmitOrder(ctx, req); err != nil {
		log.Fatalf("submit order: %v", err)
	}
	if err := agg.SyncFills(ctx); err != nil {
		log.Fatalf("sync fills: %v", err)
	}

	if err := st.SaveSubPool(ctx, sp.Snapshot()); err != nil {
		log.Fatalf("save subpool: %v", err)
	}

	if hb != nil {
		snapPath := filepath.Join(*stateDir, "broker_snapshot.json")
		if err := broker.SaveHistoricalBrokerSnapshot(snapPath, hb.Snapshot()); err != nil {
			log.Fatalf("save broker snapshot: %v", err)
		}
	}

	printJSON(sp.Snapshot())
}

func runSetWakeup(args []string) {
	fs := flag.NewFlagSet("set-wakeup", flag.ExitOnError)
	stateDir := fs.String("state-dir", "", "エージェントディレクトリパス（必須）")
	after := fs.String("after", "", "この時刻以降に起動（RFC3339形式）")
	priceGTE := fs.String("price-gte", "", "レートがこの価格以上になったら起動（例: USDJPY:150.0）")
	priceLTE := fs.String("price-lte", "", "レートがこの価格以下になったら起動（例: USDJPY:148.0）")
	anyFill := fs.Bool("any-fill", false, "未約定注文が約定したら起動")
	fs.Parse(args)

	if *stateDir == "" {
		fmt.Fprintln(os.Stderr, "Usage: multi-fx set-wakeup --state-dir <dir> [--after <time>] [--price-gte <pair:price>] [--price-lte <pair:price>] [--any-fill]")
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
	if err := wakeupStore.Save(ctx, subPoolIDFromDir(*stateDir), cond); err != nil {
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
