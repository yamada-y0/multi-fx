package main

import (
	"bufio"
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
	"github.com/yamada/fxd/internal/order"
	"github.com/yamada/fxd/internal/pool"
	"github.com/yamada/fxd/internal/rule"
	"github.com/yamada/fxd/internal/store"
	"github.com/yamada/fxd/internal/tick"
	"github.com/yamada/fxd/pkg/currency"
	pkgmarket "github.com/yamada/fxd/pkg/market"
	pkgorder "github.com/yamada/fxd/pkg/order"
)

func main() {
	stateDir := flag.String("state-dir", "", "エージェントディレクトリパス（必須）")
	csvPath := flag.String("data", "", "historical モード: Dukascopy CSVファイルパス")
	pair := flag.String("pair", "USDJPY", "通貨ペア")
	loop := flag.Bool("loop", false, "データ終端かフロアルール発動まで連続実行")
	flag.Parse()

	if *stateDir == "" {
		fmt.Fprintln(os.Stderr, "Usage: kick --state-dir <dir> [--data <csv> --pair <pair>] [--loop]")
		flag.PrintDefaults()
		os.Exit(1)
	}

	for {
		done, err := runOnce(*stateDir, *csvPath, *pair)
		if err != nil {
			log.Fatalf("kick: %v", err)
		}
		if done || !*loop {
			break
		}
	}
}

// runOnce は1ティック分の処理を実行する
// done=true のとき呼び出し元はループを終了すべき
func runOnce(stateDir, csvPath, pairStr string) (done bool, err error) {
	ctx := context.Background()
	id := pool.SubPoolID(filepath.Base(stateDir))

	st, err := store.NewJSONStore(stateDir)
	if err != nil {
		return false, fmt.Errorf("store: %v", err)
	}

	snap, err := st.LoadSubPool(ctx, id)
	if err != nil {
		return false, fmt.Errorf("subpool not found: %v", err)
	}

	// Broker構築・復元
	b, dataDone, err := setupBroker(stateDir, csvPath, pairStr)
	if err != nil {
		return false, fmt.Errorf("setup broker: %v", err)
	}
	if dataDone {
		log.Printf("historical データ終端 → 終了")
		return true, nil
	}

	// レート取得
	rate, err := b.FetchRate(ctx, currency.Pair(pairStr))
	if err != nil {
		return false, fmt.Errorf("fetch rate: %v", err)
	}

	// SubPool・Aggregator復元
	sp := pool.RestoreSubPool(snap)
	subPools := map[pool.SubPoolID]pool.SubPool{sp.ID(): sp}
	agg := order.RestoreAggregator(b, subPools, order.NewIdentityMapper(), st)

	wakeupStore := agent.NewJSONWakeupStore(filepath.Join(stateDir, "wakeup.json"))
	engine := rule.NewRuleEngine()
	engine.Register(rule.FloorRule{ThresholdRatio: 0.75})

	ticker := tick.New(agg, sp, wakeupStore, engine, st)
	result, err := ticker.Tick(ctx, rate)
	if err != nil {
		return false, fmt.Errorf("tick: %v", err)
	}

	// Brokerスナップショットを保存（Advance後の状態）
	if hb, ok := b.(broker.HistoricalBroker); ok {
		snapPath := filepath.Join(stateDir, "broker_snapshot.json")
		if err := broker.SaveHistoricalBrokerSnapshot(snapPath, hb.Snapshot()); err != nil {
			return false, fmt.Errorf("save broker snapshot: %v", err)
		}
	}

	if result.Done {
		log.Printf("フロアルール発動 → 終了")
		return true, nil
	}

	if !result.ShouldWakeup {
		log.Printf("[%s] WakeupCondition未達 → スキップ", rate.Timestamp.Format("2006-01-02T15:04Z"))
		return false, nil
	}

	log.Printf("[%s] Claude起動 (fills=%d)", rate.Timestamp.Format("2006-01-02T15:04Z"), result.FillCount)

	sessionID, err := runClaude(stateDir, snap.SessionID)
	if err != nil {
		return false, fmt.Errorf("claude: %v", err)
	}

	// session_id をSubPoolに保存
	if sessionID != "" && sessionID != snap.SessionID {
		sp.SetSessionID(sessionID)
		if err := st.SaveSubPool(ctx, sp.Snapshot()); err != nil {
			return false, fmt.Errorf("save subpool: %v", err)
		}
		log.Printf("session_id 更新: %s", sessionID)
	}

	// 会話ログをMarkdownとしてstate-dir/logs/配下に保存
	if sessionID != "" {
		if err := saveSessionLog(stateDir, sessionID, rate.Timestamp); err != nil {
			log.Printf("warn: save session log: %v", err)
		}
	}

	return false, nil
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
// state-dir を --add-dir で渡すことで CLAUDE.md を参照可能にする
// --system-prompt でFXエージェントとしての役割を確立し、プロジェクトのCLAUDE.mdより優先させる
func runClaude(stateDir, prevSessionID string) (string, error) {
	systemPrompt := "あなたはFX取引エージェントです。" +
		stateDir + "/CLAUDE.md の行動手順・戦略方針・コマンドリファレンスに従って行動してください。"
	prompt := "CLAUDE.mdの行動手順に従って行動してください。"
	multiFXPath, err := resolveMultiFX()
	if err != nil {
		return "", fmt.Errorf("fxd not found: %w", err)
	}
	allowedTool := fmt.Sprintf("Bash(%s *)", multiFXPath)

	args := []string{
		"-p", prompt,
		"--output-format", "json",
		"--add-dir", stateDir,
		"--system-prompt", systemPrompt,
		"--allowedTools", allowedTool,
	}
	if prevSessionID != "" {
		args = append(args, "--resume", prevSessionID)
	}

	claudePath, err := resolveClaude()
	if err != nil {
		return "", fmt.Errorf("claude not found in PATH: %w", err)
	}

	cmd := exec.Command(claudePath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

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

// saveSessionLog はClaudeのjsonlセッションログをMarkdownに変換してstate-dir/logs/に保存する
func saveSessionLog(stateDir, sessionID string, tickTime time.Time) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home dir: %w", err)
	}

	// Claude のセッションログはカレントディレクトリに紐づく
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	projectKey := strings.ReplaceAll(cwd, "/", "-")
	jsonlPath := filepath.Join(home, ".claude", "projects", projectKey, sessionID+".jsonl")

	data, err := os.ReadFile(jsonlPath)
	if os.IsNotExist(err) {
		return nil // ログがない場合はスキップ
	}
	if err != nil {
		return fmt.Errorf("read jsonl: %w", err)
	}

	// jsonlをMarkdownに変換
	md := formatSessionLog(sessionID, tickTime, data)

	logsDir := filepath.Join(stateDir, "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return fmt.Errorf("mkdir logs: %w", err)
	}

	filename := fmt.Sprintf("%s_%s.md", tickTime.UTC().Format("2006-01-02T15-04-05Z"), sessionID[:8])
	outPath := filepath.Join(logsDir, filename)
	return os.WriteFile(outPath, []byte(md), 0644)
}

// formatSessionLog はjsonlバイト列をMarkdown文字列に変換する
func formatSessionLog(sessionID string, tickTime time.Time, data []byte) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# セッションログ\n\n")
	sb.WriteString(fmt.Sprintf("- **セッションID**: %s\n", sessionID))
	sb.WriteString(fmt.Sprintf("- **ティック時刻**: %s\n\n", tickTime.UTC().Format(time.RFC3339)))
	sb.WriteString("---\n\n")

	type message struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"message"`
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var msg message
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if msg.Type != "user" && msg.Type != "assistant" {
			continue
		}

		role := "**User**"
		if msg.Type == "assistant" {
			role = "**Assistant**"
		}

		// contentはstring or []contentBlock
		var text string
		switch v := msg.Message.Content.(type) {
		case string:
			text = v
		case []any:
			for _, block := range v {
				m, ok := block.(map[string]any)
				if !ok {
					continue
				}
				switch m["type"] {
				case "text":
					if t, ok := m["text"].(string); ok {
						text += t
					}
				case "tool_use":
					name, _ := m["name"].(string)
					input, _ := json.Marshal(m["input"])
					text += fmt.Sprintf("\n```\n[Tool: %s]\n%s\n```\n", name, string(input))
				case "tool_result":
					content, _ := m["content"].(string)
					if content == "" {
						if arr, ok := m["content"].([]any); ok {
							for _, c := range arr {
								if cm, ok := c.(map[string]any); ok {
									if cm["type"] == "text" {
										content += cm["text"].(string)
									}
								}
							}
						}
					}
					text += fmt.Sprintf("\n```\n[Tool Result]\n%s\n```\n", content)
				}
			}
		}

		if strings.TrimSpace(text) == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf("### %s\n\n%s\n\n", role, strings.TrimSpace(text)))
	}

	return sb.String()
}

// resolveMultiFX は fxd コマンドのフルパスを返す
// kick バイナリと同じディレクトリを優先し、なければ PATH から探す
func resolveMultiFX() (string, error) {
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "fxd")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	if p, err := exec.LookPath("fxd"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("fxd not found in same directory as kick or PATH")
}

// resolveClaude は claude コマンドのフルパスを返す
// exec.LookPath で見つからない場合は `sh -c 'which claude'` にフォールバックする
func resolveClaude() (string, error) {
	if p, err := exec.LookPath("claude"); err == nil {
		return p, nil
	}
	out, err := exec.Command("sh", "-c", "which claude").Output()
	if err != nil {
		return "", fmt.Errorf("which claude: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// stubBroker は real モード用スタブ（RealApiBroker未実装の代替）
type stubBroker struct {
	pair currency.Pair
}

func (b *stubBroker) SubmitOrder(_ context.Context, o pkgorder.Order) (broker.OrderID, error) {
	return broker.OrderID("stub-" + string(o.Pair)), nil
}
func (b *stubBroker) FetchOrders(_ context.Context) ([]pkgorder.PendingOrder, error) {
	return nil, nil
}
func (b *stubBroker) FetchFillEvents(_ context.Context, _ string) ([]pkgorder.FillEvent, error) {
	return nil, nil
}
func (b *stubBroker) CancelOrder(_ context.Context, _ broker.OrderID) error { return nil }
func (b *stubBroker) FetchPositions(_ context.Context) ([]pkgorder.Position, error) {
	return nil, nil
}
func (b *stubBroker) FetchRate(_ context.Context, pair currency.Pair) (currency.Rate, error) {
	return currency.Rate{Pair: pair, Bid: decimal.NewFromFloat(150.0), Ask: decimal.NewFromFloat(150.0)}, nil
}
func (b *stubBroker) FetchCandles(_ context.Context, _ currency.Pair, _ string, _ int) ([]pkgmarket.Candle, error) {
	return nil, nil
}
func (b *stubBroker) Name() string { return "stub" }
