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

## 依存方向（循環インポート禁止）

```
cmd/*          →  internal/tick, internal/broker, internal/agent, internal/store
internal/tick  →  internal/broker, internal/agent, internal/store, pkg/currency
internal/broker → pkg/order, pkg/currency, pkg/market
internal/agent  → pkg/currency
internal/store  （他のinternalパッケージをインポートしない）
internal/*      → pkg/currency, pkg/order, pkg/market, pkg/clock
```

## 変更時の注意

- `internal/broker/broker.go` の `Broker` インターフェースを変更する場合は、`historical.go` / `oanda.go` および `cmd/cli` / `cmd/kick` の stubBroker 相当を合わせて変更する
- `internal/broker/broker.go` の `HistoricalBrokerSnapshot` を変更する場合は、既存の `broker_snapshot.json` との互換性を確認する
- `internal/agent/wakeup.go` の `WakeupCondition.IsMet()` シグネチャを変更する場合は `internal/tick/tick.go` も合わせて変更する
