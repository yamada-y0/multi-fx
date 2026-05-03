package order

import (
	"context"
	"fmt"

	"github.com/yamada/multi-fx/internal/broker"
	"github.com/yamada/multi-fx/internal/pool"
	"github.com/yamada/multi-fx/internal/store"
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

func (a *aggregator) SyncFills(ctx context.Context) error {
	fills, err := a.broker.FetchFills(ctx)
	if err != nil {
		return fmt.Errorf("aggregator: fetch fills: %w", err)
	}

	for _, f := range fills {
		id := broker.OrderID(f.OrderID)
		managed, ok := a.orders[id]
		if !ok {
			continue
		}

		sp, ok := a.subPools[managed.Req.SubPoolID]
		if !ok {
			continue
		}

		poolFill := ToPoolFill(f, managed)
		sp.OnFill(poolFill)

		if err := a.store.SaveFill(ctx, poolFill); err != nil {
			return fmt.Errorf("aggregator: save fill: %w", err)
		}

		if managed.Req.OrderIntent == pool.OrderIntentOpen {
			a.mapper.Register(f.PositionID, f.PositionID)
		}

		delete(a.orders, id)
	}
	return nil
}

func (a *aggregator) ActiveOrders() []ManagedOrder {
	result := make([]ManagedOrder, 0, len(a.orders))
	for _, o := range a.orders {
		result = append(result, o)
	}
	return result
}
