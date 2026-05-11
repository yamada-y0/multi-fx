package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

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
		"### cancel-order — 未約定注文のキャンセル\n\n" +
		"OrderIDはsnapshotのOrders[].IDから取得すること。\n\n" +
		"```bash\n" +
		mfx + " cancel-order \\\n" +
		"  --state-dir " + sd + " \\\n" +
		"  --order-id <OrderID>\n" +
		"```\n\n" +
		"### note — 気づき・要望の記録\n\n" +
		"コマンドの不足・改善点・判断の根拠など、開発者へのフィードバックを記録する。\n" +
		"`notes.md` にタイムスタンプ付きで追記される。\n" +
		"「このコマンドがあれば〜できた」「この情報が欲しかった」などを積極的に残すこと。\n\n" +
		"```bash\n" +
		mfx + " note --state-dir " + sd + " \"気づいたこと・要望\"\n" +
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
