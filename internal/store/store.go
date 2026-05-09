package store

import "context"

// StateStore はエージェント状態を永続化する抽象
// 本番: JSON実装（json.go）、テスト: インメモリ実装（memory.go）
type StateStore interface {
	// FetchFillEvents の sinceID 永続化（ブローカーごとに1つ）
	SaveLastFillEventID(ctx context.Context, id string) error
	LoadLastFillEventID(ctx context.Context) (string, error)

	// セッションID永続化（Claude Code のセッション継続用）
	SaveSessionID(ctx context.Context, id string) error
	LoadSessionID(ctx context.Context) (string, error)
}
