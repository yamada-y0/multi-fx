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
	"time"

	"github.com/shopspring/decimal"
	"github.com/yamada/multi-fx/internal/agent"
	"github.com/yamada/multi-fx/internal/broker"
	"github.com/yamada/multi-fx/internal/order"
	"github.com/yamada/multi-fx/internal/pool"
	"github.com/yamada/multi-fx/internal/rule"
	"github.com/yamada/multi-fx/internal/store"
	"github.com/yamada/multi-fx/internal/tick"
	"github.com/yamada/multi-fx/pkg/currency"
	pkgorder "github.com/yamada/multi-fx/pkg/order"
)

func main() {
	stateDir := flag.String("state-dir", "", "JSONStoreのディレクトリパス（必須）")
	subPoolID := flag.String("subpool", "", "対象SubPoolID（必須）")
	systemPrompt := flag.String("system-prompt", "", "Claudeに渡す戦略方針テキスト（必須）")
	csvPath := flag.String("data", "", "historical モード: Dukascopy CSVファイルパス")
	pair := flag.String("pair", "USDJPY", "通貨ペア")
	flag.Parse()

	if *stateDir == "" || *subPoolID == "" || *systemPrompt == "" {
		fmt.Fprintln(os.Stderr, "Usage: kick --state-dir <dir> --subpool <id> --system-prompt <prompt> [--data <csv> --pair <pair>]")
		flag.PrintDefaults()
		os.Exit(1)
	}

	ctx := context.Background()
	id := pool.SubPoolID(*subPoolID)

	st, err := store.NewJSONStore(*stateDir)
	if err != nil {
		log.Fatalf("store: %v", err)
	}

	snap, err := st.LoadSubPool(ctx, id)
	if err != nil {
		log.Fatalf("subpool not found: %v", err)
	}

	// ブローカー・レート取得
	b, rate, done, err := setupBroker(ctx, *stateDir, *csvPath, *pair)
	if err != nil {
		log.Fatalf("setup broker: %v", err)
	}
	if done {
		log.Printf("historical データ終端 → 終了")
		os.Exit(0)
	}

	// SubPool・Aggregator復元
	sp := pool.RestoreSubPool(snap)
	subPools := map[pool.SubPoolID]pool.SubPool{sp.ID(): sp}
	agg := order.RestoreAggregator(b, subPools, order.NewIdentityMapper(), st)

	wakeupStore := agent.NewJSONWakeupStore(filepath.Join(*stateDir, "wakeup.json"))
	engine := rule.NewRuleEngine()
	engine.Register(rule.FloorRule{ThresholdRatio: 1.0})

	ticker := tick.New(agg, sp, wakeupStore, engine, st)
	result, err := ticker.Tick(ctx, rate)
	if err != nil {
		log.Fatalf("tick: %v", err)
	}

	if result.Done {
		log.Printf("フロアルール発動 → 終了")
		os.Exit(0)
	}

	if !result.ShouldWakeup {
		log.Printf("WakeupCondition未達 → 終了")
		os.Exit(0)
	}

	log.Printf("Claude起動 (fills=%d)", result.FillCount)

	sessionID, err := runClaude(*systemPrompt, snap.SessionID)
	if err != nil {
		log.Fatalf("claude: %v", err)
	}

	// session_id をSubPoolに保存
	if sessionID != "" && sessionID != snap.SessionID {
		sp.SetSessionID(sessionID)
		if err := st.SaveSubPool(ctx, sp.Snapshot()); err != nil {
			log.Fatalf("save subpool: %v", err)
		}
		log.Printf("session_id 更新: %s", sessionID)
	}
}

// setupBroker はモードに応じてBrokerとレートを準備する
// historical モードではカーソルを1進めて保存する
// done=true のときデータ終端に達したことを示す
func setupBroker(ctx context.Context, stateDir, csvPath, pairStr string) (broker.Broker, currency.Rate, bool, error) {
	pair := currency.Pair(pairStr)

	if csvPath == "" {
		// real モード: スタブ（RealApiBroker未実装）
		b := &stubBroker{pair: pair}
		rate := currency.Rate{
			Pair:      pair,
			Bid:       decimal.NewFromFloat(150.0),
			Ask:       decimal.NewFromFloat(150.0),
			Timestamp: time.Now(),
		}
		return b, rate, false, nil
	}

	// historical モード
	statePath := filepath.Join(stateDir, "historical_state.json")
	hs, err := store.LoadHistoricalState(statePath)
	if err != nil {
		return nil, currency.Rate{}, false, fmt.Errorf("load historical state: %w", err)
	}

	b, err := broker.NewHistoricalBroker(pair, csvPath)
	if err != nil {
		return nil, currency.Rate{}, false, fmt.Errorf("historical broker: %w", err)
	}

	// カーソル位置まで復元
	for i := 0; i < hs.Cursor; i++ {
		if !b.Advance() {
			return nil, currency.Rate{}, true, nil
		}
	}

	rate, err := b.FetchRate(ctx, pair)
	if err != nil {
		return nil, currency.Rate{}, false, fmt.Errorf("fetch rate: %w", err)
	}

	// カーソルを1進めて保存
	b.Advance()
	if err := store.SaveHistoricalState(statePath, store.HistoricalState{Cursor: hs.Cursor + 1}); err != nil {
		return nil, currency.Rate{}, false, fmt.Errorf("save historical state: %w", err)
	}

	return b, rate, false, nil
}

// runClaude は Claude Code を起動してセッションIDを返す
func runClaude(systemPrompt, prevSessionID string) (string, error) {
	args := []string{"-p", "--output-format", "json", "--system-prompt", systemPrompt}
	if prevSessionID != "" {
		args = append(args, "--resume", prevSessionID)
	}

	cmd := exec.Command("claude", args...)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("claude exited: %w", err)
	}

	var result struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return "", fmt.Errorf("parse claude output: %w", err)
	}
	return result.SessionID, nil
}

// stubBroker は real モード用スタブ（RealApiBroker未実装の代替）
type stubBroker struct {
	pair currency.Pair
}

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
