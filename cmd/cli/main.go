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

	"github.com/shopspring/decimal"
	"github.com/yamada/fxd/internal/agent"
	"github.com/yamada/fxd/internal/broker"
	"github.com/yamada/fxd/internal/store"
	"github.com/yamada/fxd/pkg/currency"
	"github.com/yamada/fxd/pkg/indicator"
	pkgmarket "github.com/yamada/fxd/pkg/market"
	pkgorder "github.com/yamada/fxd/pkg/order"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: fxd <command> [options]")
		fmt.Fprintln(os.Stderr, "Commands: snapshot, submit-order, set-wakeup, market, init-agent")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "snapshot":
		runSnapshot(os.Args[2:])
	case "submit-order":
		runSubmitOrder(os.Args[2:])
	case "cancel-order":
		runCancelOrder(os.Args[2:])
	case "set-wakeup":
		runSetWakeup(os.Args[2:])
	case "market":
		runMarket(os.Args[2:])
	case "note":
		runNote(os.Args[2:])
	case "init-agent":
		runInitAgent(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

// snapshot はOANDAから直接口座情報・ポジション・注文を取得して出力する
func runSnapshot(args []string) {
	fs := flag.NewFlagSet("snapshot", flag.ExitOnError)
	stateDir := fs.String("state-dir", "", "エージェントディレクトリパス（必須）")
	pair := fs.String("pair", "USDJPY", "通貨ペア")
	fs.Parse(args)

	if *stateDir == "" {
		fmt.Fprintln(os.Stderr, "Usage: fxd snapshot --state-dir <dir> [--pair <pair>]")
		os.Exit(1)
	}

	ctx := context.Background()
	tb, _, _, err := setupBrokers(*stateDir, "", currency.Pair(*pair))
	if err != nil {
		log.Fatalf("setup broker: %v", err)
	}

	account, err := tb.FetchAccount(ctx)
	if err != nil {
		log.Fatalf("fetch account: %v", err)
	}
	positions, err := tb.FetchPositions(ctx)
	if err != nil {
		log.Fatalf("fetch positions: %v", err)
	}
	orders, err := tb.FetchOrders(ctx)
	if err != nil {
		log.Fatalf("fetch orders: %v", err)
	}

	type snapshotOutput struct {
		Account   pkgorder.AccountInfo    `json:"Account"`
		Positions []pkgorder.Position     `json:"Positions"`
		Orders    []pkgorder.PendingOrder `json:"Orders"`
	}
	printJSON(snapshotOutput{Account: account, Positions: positions, Orders: orders})
}

func runInitAgent(args []string) {
	fs := flag.NewFlagSet("init-agent", flag.ExitOnError)
	cwd, _ := os.Getwd()
	baseDir := fs.String("base-dir", filepath.Join(cwd, "agents"), "エージェントを配置するベースディレクトリ")
	fs.Parse(args)

	stateDir := filepath.Join(*baseDir, fmt.Sprintf("agent-%d", time.Now().Unix()))
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		log.Fatalf("mkdir: %v", err)
	}

	mfxPath, err := resolveMultiFXPath()
	if err != nil {
		log.Fatalf("fxd path: %v", err)
	}
	claudeMDPath := filepath.Join(stateDir, "CLAUDE.md")
	if err := writeClaudeMD(claudeMDPath, stateDir, mfxPath); err != nil {
		log.Fatalf("write CLAUDE.md: %v", err)
	}

	fmt.Fprintf(os.Stderr, "エージェントを作成しました: %s\n%s の戦略方針を記入してください。\n", stateDir, claudeMDPath)
	printJSON(map[string]string{"state_dir": stateDir})
}

func resolveMultiFXPath() (string, error) {
	if exe, err := os.Executable(); err == nil {
		return exe, nil
	}
	if p, err := exec.LookPath("fxd"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("fxd binary not found")
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
		"- `fxd`コマンドのパス: `" + mfx + "`\n" +
		"- state-dir: `" + sd + "`\n\n" +
		"### snapshot — 口座状態の確認\n\n" +
		"```bash\n" +
		mfx + " snapshot --state-dir " + sd + "\n" +
		"```\n\n" +
		"### market — 市場データの取得（新しい順）\n\n" +
		"出力フィールド:\n" +
		"- `CurrentTime` : 現在時刻\n" +
		"- `Bid` / `Ask` : 現在のBid/Askレート\n" +
		"- `Candles` : ローソク足（インデックス0が最新）\n" +
		"- `Indicators` : テクニカル指標\n" +
		"  - `SMA` : 単純移動平均（20/50/200期間）。SMA200は200本以上必要\n" +
		"  - `EMA` : 指数移動平均（12/26期間）\n" +
		"  - `RSI` : RSI（14期間）。70以上=買われすぎ、30以下=売られすぎ\n" +
		"  - `ATR` : 平均真の値幅（14期間）。ストップロス幅の目安に使える\n" +
		"  - `MACD` : MACD/Signal/Hist（12/26/9期間）\n" +
		"- `Calendar` : 今週のUSD/JPY経済指標（Impact: High/Medium/Low/Holiday）\n\n" +
		"データ不足の指標はゼロ値になる。SMA200を使う場合は `--n 200` 以上を指定すること。\n\n" +
		"```bash\n" +
		mfx + " market --state-dir " + sd + " --pair USDJPY --n 200 --granularity H1\n" +
		"```\n\n" +
		"historicalモード（CSVバックテスト）では `--data <csv-path>` を追加する。historicalモードではCalendarは出力されない。\n\n" +
		"**経済指標への対応:**\n" +
		"CalendarのImpact=Highのイベント前後は相場が急変しやすい。\n" +
		"重要指標の発表時刻を `--after` で指定してその直前に起動するよう設定することを検討すること。\n\n" +
		"### submit-order — 発注（新規）\n\n" +
		"```bash\n" +
		mfx + " submit-order \\\n" +
		"  --state-dir " + sd + " \\\n" +
		"  --pair USDJPY \\\n" +
		"  --side <long|short> \\\n" +
		"  --lots <ロット数> \\\n" +
		"  --stop-loss <ストップロス価格> \\\n" +
		"  --order-type <market|limit>\n" +
		"```\n\n" +
		"### submit-order — 決済\n\n" +
		"決済時の--sideはポジションの方向（ロングポジションならlong、ショートポジションならshort）を指定する。\n" +
		"PositionIDはsnapshotのPositions[].IDから取得すること。\n\n" +
		"```bash\n" +
		mfx + " submit-order \\\n" +
		"  --state-dir " + sd + " \\\n" +
		"  --pair USDJPY \\\n" +
		"  --side <long|short> \\\n" +
		"  --lots <ロット数> \\\n" +
		"  --stop-loss <ストップロス価格> \\\n" +
		"  --close-position-id <PositionID>\n" +
		"```\n\n" +
		"### set-wakeup — 次回起動条件の設定\n\n" +
		"複数フラグを同時指定するとOR評価になる（どれか1つが満たされたら起動）。\n\n" +
		"```bash\n" +
		mfx + " set-wakeup --state-dir " + sd + " \\\n" +
		"  [--after <RFC3339形式>] \\\n" +
		"  [--price-gte USDJPY:<価格>] \\\n" +
		"  [--price-lte USDJPY:<価格>] \\\n" +
		"  [--any-fill]\n" +
		"```\n\n" +
		"**ポジション保有中の設定例（ロング、SL=141.0、TP=143.0）:**\n\n" +
		"```bash\n" +
		mfx + " set-wakeup --state-dir " + sd + " --price-lte USDJPY:141.0 --price-gte USDJPY:143.0\n" +
		"```\n\n" +
		"SL/TPどちらかに到達したとき起動。毎ティック起動は不要。\n\n" +
		"## 注意事項\n\n" +
		"- StopLossは必ず指定すること（リスク管理上必須）\n" +
		"- PositionIDはsnapshotのPositions[].IDから取得すること\n" +
		"- ローソク足は新しい順（インデックス0が最新）で返される\n" +
		"- set-wakeup --after に渡す時刻は market の CurrentTime を基準にすること。壁時計の現在時刻は使わないこと\n"
	return os.WriteFile(path, []byte(content), 0644)
}

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

// setupBrokers は TradingBroker / MarketBroker / HistoricalBroker（historical時のみ）を返す。
func setupBrokers(stateDir, csvPath string, pair currency.Pair) (broker.TradingBroker, broker.MarketBroker, broker.HistoricalBroker, error) {
	if csvPath != "" {
		hb, err := restoreHistoricalBroker(stateDir, csvPath, pair)
		if err != nil {
			return nil, nil, nil, err
		}
		return hb, hb, hb, nil
	}
	tb, mb, err := setupOandaBrokers()
	if err != nil {
		return nil, nil, nil, err
	}
	return tb, mb, nil, nil
}

// setupOandaBrokers は取引用 TradingBroker と市場データ用 MarketBroker を返す。
// OANDA_MARKET_API_TOKEN / OANDA_MARKET_PRACTICE が設定されていればlive環境のMarketBrokerを使う。
func setupOandaBrokers() (broker.TradingBroker, broker.MarketBroker, error) {
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

func runNote(args []string) {
	fs := flag.NewFlagSet("note", flag.ExitOnError)
	stateDir := fs.String("state-dir", "", "エージェントディレクトリパス（必須）")
	fs.Parse(args)

	if *stateDir == "" || len(fs.Args()) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: fxd note --state-dir <dir> <メッセージ>")
		os.Exit(1)
	}

	jst := time.FixedZone("JST", 9*60*60)
	ts := time.Now().In(jst).Format("2006-01-02 15:04:05 JST")
	line := fmt.Sprintf("- [%s] %s\n", ts, strings.Join(fs.Args(), " "))

	notesPath := filepath.Join(*stateDir, "notes.md")
	f, err := os.OpenFile(notesPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("open notes: %v", err)
	}
	defer f.Close()

	if _, err := f.WriteString(line); err != nil {
		log.Fatalf("write note: %v", err)
	}

	printJSON(map[string]string{"noted": strings.Join(fs.Args(), " ")})
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
