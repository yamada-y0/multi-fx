package store

import (
	"context"

	"github.com/yamada/multi-fx/internal/pool"
)

// StateStore は SubPool と MasterPool の状態を永続化する抽象
// 本番: DynamoDB 実装、テスト: インメモリ実装
type StateStore interface {
	// SubPool の永続化
	SaveSubPool(ctx context.Context, snap pool.SubPoolSnapshot) error
	LoadSubPool(ctx context.Context, id pool.SubPoolID) (pool.SubPoolSnapshot, error)
	ListSubPools(ctx context.Context) ([]pool.SubPoolSnapshot, error)

	// MasterPool 残高の永続化
	SaveMasterBalance(ctx context.Context, balance float64) error
	LoadMasterBalance(ctx context.Context) (float64, error)
}
