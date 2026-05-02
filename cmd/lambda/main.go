package main

// Lambda エントリポイント
//
// cron（ローカル or GitHub Actions）からキックされるたびに1ティックを実行する。
//
// 処理フロー:
//   1. StateStore から MasterPool・SubPool の状態を復元
//   2. Broker からレートを取得し RateFeed へ Publish
//   3. 各 SubPool の OnRate を呼び出し含み損益を更新
//   4. RuleEngine で全 SubPool を評価し、強制アクションがあれば即時実行
//   5. Commander.Tick を呼び出し LLM 指示を処理（頻度間引き可）
//   6. 各 SubAgent の OnTick を呼び出し発注判断
//   7. Aggregator.Flush でネッティング → Broker 発注 → Fill 配送
//   8. 状態を StateStore に保存して終了

func main() {
	// TODO: Lambda ハンドラの登録
}
