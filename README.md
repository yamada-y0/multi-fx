# multi-fx

AIエージェントを組み込んだFX自動取引システムの技術検証プロジェクト。

複数の仮想口座（SubPool）で異なる戦略を並走させ、「フロアルール（初期割り当てを下回ったら強制停止）」を全体の安全装置とする。

## アーキテクチャ概要

```
【Broker層】
  HistoricalDataBroker  ← 開発・バックテスト時
  RealApiBroker         ← 将来の差し替え先（未実装）
       ↓ レート配信
【RateFeed / EventBus】
       ↓
【Order Aggregator】
  全SubPoolの発注をネッティングして両建てを防止
  純ポジションの差分だけBrokerへ発注
       ↓
【Sub Pool × n】
  仮想口座（残高・ポジション・損益を自己管理）
  └── SubAgent（戦略ロジック）
【Commander AIエージェント】
  Master Poolを管理。SubPool生成・停止・解約を指示
```


## ディレクトリ構成

```
multi-fx/
├── cmd/
│   ├── lambda/        # Lambda エントリポイント（cron kick）
│   └── dev/           # ローカルバックテスト用バイナリ
├── internal/
│   ├── commander/     # Commander AI・LLM 指示チャネル
│   ├── pool/          # MasterPool・SubPool・ライフサイクル FSM
│   ├── agent/         # 戦略インターフェース・ファクトリ
│   ├── order/         # 発注集約・ネッティング
│   ├── broker/        # Broker 抽象・Historical 実装
│   ├── feed/          # RateFeed・EventBus
│   ├── rule/          # フロアルールなど決定論的強制アクション
│   └── store/         # 状態永続化（DynamoDB）
└── pkg/
    ├── currency/      # 通貨ペア型
    └── clock/         # 時刻抽象（テスト差し替え用）
```

## 実行モデル

常駐型ではなく**定期キック型**。

```
cron（ローカル or GitHub Actions）
  ↓ 定期キック
Lambda（判断ロジック実行）
  ↓
DynamoDB（ポジション・残高の状態管理）
```

## 開発

```bash
# ビルド確認
go build ./...

# テスト
go test ./...

# バックテスト（実装後）
go run ./cmd/dev -data ./testdata/USDJPY_2024.csv
```

## 設計上の制約

- **逆指値必須**: `OrderRequest.StopLoss` が必須フィールド。逆指値なしの発注は打たない
- **フロアルール**: `EquityBalance < InitialBalance` で SubPool を強制停止
- **ライフサイクル一方向**: Active → Suspended → Terminated のみ。復活は新規 SubPool 生成として扱う
- **LLM 非決定性の境界**: `commander.InstructionChannel.Request()` だけが LLM を呼ぶ。以降は全て決定論的

## 未実装（TODO）

- `internal/broker/historical.go` — CSV/OHLCV データ再生
- `internal/order/virtual.go` — SubPool ごとの仮想約定価格算出
- `internal/commander/llm.go` — Claude API 呼び出し・プロンプト設計
- `internal/store/dynamo.go` — DynamoDB テーブル設計・実装
- `cmd/lambda/main.go` — Lambda ハンドラ登録
- 各種戦略実装（`internal/agent/` 配下）
