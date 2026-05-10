# AGENTS.md

AIエージェント（Claude Codeなど）がこのリポジトリで作業する際の指針。

## プロジェクト概要

定期キック型のFX自動取引システム。Claude Codeをトレーディングエージェントとして組み込む。
詳細は [README.md](./README.md) を参照。

## ビルド・テスト

コード変更後は必ず実行すること:

```bash
go build ./...
go vet ./...
go test ./...
```

## 設計上の絶対ルール（変更禁止）

1. **StopLoss必須**: `Order.StopLoss` はゼロ値で発注するコードを書かない
2. **Broker独立**: `internal/broker` は他の `internal/` パッケージに依存しない
3. **単一口座前提**: SubPool等の仮想分割は行わない。ポジション・残高はOANDAから直接取得する

## コーディング規約

- **パッケージ構成**: `internal/` は外部公開しない。外部から使う型は `pkg/` に置く
- **エラーハンドリング**: `fmt.Errorf("パッケージ名: %w", err)` でラップして文脈を付ける
- **コメント**: 非自明な制約・不変条件・特定バグへのワークアラウンドのみコメントを書く

## パッケージ構成

| パッケージ | 役割 |
|---|---|
| `cmd/kick` | cronエントリポイント。WakeupCondition判定→Claude起動 |
| `cmd/cli` | Claude向けCLIサブコマンド（snapshot/market/submit-order/set-wakeup/init-agent）|
| `internal/broker` | Broker抽象（TradingBroker/MarketBroker）とOANDA・historical実装 |
| `internal/tick` | 1ティック分のサイクル（FetchFillEvents同期→WakeupCondition評価） |
| `internal/agent` | WakeupCondition定義・JSONストア |
| `internal/store` | 状態永続化（lastFillEventID・sessionID） |
| `pkg/indicator` | テクニカル指標算出（SMA/EMA/RSI/ATR/MACD） |
| `pkg/order` | 共有型（Order/Position/FillEvent/AccountInfo/CalendarEvent等） |
| `pkg/market` | Candle型 |
| `pkg/currency` | Pair/Rate型 |

## 依存方向（循環インポート禁止）

```
cmd/*           →  internal/tick, internal/broker, internal/agent, internal/store, pkg/indicator
internal/tick   →  internal/broker, internal/agent, internal/store, pkg/currency
internal/broker →  pkg/order, pkg/currency, pkg/market
internal/agent  →  pkg/currency
internal/store  （他のinternalパッケージをインポートしない）
pkg/indicator   →  pkg/market
internal/*      →  pkg/currency, pkg/order, pkg/market, pkg/clock
```

## Brokerインターフェース

`Broker` は `TradingBroker` と `MarketBroker` を統合したインターフェース。

- **TradingBroker**: SubmitOrder / CancelOrder / FetchPositions / FetchOrders / FetchFillEvents / FetchAccount
- **MarketBroker**: FetchRate / FetchCandles / FetchCalendar

OANDAモードでは `NewOandaTradingBroker`（practice環境）と `NewOandaMarketBroker`（live環境）を別々に生成して使い分けられる。環境変数 `OANDA_MARKET_API_TOKEN` / `OANDA_MARKET_PRACTICE` を設定した場合のみ別インスタンスを使用し、未設定時はTradingBrokerと同じトークンにフォールバックする。

## 変更時の注意

- `TradingBroker` / `MarketBroker` インターフェースを変更する場合は `historical.go` / `oanda.go` および `cmd/cli` / `cmd/kick` を合わせて変更する
- `HistoricalBrokerSnapshot` を変更する場合は既存の `broker_snapshot.json` との互換性を確認する
- `WakeupCondition.IsMet()` シグネチャを変更する場合は `internal/tick/tick.go` も合わせて変更する
