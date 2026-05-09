package order

import (
	"context"
	"fmt"

	"github.com/yamada/fxd/internal/broker"
	"github.com/yamada/fxd/internal/pool"
	"github.com/yamada/fxd/internal/store"
	pkgorder "github.com/yamada/fxd/pkg/order"
)

type aggregator struct {
	broker   broker.Broker
	subPools map[pool.SubPoolID]pool.SubPool
	orders   map[broker.OrderID]ManagedOrder
	mapper   PositionIDMapper
	store    store.StateStore
}

func NewAggregator(b broker.Broker, subPools map[pool.SubPoolID]pool.SubPool, mapper PositionIDMapper, st store.StateStore) Aggregator {
	return &aggregator{
		broker:   b,
		subPools: subPools,
		orders:   make(map[broker.OrderID]ManagedOrder),
		mapper:   mapper,
		store:    st,
	}
}

// RestoreAggregator は各SubPoolのPendingOrdersを正としてAggregatorを復元する
func RestoreAggregator(b broker.Broker, subPools map[pool.SubPoolID]pool.SubPool, mapper PositionIDMapper, st store.StateStore) Aggregator {
	orders := make(map[broker.OrderID]ManagedOrder)
	for _, sp := range subPools {
		for _, po := range sp.Snapshot().PendingOrders {
			id := broker.OrderID(po.BrokerOrderID)
			orders[id] = ManagedOrder{BrokerOrderID: id, Req: po.Req}
		}
	}
	return &aggregator{
		broker:   b,
		subPools: subPools,
		orders:   orders,
		mapper:   mapper,
		store:    st,
	}
}

func (a *aggregator) SubmitOrder(ctx context.Context, req pool.OrderRequest) error {
	o := ToOrder(req, a.mapper)
	id, err := a.broker.SubmitOrder(ctx, o)
	if err != nil {
		return fmt.Errorf("aggregator: submit order: %w", err)
	}
	managed := ManagedOrder{BrokerOrderID: id, Req: req}
	a.orders[id] = managed

	if sp, ok := a.subPools[req.SubPoolID]; ok {
		sp.AddPendingOrder(pool.PendingOrder{
			BrokerOrderID: string(id),
			Req:           req,
		})
	}
	return nil
}

func (a *aggregator) CancelOrder(ctx context.Context, subPoolID pool.SubPoolID, brokerOrderID broker.OrderID) error {
	managed, ok := a.orders[brokerOrderID]
	if !ok {
		return fmt.Errorf("aggregator: order not found: %s", brokerOrderID)
	}
	if managed.Req.SubPoolID != subPoolID {
		return fmt.Errorf("aggregator: order %s does not belong to subpool %s", brokerOrderID, subPoolID)
	}
	if err := a.broker.CancelOrder(ctx, brokerOrderID); err != nil {
		return fmt.Errorf("aggregator: cancel order: %w", err)
	}
	delete(a.orders, brokerOrderID)

	if sp, ok := a.subPools[subPoolID]; ok {
		sp.RemovePendingOrder(string(brokerOrderID))
	}
	return nil
}

// SyncFills はブローカーの現在状態と Aggregator/SubPool の既知状態を比較し、
// 差分から約定を導出して SubPool に通知する。
//
// 約定検出のロジック:
//   - a.orders にあってブローカーの pending 一覧にない → 約定済み
//   - Open 約定: ブローカーの positions にあって SubPool にないポジション → PositionID を特定
//   - Close 約定: SubPool にあってブローカーの positions にないポジション → 決済済み
func (a *aggregator) SyncFills(ctx context.Context) error {
	brokerOrders, err := a.broker.FetchOrders(ctx)
	if err != nil {
		return fmt.Errorf("aggregator: fetch orders: %w", err)
	}
	brokerPositions, err := a.broker.FetchPositions(ctx)
	if err != nil {
		return fmt.Errorf("aggregator: fetch positions: %w", err)
	}

	// ブローカー側でまだ pending なオーダーのセット
	stillPending := make(map[broker.OrderID]struct{}, len(brokerOrders))
	for _, o := range brokerOrders {
		stillPending[broker.OrderID(o.ID)] = struct{}{}
	}

	// ブローカー側のポジションセット
	brokerPosSet := make(map[string]pkgorder.Position, len(brokerPositions))
	for _, p := range brokerPositions {
		brokerPosSet[p.ID] = p
	}

	// a.orders にあってブローカーの pending にない → 約定済み
	for id, managed := range a.orders {
		if _, ok := stillPending[id]; ok {
			continue
		}

		sp, ok := a.subPools[managed.Req.SubPoolID]
		if !ok {
			delete(a.orders, id)
			continue
		}

		var fill pkgorder.Fill
		fill.OrderID = string(id)
		fill.Pair = managed.Req.Pair
		fill.Side = managed.Req.Side
		fill.Lots = managed.Req.Lots

		switch managed.Req.OrderIntent {
		case pool.OrderIntentOpen:
			// SubPool にないポジションがブローカー側にある → それが新規約定ポジション
			posID := a.findNewPosition(sp, brokerPosSet)
			if posID == "" {
				// まだポジションが反映されていない（次ティックに持ち越し）
				continue
			}
			bp := brokerPosSet[posID]
			fill.PositionID = posID
			fill.FilledPrice = bp.OpenPrice
			fill.FilledAt = bp.OpenedAt
			a.mapper.Register(posID, posID)

		case pool.OrderIntentClose:
			// SubPool にあってブローカーにないポジション → 決済済み
			posID := managed.Req.ClosePositionID
			if _, stillOpen := brokerPosSet[posID]; stillOpen {
				// まだ決済されていない
				continue
			}
			fill.PositionID = posID
			fill.FilledPrice = managed.Req.StopLoss // 近似値。正確な約定価格はブローカーAPIから取得が必要
			fill.FilledAt = managed.Req.RequestedAt
		}

		poolFill := ToPoolFill(fill, managed)
		sp.OnFill(poolFill)

		if err := a.store.SaveFill(ctx, poolFill); err != nil {
			return fmt.Errorf("aggregator: save fill: %w", err)
		}

		delete(a.orders, id)
	}
	return nil
}

// findNewPosition は SubPool にないポジションIDをブローカー側から探す
func (a *aggregator) findNewPosition(sp pool.SubPool, brokerPosSet map[string]pkgorder.Position) string {
	spPosSet := make(map[string]struct{})
	for _, p := range sp.Snapshot().Positions {
		spPosSet[p.ID] = struct{}{}
	}
	for id := range brokerPosSet {
		if _, known := spPosSet[id]; !known {
			return id
		}
	}
	return ""
}

func (a *aggregator) ActiveOrders() []ManagedOrder {
	result := make([]ManagedOrder, 0, len(a.orders))
	for _, o := range a.orders {
		result = append(result, o)
	}
	return result
}
