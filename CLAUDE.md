# CLAUDE.md

Claude Code がこのリポジトリで作業する際の指針。AGENTS.md と重複する内容は AGENTS.md を正とする。

## コミュニケーション

- ユーザーへの受け答えは日本語で行うこと
- コミットメッセージはタイトル行を英語、本文（詳細）を日本語で書くこと

## 必須確認コマンド

コードを変更したら必ず実行すること:

```bash
go build ./...
go vet ./...
```

## このプロジェクトで守ること

- `Order.StopLoss` はゼロ値で渡さない
- `internal/broker` は他の `internal/` パッケージに依存しない
- ポジション・残高はOANDAから直接取得する（ローカル台帳を持たない）

詳細は [AGENTS.md](./AGENTS.md) を参照。
