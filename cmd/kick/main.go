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
	stateDir := flag.String("state-dir", "", "エージェントディレクトリパス（必須）")
	csvPath := flag.String("data", "", "historical モード: Dukascopy CSVファイルパス")
	pair := flag.String("pair", "USDJPY", "通貨ペア")
	flag.Parse()

	if *stateDir == "" {
		fmt.Fprintln(os.Stderr, "Usage: kick --state-dir <dir> [--data <csv> --pair <pair>]")
		flag.PrintDefaults()
		os.Exit(1)
	}

	ctx := context.Background()
	id := pool.SubPoolID(filepath.Base(*stateDir))

	st, err := store.NewJSONStore(*stateDir)
	if err != nil {
		log.Fatalf("store: %v", err)
	}

	snap, err := st.LoadSubPool(ctx, id)
	if err != nil {
		log.Fatalf("subpool not found: %v", err)
	}

	// Broker構築・復元
	b, done, err := setupBroker(*stateDir, *csvPath, *pair)
	if err != nil {
		log.Fatalf("setup broker: %v", err)
	}
	if done {
		log.Printf("historical データ終端 → 終了")
		os.Exit(0)
	}

	// レート取得
	rate, err := b.FetchRate(ctx, currency.Pair(*pair))
	if err != nil {
		log.Fatalf("fetch rate: %v", err)
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

	// Brokerスナップショットを保存（Advance後の状態）
	if hb, ok := b.(broker.HistoricalBroker); ok {
		snapPath := filepath.Join(*stateDir, "broker_snapshot.json")
		if err := broker.SaveHistoricalBrokerSnapshot(snapPath, hb.Snapshot()); err != nil {
			log.Fatalf("save broker snapshot: %v", err)
		}
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

	sessionID, err := runClaude(*stateDir, snap.SessionID)
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

// setupBroker はモードに応じてBrokerを構築・復元する
// historical モードでは broker_snapshot.json から復元して Advance() を1回呼ぶ
// done=true のときデータ終端に達したことを示す
func setupBroker(stateDir, csvPath, pairStr string) (broker.Broker, bool, error) {
	pair := currency.Pair(pairStr)

	if csvPath == "" {
		// real モード: スタブ（RealApiBroker未実装）
		return &stubBroker{pair: pair}, false, nil
	}

	// historical モード
	b, err := broker.NewHistoricalBroker(pair, csvPath)
	if err != nil {
		return nil, false, fmt.Errorf("historical broker: %w", err)
	}

	snapPath := filepath.Join(stateDir, "broker_snapshot.json")
	snap, err := broker.LoadHistoricalBrokerSnapshot(snapPath)
	if err != nil {
		return nil, false, fmt.Errorf("load broker snapshot: %w", err)
	}
	b.Restore(snap)

	// 1ティック進める
	if !b.Advance() {
		return nil, true, nil
	}

	return b, false, nil
}

// runClaude は Claude Code を起動してセッションIDを返す
// state-dir を --add-dir で渡すことで AGENT.md を参照可能にする
func runClaude(stateDir, prevSessionID string) (string, error) {
	args := []string{"-p", "--output-format", "json", "--add-dir", stateDir}
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
