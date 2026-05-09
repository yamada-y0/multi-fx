# fxd

AIエージェントを組み込んだFX自動取引システムの技術検証プロジェクト。

Claude Codeをトレーディングエージェントとして組み込み、定期キック型アーキテクチャで取引判断を行う。
OANDA REST API v20 を使ったリアル取引と、Dukascopy CSV を使ったヒストリカルバックテストの両方に対応する。

## アーキテクチャ概要

```
cron（ローカル or CI）
  ↓ 定期キック
cmd/kick（FetchFillEvents同期 → WakeupCondition判定 → Claudeを起動）
  ↓ --allowedTools "Bash(fxd *)"
Claude Code（FXエージェントとして動作）
  ↓ Bashツール経由でサブコマンドを呼び出す
cmd/cli（fxd）
  ├── snapshot     — 口座状態の確認（OANDA直接参照）
  ├── market       — 市場データの取得
  ├── submit-order — 発注・決済
  ├── set-wakeup   — 次回起動条件の設定
  └── init-agent   — エージェントディレクトリの初期化
  ↓
internal/broker（OANDA API / HistoricalBroker）
```

### 実行モデル

常駐型ではなく**定期キック型**。

1. `kick` が起動する
2. Broker を構築（OANDAモード or HistoricalBroker）
3. レートを取得し、`internal/tick.Ticker.Tick()` で1ティック分の処理を実行
   - FetchFillEvents（約定同期 + lastFillEventID更新） → WakeupCondition評価
4. WakeupConditionが達成されていれば `claude -p` を起動
5. Claude（FXエージェント）が `fxd` サブコマンドを使って判断・発注・WakeupCondition設定
6. Claude終了後、セッションログをMarkdownとして `state-dir/logs/` に保存

### ディレクトリ構成

```
fxd/
├── cmd/
│   ├── kick/              # 定期キックエントリポイント（cronから呼ばれる）
│   └── cli/               # CLIサブコマンド群（Claude Codeが使用）
├── internal/
│   ├── tick/              # 1ティック分の処理サイクル
│   ├── agent/             # WakeupCondition・WakeupStore
│   ├── broker/            # Broker抽象・HistoricalBroker・OandaBroker実装
│   └── store/             # 状態永続化（lastFillEventID・sessionID）
└── pkg/
    ├── currency/          # 通貨ペア・レート型
    ├── market/            # Candle（足データ）
    ├── order/             # Order・FillEvent・Position・AccountInfo（Broker向け汎用型）
    └── clock/             # 時刻抽象（テスト差し替え用）
```

## セットアップ

### 環境変数

OANDA デモ口座の認証情報を `~/.fxd/practice.env` に記述する:

```bash
# ~/.fxd/practice.env (chmod 600)
OANDA_API_TOKEN=your-token
OANDA_ACCOUNT_ID=your-account-id
OANDA_PRACTICE=true
```

本番口座は `~/.fxd/live.env` を別途作成し、`OANDA_PRACTICE=false` にする。

### エージェントの作成

```bash
fxd init-agent --base-dir ./agents
```

生成されたディレクトリの `CLAUDE.md` に戦略方針を記入する。

## CLIサブコマンドリファレンス

### snapshot — 口座状態の確認

OANDAから直接残高・ポジション・注文を取得する。

```bash
fxd snapshot --state-dir <state-dir> [--pair USDJPY]
```

### market — 市場データの取得（新しい順）

```bash
# OANDAモード
. ~/.fxd/practice.env && fxd market \
  --state-dir <state-dir> \
  --pair USDJPY \
  --n 20 \
  --granularity M1

# historicalモード（CSV）
fxd market \
  --state-dir <state-dir> \
  --data <csv-path> \
  --pair USDJPY \
  --n 20
```

### submit-order — 発注・決済

```bash
# 新規発注
fxd submit-order \
  --state-dir <state-dir> \
  --pair USDJPY \
  --side <long|short> \
  --lots <ロット数> \
  --stop-loss <価格> \
  --order-type <market|limit>

# 決済
fxd submit-order \
  --state-dir <state-dir> \
  --pair USDJPY \
  --side <long|short> \
  --lots <ロット数> \
  --stop-loss <価格> \
  --close-position-id <PositionID>
```

### set-wakeup — 次回起動条件の設定（OR評価）

複数フラグを同時指定するとOR評価になる。

```bash
fxd set-wakeup --state-dir <state-dir> \
  [--after <RFC3339>] \
  [--price-gte USDJPY:150.0] \
  [--price-lte USDJPY:148.0] \
  [--any-fill]
```

## kick — 定期キック

```bash
# OANDAモード
. ~/.fxd/practice.env && kick --state-dir <state-dir> --pair USDJPY

# historicalモード（1ティック）
kick --state-dir <state-dir> --data <csv-path> --pair USDJPY

# historicalモード（データ終端まで連続実行）
kick --state-dir <state-dir> --data <csv-path> --pair USDJPY --loop
```

## エージェントのstate-dir構成

```
<state-dir>/
├── CLAUDE.md                        # 戦略方針・コマンドリファレンス（Claudeに渡す）
├── wakeup.json                      # 次回起動条件
├── last_fill_event_id.json          # FetchFillEvents の sinceID カーソル
├── session_id.json                  # Claude Code のセッション継続用ID
├── broker_snapshot.json             # HistoricalBroker の状態（historicalモードのみ）
└── logs/
    └── <timestamp>_<session>.md     # Claudeのセッションログ（Markdown）
```

## 開発

```bash
# ビルド確認
go build ./...
go vet ./...

# テスト
go test ./...

# バイナリビルド
go build -o /tmp/fxd ./cmd/cli
go build -o /tmp/kick ./cmd/kick
```

## 設計上の制約

- **StopLoss必須**: `Order.StopLoss` はゼロ値で発注するコードを書かない
- **Broker独立**: `internal/broker` は他の `internal/` パッケージに依存しない
- **単一口座前提**: SubPool等の仮想分割は行わない。OANDA口座を直接参照する
