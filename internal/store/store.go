package store

import (
	"context"

	"github.com/shopspring/decimal"
	"github.com/yamada/multi-fx/internal/pool"
)

// StateStore は SubPool・Fill・MasterPool の状態を永続化する抽象
// 本番: DynamoDB 実装、バックテスト: インメモリ実装
type StateStore interface {
	// SubPool の永続化
	SaveSubPool(ctx context.Context, snap pool.SubPoolSnapshot) error
	LoadSubPool(ctx context.Context, id pool.SubPoolID) (pool.SubPoolSnapshot, error)

	// 初期ロード用: Active/Suspended のみ返す（フィルターはインフラ層の責務）
	ListActiveSubPools(ctx context.Context) ([]pool.SubPoolSnapshot, error)

	// Fill 履歴の永続化
	SaveFill(ctx context.Context, fill pool.Fill) error
	ListFills(ctx context.Context, subPoolID pool.SubPoolID) ([]pool.Fill, error)

	// MasterPool 残高の永続化
	SaveMasterBalance(ctx context.Context, balance decimal.Decimal) error
	LoadMasterBalance(ctx context.Context) (decimal.Decimal, error)
}
