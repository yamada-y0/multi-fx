# multi-fx

AIエージェントを組み込んだFX自動取引システムの技術検証プロジェクト。

複数の仮想口座（SubPool）で異なる戦略を並走させ、「フロアルール（初期割り当てを下回ったら強制停止）」を全体の安全装置とする。

## アーキテクチャ概要

```
【Commander】
  MasterPoolの状態をLLMに渡し、SubPoolのライフサイクルを制御
  （生成・停止・解約・Agentへの指示）
       ↓
【Agent × n】
  SubPoolを1つ担当。Strategy（発注判断）を呼び出し、
  OrderRequestをAggregatorへ渡す
  └── Strategy: OnTick(Snapshot, MarketContext) → []OrderRequest
       ↓
【Order Aggregator】
  pool.OrderRequest を pkg/order.Order に変換してBrokerへ中継
  BrokerからのFillをpool.Fillに変換してSubPoolへ配送
  PositionIDのマッピングを管理・Fill履歴をStoreへ保存
       ↓
【Broker】
  証券会社APIのラッパー。発注・約定判定・ポジション保持・レート提供
  HistoricalBroker（バックテスト用）/ RealApiBroker（本番、未実装）

【SubPool × n】
  純粋な仮想口座。残高・ポジション・ライフサイクルのみを管理
  OnRate / OnFill で状態を更新する（発注判断はしない）

【StateStore】
  SubPoolスナップショット・Fill履歴・MasterPool残高を永続化
  MemoryStore（バックテスト用）/ DynamoStore（本番、未実装）
```

## 依存の方向性

```
pkg/currency  pkg/market  pkg/order   ← 汎用層（外部依存なし、誰でも参照可）
       ↑              ↑         ↑
    pool    ←→    agent        broker  ← 内部層（poolとbrokerは互いを知らない）
       ↑                ↑         ↑
    rule            internal/market   |
       ↑                ↑            |
    commander        order（Aggregator）← 変換責務を持つ唯一の層
                          ↓
                        store         ← 永続化層
```

**設計の考え方**

- `broker` は証券会社APIのラッパーとして独立させる。`pool` など内部概念を知らず、`pkg/order` の汎用型のみを扱う
- `pool.OrderRequest`（Agent→Aggregator間）と `pkg/order.Order`（Aggregator→Broker間）を分離し、内部概念（SubPoolID・OrderIntent・ClosePositionIDのシステム内ID）の漏れを防ぐ
- Aggregatorが両者の変換責務を持ち、PositionIDのマッピング（初期実装は恒等変換）を管理する
- Fill永続化はAggregatorのSyncFills内でStoreへ直接書く。イベントバスは使わない
- LLMの非決定性は `Strategy.OnTick` と `commander.InstructionChannel` の2箇所に閉じ込める。それ以降は全て決定論的に処理する

## ディレクトリ構成

```
multi-fx/
├── cmd/
│   ├── lambda/            # Lambda エントリポイント（cron kick）※未実装
│   └── dev/               # ローカルバックテスト用バイナリ ※未実装
├── internal/
│   ├── commander/         # Commander AI・LLM 指示チャネル（インターフェース定義済み、LLM実装未）
│   ├── pool/              # SubPool・MasterPool・ライフサイクル FSM の型定義と実装
│   ├── agent/             # Strategy インターフェース・ファクトリ・レジストリ
│   ├── order/             # Aggregator（変換・中継）・PositionIDMapper
│   ├── broker/            # Broker 抽象・HistoricalBroker 実装（RealApi未実装）
│   ├── market/            # MarketDataProvider（足データをMarketContextに組み立て）
│   ├── rule/              # フロアルールなど決定論的強制アクション
│   └── store/             # 状態永続化（MemoryStore実装済み、DynamoDB未実装）
└── pkg/
    ├── currency/          # 通貨ペア・レート型
    ├── market/            # Candle（足データ）・MarketContext
    ├── order/             # Order・Fill・Position・OrderIntent（Broker向け汎用型）
    └── clock/             # 時刻抽象（テスト差し替え用）
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

1ティックの処理フロー:
1. StateStore から MasterPool・SubPool の状態を復元
2. Broker からレートを取得し、各 SubPool の OnRate を呼んで含み損益を更新
3. RuleEngine で全 SubPool を評価し、強制アクションがあれば即時実行
4. Commander.Tick を呼び出し LLM 指示を処理（頻度間引き可）
5. 各 Agent の OnTick を呼び出し発注判断 → Aggregator.SubmitOrder
6. Aggregator.SyncFills で約定確認 → SubPool への Fill 配送・Store への保存
7. 状態を StateStore に保存して終了

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

- **フロアルール**: `EquityBalance < InitialBalance` で SubPool を強制停止
- **ライフサイクル一方向**: Active → Suspended → Terminated のみ。復活は新規 SubPool 生成として扱う
- **LLM 非決定性の境界**: `Strategy.OnTick` と `commander.InstructionChannel.Request()` だけが LLM を呼ぶ。以降は全て決定論的
- **Broker は内部概念を知らない**: `pool` パッケージに依存しない。変換は Aggregator が担う
- **StopLoss は必須**: `OrderRequest.StopLoss` はゼロ値で渡さない

## 未実装（TODO）

- `internal/commander/llm.go` — Claude API 呼び出し・プロンプト設計
- `internal/store/dynamo.go` — DynamoDB テーブル設計・実装
- `cmd/lambda/main.go` — Lambda ハンドラ登録
- `cmd/dev/main.go` — バックテスト実行ループ
- 各種 Agent・Strategy 実装（`internal/agent/` 配下）
- `internal/broker/realapi.go` — 本番ブローカー API 接続
