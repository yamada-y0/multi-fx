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
	"github.com/yamada/multi-fx/internal/pool"
	"github.com/yamada/multi-fx/internal/store"
	"github.com/yamada/multi-fx/pkg/currency"
)

func main() {
	stateDir := flag.String("state-dir", "", "JSONStoreのディレクトリパス（必須）")
	subPoolID := flag.String("subpool", "", "対象SubPoolID（必須）")
	systemPrompt := flag.String("system-prompt", "", "Claudeに渡す戦略方針テキスト（必須）")
	flag.Parse()

	if *stateDir == "" || *subPoolID == "" || *systemPrompt == "" {
		fmt.Fprintln(os.Stderr, "Usage: kick --state-dir <dir> --subpool <id> --system-prompt <prompt>")
		flag.PrintDefaults()
		os.Exit(1)
	}

	ctx := context.Background()
	id := pool.SubPoolID(*subPoolID)

	st, err := store.NewJSONStore(*stateDir)
	if err != nil {
		log.Fatalf("store: %v", err)
	}

	wakeupStore := agent.NewJSONWakeupStore(filepath.Join(*stateDir, "wakeup.json"))

	// SubPoolの存在確認
	if _, err := st.LoadSubPool(ctx, id); err != nil {
		log.Fatalf("subpool not found: %v", err)
	}

	// WakeupCondition評価
	cond, ok, err := wakeupStore.Load(ctx, id)
	if err != nil {
		log.Fatalf("wakeup store: %v", err)
	}

	if ok {
		// スタブ: 現在レートを固定値で代替（RealApiBroker未実装のため）
		rates := map[currency.Pair]decimal.Decimal{
			currency.USDJPY: decimal.NewFromFloat(150.0),
		}
		now := time.Now()

		if !cond.IsMet(now, rates) {
			log.Printf("WakeupCondition未達 → 終了")
			os.Exit(0)
		}

		// 条件達成: WakeupConditionを削除
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
