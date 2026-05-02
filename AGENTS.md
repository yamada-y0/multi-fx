# AGENTS.md

このファイルはAIエージェント（Claude Codeなど）がこのリポジトリで作業する際の指針。

## プロジェクト概要

FX自動取引システムの技術検証。Go + AWS Lambda + DynamoDB 構成。詳細は [README.md](./README.md) を参照。

## ビルド・テスト

```bash
go build ./...   # ビルド確認（エラーゼロが必須）
go test ./...    # テスト実行
go vet ./...     # 静的解析
```

コード変更後は必ず `go build ./...` でエラーがないことを確認すること。

## 設計上の絶対ルール（変更禁止）

1. **フロアルール**: `rule.FloorRule` は全戦略共通の安全装置。条件を緩める変更は行わない
2. **ライフサイクル一方向**: Active → Suspended → Terminated の順序は変えない。`pool.ValidateTransition` をバイパスしない
3. **LLM 非決定性の境界**: LLM を呼び出すコードは `internal/commander/` にのみ置く。他のパッケージに LLM 呼び出しを追加しない
4. **逆指値必須**: `OrderRequest.StopLoss` は必須。ゼロ値で発注するコードを書かない
5. **資金移動禁止**: SubPool 間の直接資金移動は行わない。Master ← SubPool の返還を経由すること

## コーディング規約

- **パッケージ構成**: `internal/` は外部公開しない。外部から使う型は `pkg/` に置く
- **インターフェース**: 実装より先にインターフェースを定義する。テスト可能性を優先
- **エラーハンドリング**: `fmt.Errorf("パッケージ名: %w", err)` でラップして文脈を付ける
- **時刻**: `time.Now()` を直接呼ばず `pkg/clock.Clock` を使う（テスト差し替えのため）
- **コメント**: 未実装箇所は `// TODO:` で理由と設計メモを残す

## 依存方向（循環インポート禁止）

```
cmd/* → internal/commander
         internal/commander → internal/pool, internal/rule
         internal/order     → internal/pool
         internal/broker    → internal/order, internal/pool
         internal/agent     → internal/pool
         internal/*         → pkg/currency, pkg/clock
```

`internal/pool` は他の `internal/` パッケージをインポートしない（最下層）。

## 実装を始める前に確認すること

- インターフェース定義が変わる場合は、依存するすべてのパッケージへの影響を確認する
- `internal/pool/sub.go` の `SubPool` インターフェースは多くのパッケージが依存する。シグネチャ変更は慎重に
- DynamoDB テーブル設計が固まっていない段階では `store.StateStore` のインメモリ実装でテストを書く

## 未決事項（実装前に設計を固める）

- `internal/agent/Strategy.OnTick` のシグネチャ詳細（戦略ごとのパラメータ）
- `internal/order/virtual.go` の仮想約定価格算出ロジック
- `internal/commander/llm.go` のプロンプト設計・JSON スキーマ
- DynamoDB テーブル設計（PK/SK 構造）
- Commander の LLM 呼び出し頻度（毎ティック vs 間引き）
