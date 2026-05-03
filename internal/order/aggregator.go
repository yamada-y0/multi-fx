package order

import (
	"context"

	"github.com/yamada/multi-fx/internal/broker"
	"github.com/yamada/multi-fx/internal/pool"
	"github.com/yamada/multi-fx/pkg/currency"
)

// ManagedOrder は Aggregator が管理するオーダー（SubPool と Broker のオーダーを紐付ける）
type ManagedOrder struct {
	BrokerOrderID broker.OrderID
	Req           pool.OrderRequest // 元の SubPool からの依頼
}

// PositionSummary は通貨ペア単位のポジション集計
type PositionSummary struct {
	Pair        currency.Pair
	NetLots     map[pool.SubPoolID][]pool.Position // SubPool ごとのポジション一覧
}

// Aggregator は SubPool と Broker の間に立ち、
// オーダーの中継・管理と Fill の配送を担う
//
// 【責務】
//   - SubPool からの OrderRequest を Broker へ中継
//   - SubPool ↔ BrokerOrderID の対応管理
//   - Broker からの Fill を該当 SubPool へ配送
//   - 通貨ペア単位のポジション集計
//
// 【初期スコープ外・将来拡張】
//   - ネッティング（SubPool 間の買い売りを相殺して Broker への発注量を削減）
type Aggregator interface {
	// SubmitOrder は SubPool からの OrderRequest を受け取り Broker へ発注する
	// BrokerOrderID と SubPool の対応を内部で保持する
	SubmitOrder(ctx context.Context, req pool.OrderRequest) error

	// CancelOrder は SubPool からのキャンセル依頼を Broker へ中継する
	CancelOrder(ctx context.Context, subPoolID pool.SubPoolID, brokerOrderID broker.OrderID) error

	// SyncFills は Broker から約定済み Fill を取得し、該当 SubPool へ配送する
	// 1ティックごとに呼び出す
	SyncFills(ctx context.Context) error

	// ActiveOrders は現在 PENDING 状態のオーダー一覧を返す
	ActiveOrders() []ManagedOrder
}
