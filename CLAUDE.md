# CLAUDE.md

Claude Code がこのリポジトリで作業する際の指針。AGENTS.md と重複する内容は AGENTS.md を正とする。

## 必須確認コマンド

コードを変更したら必ず実行すること:

```bash
go build ./...
go vet ./...
```

## このプロジェクトで守ること

- LLM 呼び出しは `internal/commander/` だけに置く
- `rule.FloorRule` の条件は緩めない
- `pool.ValidateTransition` をバイパスしない
- `OrderRequest.StopLoss` はゼロ値で渡さない

詳細は [AGENTS.md](./AGENTS.md) を参照。
