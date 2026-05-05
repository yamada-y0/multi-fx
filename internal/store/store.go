package store

import (
	"context"

	"github.com/yamada/multi-fx/internal/pool"
)

// StateStore は SubPool・Fill の状態を永続化する抽象
// 本番: JSON実装（json.go）、テスト: インメモリ実装（memory.go）
type StateStore interface {
	// SubPool の永続化
	SaveSubPool(ctx context.Context, snap pool.SubPoolSnapshot) error
	LoadSubPool(ctx context.Context, id pool.SubPoolID) (pool.SubPoolSnapshot, error)

	// Fill 履歴の永続化
	SaveFill(ctx context.Context, fill pool.Fill) error
	ListFills(ctx context.Context, subPoolID pool.SubPoolID) ([]pool.Fill, error)
}
