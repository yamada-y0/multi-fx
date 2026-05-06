# fxd

AIエージェントを組み込んだFX自動取引システムの技術検証プロジェクト。

Claude Codeをトレーディングエージェントとして組み込み、定期キック型アーキテクチャでバックテストを実行する。
複数のエージェント（SubPool）を並走させ、「フロアルール（初期割り当てを下回ったら強制停止）」を全体の安全装置とする。

## アーキテクチャ概要

```
cron（ローカル or CI）
  ↓ 定期キック
cmd/kick（WakeupCondition判定 → Claudeを起動）
  ↓ --allowedTools "Bash(fxd *)"
Claude Code（FXエージェントとして動作）
  ↓ Bashツール経由でサブコマンドを呼び出す
cmd/cli（fxd）
  ├── snapshot  — 口座状態の確認
  ├── market    — 市場データの取得
  ├── submit-order — 発注・決済
  └── set-wakeup   — 次回起動条件の設定
  ↓
internal/（SubPool・Broker・Store・RuleEngine）
```

### 実行モデル

常駐型ではなく**定期キック型**。

1. `kick` が起動する
2. HistoricalBrokerを1ティック進める（`broker_snapshot.json`から復元 → `Advance()`）
3. レートを取得し、`internal/tick.Ticker.Tick()` で1ティック分の処理を実行
   - OnRate（含み損益更新） → SyncFills（約定判定） → フロアルール評価 → WakeupCondition評価 → SubPool保存
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
│   ├── pool/              # SubPool・ライフサイクル FSM の型定義と実装
│   ├── agent/             # WakeupCondition・WakeupStore
│   ├── order/             # Aggregator（変換・中継）・PositionIDMapper
│   ├── broker/            # Broker抽象・HistoricalBroker実装
│   ├── rule/              # フロアルールなど決定論的強制アクション
│   └── store/             # 状態永続化（JSONStore実装済み）
└── pkg/
    ├── currency/          # 通貨ペア・レート型
    ├── market/            # Candle（足データ）
    ├── order/             # Order・Fill・Position・OrderType（Broker向け汎用型）
    └── clock/             # 時刻抽象（テスト差し替え用）
```

## エージェントの管理

エージェントはUUIDベースのディレクトリで管理される。

```
~/.fxd/agents/
└── <uuid>/
    ├── subpool.json          # SubPoolの状態
    ├── broker_snapshot.json  # HistoricalBrokerの状態（cursor + PendingOrders）
    ├── wakeup.json           # 次回起動条件
    ├── CLAUDE.md             # 戦略方針・コマンドリファレンス（Claudeに渡す）
    └── logs/
        └── <timestamp>_<session>.md  # Claudeのセッションログ（Markdown）
```

### エージェントの作成

```bash
fxd init-subpool --base-dir ~/.fxd/agents --balance 1000000
```

生成されたディレクトリの `CLAUDE.md` に戦略方針とコマンドパスを記入する。

### バックテストの実行（1ティック）

```bash
kick \
  --state-dir ~/.fxd/agents/<uuid> \
  --data ./testdata/USDJPY_2024_h1.csv \
  --pair USDJPY
```

## CLIサブコマンドリファレンス

### snapshot — 口座状態の確認

```bash
fxd snapshot --state-dir <state-dir>
```

### market — 市場データの取得（新しい順）

```bash
fxd market \
  --state-dir <state-dir> \
  --data <csv-path> \
  --pair USDJPY \
  --n 20
```

HistoricalBrokerの現在位置から過去N本のローソク足を返す（インデックス0が最新）。

### submit-order — 発注・決済

```bash
# 新規発注
fxd submit-order \
  --state-dir <state-dir> \
  --data <csv-path> \
  --pair USDJPY \
  --side <long|short> \
  --lots <ロット数> \
  --stop-loss <価格> \
  --order-type <market|limit>

# 決済
fxd submit-order \
  --state-dir <state-dir> \
  --data <csv-path> \
  --pair USDJPY \
  --side <long|short> \
  --lots <ロット数> \
  --stop-loss <価格> \
  --close-position-id <PositionID>
```

### set-wakeup — 次回起動条件の設定（OR評価）

```bash
# 指定時刻以降
fxd set-wakeup --state-dir <state-dir> --after <RFC3339>

# レートが価格以上
fxd set-wakeup --state-dir <state-dir> --price-gte USDJPY:150.0

# レートが価格以下
fxd set-wakeup --state-dir <state-dir> --price-lte USDJPY:148.0

# 未約定注文が約定したら
fxd set-wakeup --state-dir <state-dir> --any-fill
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

- **フロアルール**: `EquityBalance < InitialBalance` で SubPool を強制停止。`rule.FloorRule` の条件は緩めない
- **ライフサイクル一方向**: Active → Suspended → Terminated のみ。`pool.ValidateTransition` をバイパスしない
- **StopLoss必須**: `OrderRequest.StopLoss` はゼロ値で渡さない
- **Broker独立**: `broker` パッケージは `pool` に依存しない。変換は Aggregator が担う
- **HistoricalBrokerSnapshot**: cursorとPendingOrdersをまとめてJSONで永続化し、プロセス間でBroker状態を共有する

## 未実装

- `internal/broker/realapi.go` — 本番ブローカーAPI接続
- cronによる自動定期実行
