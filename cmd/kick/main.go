package main

import (
	"context"
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
	"github.com/yamada/multi-fx/internal/pool"
	"github.com/yamada/multi-fx/internal/store"
	"github.com/yamada/multi-fx/pkg/currency"
)

func main() {
	stateDir := flag.String("state-dir", "", "JSONStoreのディレクトリパス（必須）")
	subPoolID := flag.String("subpool", "", "対象SubPoolID（必須）")
	systemPrompt := flag.String("system-prompt", "", "Claudeに渡す戦略方針テキスト（必須）")
	csvPath := flag.String("data", "", "historical モード: Dukascopy CSVファイルパス")
	pair := flag.String("pair", "USDJPY", "historical モード: 通貨ペア")
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

	// SubPoolの存在確認
	if _, err := st.LoadSubPool(ctx, id); err != nil {
		log.Fatalf("subpool not found: %v", err)
	}

	wakeupStore := agent.NewJSONWakeupStore(filepath.Join(*stateDir, "wakeup.json"))

	// レート取得
	rate, done, err := fetchRate(ctx, *stateDir, *csvPath, *pair)
	if err != nil {
		log.Fatalf("fetch rate: %v", err)
	}
	if done {
		log.Printf("historical データ終端 → 終了")
		os.Exit(0)
	}

	// WakeupCondition評価
	cond, ok, err := wakeupStore.Load(ctx, id)
	if err != nil {
		log.Fatalf("wakeup store: %v", err)
	}

	if ok {
		rates := map[currency.Pair]decimal.Decimal{rate.Pair: rate.Bid}
		if !cond.IsMet(rate.Timestamp, rates) {
			log.Printf("WakeupCondition未達 → 終了")
			os.Exit(0)
		}
		if err := wakeupStore.Delete(ctx, id); err != nil {
			log.Fatalf("wakeup delete: %v", err)
		}
		log.Printf("WakeupCondition達成 → Claude起動")
	} else {
		log.Printf("WakeupConditionなし → Claude起動")
	}

	// Claude起動
	cmd := exec.Command("claude", "-p", *systemPrompt)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Fatalf("claude: %v", err)
	}
}

// fetchRate はモードに応じてレートを取得する
// historical モード（csvPath != ""）の場合はカーソルを1進めて保存する
// done=true のときデータ終端に達したことを示す
func fetchRate(ctx context.Context, stateDir, csvPath, pairStr string) (currency.Rate, bool, error) {
	if csvPath == "" {
		// real モード: スタブ（RealApiBroker未実装）
		return currency.Rate{
			Pair:      currency.Pair(pairStr),
			Bid:       decimal.NewFromFloat(150.0),
			Ask:       decimal.NewFromFloat(150.0),
			Timestamp: time.Now(),
		}, false, nil
	}

	// historical モード
	statePath := filepath.Join(stateDir, "historical_state.json")
	hs, err := store.LoadHistoricalState(statePath)
	if err != nil {
		return currency.Rate{}, false, fmt.Errorf("load historical state: %w", err)
	}

	b, err := broker.NewHistoricalBroker(currency.Pair(pairStr), csvPath)
	if err != nil {
		return currency.Rate{}, false, fmt.Errorf("historical broker: %w", err)
	}

	// カーソル位置まで復元
	for i := 0; i < hs.Cursor; i++ {
		if !b.Advance() {
			return currency.Rate{}, true, nil
		}
	}

	rate, err := b.FetchRate(ctx, currency.Pair(pairStr))
	if err != nil {
		return currency.Rate{}, false, fmt.Errorf("fetch rate: %w", err)
	}

	// 次回のために1進める
	if !b.Advance() {
		// 今回が最終ティック: カーソルは保存しない（次回呼び出しで終端検知）
		// cursor を終端+1 にして終端を記録する
		if err := store.SaveHistoricalState(statePath, store.HistoricalState{Cursor: hs.Cursor + 1}); err != nil {
			return currency.Rate{}, false, fmt.Errorf("save historical state: %w", err)
		}
		return rate, false, nil
	}

	if err := store.SaveHistoricalState(statePath, store.HistoricalState{Cursor: hs.Cursor + 1}); err != nil {
		return currency.Rate{}, false, fmt.Errorf("save historical state: %w", err)
	}

	return rate, false, nil
}
