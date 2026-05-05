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

1. **フロアルール**: `rule.FloorRule` は全戦略共通の安全装置。`ThresholdRatio` の条件を緩める変更は行わない
2. **ライフサイクル一方向**: Active → Suspended → Terminated の順序は変えない。`pool.ValidateTransition` をバイパスしない
3. **StopLoss必須**: `OrderRequest.StopLoss` はゼロ値で発注するコードを書かない
4. **Broker独立**: `internal/broker` は `internal/pool` に依存しない。変換は `internal/order.Aggregator` が担う
5. **資金移動禁止**: SubPool間の直接資金移動は行わない

## コーディング規約

- **パッケージ構成**: `internal/` は外部公開しない。外部から使う型は `pkg/` に置く
- **エラーハンドリング**: `fmt.Errorf("パッケージ名: %w", err)` でラップして文脈を付ける
- **コメント**: 非自明な制約・不変条件・特定バグへのワークアラウンドのみコメントを書く

## 依存方向（循環インポート禁止）

```
cmd/*  →  internal/tick, internal/broker, internal/order, internal/pool, internal/agent, internal/rule, internal/store
          internal/tick    → internal/order, internal/pool, internal/agent, internal/rule, internal/store
          internal/order   → internal/pool, internal/store
          internal/broker  → pkg/order, pkg/currency, pkg/market
          internal/pool    → pkg/currency, pkg/order  （他のinternalパッケージをインポートしない）
          internal/agent   → internal/pool, pkg/currency
          internal/rule    → internal/pool
          internal/store   → internal/pool
          internal/*       → pkg/currency, pkg/order, pkg/market, pkg/clock
```

## 変更時の注意

- `internal/pool/sub.go` の `SubPool` インターフェースは多くのパッケージが依存する。シグネチャ変更は慎重に
- `internal/broker/broker.go` の `HistoricalBrokerSnapshot` を変更する場合は、既存の `broker_snapshot.json` との互換性を確認する
- `internal/agent/wakeup.go` の `WakeupCondition.IsMet()` シグネチャを変更する場合は `internal/tick/tick.go` も合わせて変更する
