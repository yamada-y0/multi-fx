package order

import (
	"context"

	"github.com/yamada/fxd/internal/broker"
	"github.com/yamada/fxd/internal/pool"
	pkgorder "github.com/yamada/fxd/pkg/order"
)

// ManagedOrder は Aggregator が管理するオーダー（SubPool と Broker のオーダーを紐付ける）
type ManagedOrder struct {
	BrokerOrderID broker.OrderID
	Req           pool.OrderRequest // 元の Agent からの依頼（Intent・ClosePositionID など内部概念を保持）
}

// Aggregator は Agent と Broker の間に立ち、
// オーダーの変換・中継・Fill の配送を担う
//
// 【責務】
//   - pool.OrderRequest を pkg/order.Order に変換して Broker へ中継
//   - BrokerOrderID ↔ SubPoolID の対応管理
//   - Broker からの pkg/order.Fill を pool.Fill に変換して SubPool へ配送
//
// 【初期スコープ外・将来拡張】
//   - ネッティング（SubPool 間の買い売りを相殺して Broker への発注量を削減）
type Aggregator interface {
	// SubmitOrder は Agent からの OrderRequest を pkg/order.Order に変換して Broker へ発注する
	SubmitOrder(ctx context.Context, req pool.OrderRequest) error

	// CancelOrder は SubPool からのキャンセル依頼を Broker へ中継する
	CancelOrder(ctx context.Context, subPoolID pool.SubPoolID, brokerOrderID broker.OrderID) error

	// SyncFills は Broker から約定済み Fill を取得し、pool.Fill に変換して該当 SubPool へ配送する
	// 1ティックごとに呼び出す
	SyncFills(ctx context.Context) error

	// ActiveOrders は現在 PENDING 状態のオーダー一覧を返す
	ActiveOrders() []ManagedOrder
}

// ToOrder は pool.OrderRequest を Broker に渡す pkg/order.Order に変換する
// mapper を使ってシステム内部のClosePositionIDをBroker側のIDに変換する
func ToOrder(req pool.OrderRequest, mapper PositionIDMapper) pkgorder.Order {
	o := pkgorder.Order{
		Pair:       req.Pair,
		Side:       req.Side,
		Lots:       req.Lots,
		OrderType:  req.OrderType,
		Intent:     pkgorder.OrderIntent(req.OrderIntent),
		StopLoss:   req.StopLoss,
		LimitPrice: req.LimitPrice,
	}
	if req.OrderIntent == pool.OrderIntentClose {
		brokerID, _ := mapper.ToBrokerID(req.ClosePositionID)
		o.ClosePositionID = brokerID
	}
	return o
}

// ToPoolFill は Broker からの pkg/order.Fill を pool.Fill に変換する
// managed は BrokerOrderID から引いた ManagedOrder（Intent・ClosePositionID を復元するため）
func ToPoolFill(f pkgorder.Fill, managed ManagedOrder) pool.Fill {
	return pool.Fill{
		BrokerOrderID:   f.OrderID,
		PositionID:      f.PositionID,
		SubPoolID:       managed.Req.SubPoolID,
		Pair:            f.Pair,
		Side:            f.Side,
		Lots:            f.Lots,
		FilledPrice:     f.FilledPrice,
		FilledAt:        f.FilledAt,
		Intent:          managed.Req.OrderIntent,
		ClosePositionID: managed.Req.ClosePositionID,
	}
}
