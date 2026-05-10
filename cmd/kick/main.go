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

	"github.com/yamada/fxd/internal/agent"
	"github.com/yamada/fxd/internal/broker"
	"github.com/yamada/fxd/internal/store"
	"github.com/yamada/fxd/internal/tick"
	"github.com/yamada/fxd/pkg/currency"
)

func main() {
	stateDir := flag.String("state-dir", "", "エージェントディレクトリパス（必須）")
	csvPath := flag.String("data", "", "historical モード: Dukascopy CSVファイルパス")
	pair := flag.String("pair", "USDJPY", "通貨ペア")
	loop := flag.Bool("loop", false, "データ終端まで連続実行")
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

func runOnce(stateDir, csvPath, pairStr string) (done bool, err error) {
	ctx := context.Background()

	st, err := store.NewJSONStore(stateDir)
	if err != nil {
		return false, fmt.Errorf("store: %v", err)
	}

	// Broker構築
	tb, mb, hb, dataDone, err := setupBrokers(stateDir, csvPath, pairStr)
	if err != nil {
		return false, fmt.Errorf("setup broker: %v", err)
	}
	if dataDone {
		log.Printf("historical データ終端 → 終了")
		return true, nil
	}

	// レート取得
	rate, err := mb.FetchRate(ctx, currency.Pair(pairStr))
	if err != nil {
		return false, fmt.Errorf("fetch rate: %v", err)
	}

	wakeupStore := agent.NewJSONWakeupStore(filepath.Join(stateDir, "wakeup.json"))
	ticker := tick.New(tb, wakeupStore, st)
	result, err := ticker.Tick(ctx, rate)
	if err != nil {
		return false, fmt.Errorf("tick: %v", err)
	}

	// Brokerスナップショットを保存（Advance後の状態）
	if hb != nil {
		snapPath := filepath.Join(stateDir, "broker_snapshot.json")
		if err := broker.SaveHistoricalBrokerSnapshot(snapPath, hb.Snapshot()); err != nil {
			return false, fmt.Errorf("save broker snapshot: %v", err)
		}
	}

	if !result.ShouldWakeup {
		log.Printf("[%s] WakeupCondition未達 → スキップ", rate.Timestamp.Format("2006-01-02T15:04Z"))
		return false, nil
	}

	log.Printf("[%s] Claude起動 (fills=%d)", rate.Timestamp.Format("2006-01-02T15:04Z"), result.FillCount)

	prevSessionID, _ := st.LoadSessionID(ctx)
	sessionID, err := runClaude(stateDir, prevSessionID)
	if err != nil {
		return false, fmt.Errorf("claude: %v", err)
	}

	if sessionID != "" && sessionID != prevSessionID {
		if err := st.SaveSessionID(ctx, sessionID); err != nil {
			return false, fmt.Errorf("save session id: %v", err)
		}
		log.Printf("session_id 更新: %s", sessionID)
	}

	if sessionID != "" {
		if err := saveSessionLog(stateDir, sessionID); err != nil {
			log.Printf("warn: save session log: %v", err)
		}
	}

	return false, nil
}

// setupBrokers は TradingBroker / MarketBroker / HistoricalBroker（historical時のみ）を返す。
// dataDone=true のとき historical データ終端。
func setupBrokers(stateDir, csvPath, pairStr string) (broker.TradingBroker, broker.MarketBroker, broker.HistoricalBroker, bool, error) {
	pair := currency.Pair(pairStr)

	if csvPath == "" {
		tb, mb, err := setupOandaBrokers()
		if err != nil {
			return nil, nil, nil, false, err
		}
		return tb, mb, nil, false, nil
	}

	b, err := broker.NewHistoricalBroker(pair, csvPath)
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("historical broker: %w", err)
	}

	snapPath := filepath.Join(stateDir, "broker_snapshot.json")
	snap, err := broker.LoadHistoricalBrokerSnapshot(snapPath)
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("load broker snapshot: %w", err)
	}
	b.Restore(snap)

	if !b.Advance() {
		return nil, nil, nil, true, nil
	}

	return b, b, b, false, nil
}

func runClaude(stateDir, prevSessionID string) (string, error) {
	systemPrompt := "あなたはFX取引エージェントです。" +
		stateDir + "/CLAUDE.md の行動手順・戦略方針・コマンドリファレンスに従って行動してください。"
	prompt := "CLAUDE.mdの行動手順に従って行動してください。"
	fxdPath, err := resolveMultiFX()
	if err != nil {
		return "", fmt.Errorf("fxd not found: %w", err)
	}
	allowedTool := fmt.Sprintf("Bash(%s *)", fxdPath)

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

// saveSessionLog は Claude Code の jsonl をパースして stateDir/session.log に上書き保存する。
// セッションが --resume で継続されるたびに全会話が最新状態で上書きされる。
func saveSessionLog(stateDir, sessionID string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home dir: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	projectKey := strings.ReplaceAll(cwd, "/", "-")
	jsonlPath := filepath.Join(home, ".claude", "projects", projectKey, sessionID+".jsonl")

	data, err := os.ReadFile(jsonlPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read jsonl: %w", err)
	}

	md := formatSessionLog(sessionID, data)
	return os.WriteFile(filepath.Join(stateDir, "session.log"), []byte(md), 0644)
}

func formatSessionLog(sessionID string, data []byte) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# セッションログ\n\n")
	sb.WriteString(fmt.Sprintf("- **セッションID**: %s\n\n", sessionID))
	sb.WriteString("---\n\n")

	type message struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"message"`
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024) // jsonlの行が大きい場合に備えて拡張
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var msg message
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if msg.Type != "assistant" {
			continue
		}

		role := "**Assistant**"

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
										content += fmt.Sprintf("%s", cm["text"])
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
