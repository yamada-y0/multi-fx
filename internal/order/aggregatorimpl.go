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
	broker          broker.Broker
	subPools        map[pool.SubPoolID]pool.SubPool
	orders          map[broker.OrderID]ManagedOrder
	mapper          PositionIDMapper
	store           store.StateStore
	lastFillEventID string
}

func NewAggregator(b broker.Broker, subPools map[pool.SubPoolID]pool.SubPool, mapper PositionIDMapper, st store.StateStore) Aggregator {
	lastID, _ := st.LoadLastFillEventID(context.Background())
	return &aggregator{
		broker:          b,
		subPools:        subPools,
		orders:          make(map[broker.OrderID]ManagedOrder),
		mapper:          mapper,
		store:           st,
		lastFillEventID: lastID,
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
	lastID, _ := st.LoadLastFillEventID(context.Background())
	return &aggregator{
		broker:          b,
		subPools:        subPools,
		orders:          orders,
		mapper:          mapper,
		store:           st,
		lastFillEventID: lastID,
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

// SyncFills は FetchFillEvents で新規イベントを取得し SubPool に通知する。
// lastFillEventID を sinceID として使い、処理後に更新・永続化する。
func (a *aggregator) SyncFills(ctx context.Context) error {
	events, err := a.broker.FetchFillEvents(ctx, a.lastFillEventID)
	if err != nil {
		return fmt.Errorf("aggregator: fetch fill events: %w", err)
	}

	for _, ev := range events {
		managed, ok := a.orders[broker.OrderID(ev.OrderID)]
		if !ok {
			// 自分が発注していないイベント（他プロセスなど）は無視
			a.lastFillEventID = ev.ID
			continue
		}

		sp, ok := a.subPools[managed.Req.SubPoolID]
		if !ok {
			delete(a.orders, broker.OrderID(ev.OrderID))
			a.lastFillEventID = ev.ID
			continue
		}

		if ev.Intent == pkgorder.OrderIntentOpen {
			a.mapper.Register(ev.PositionID, ev.PositionID)
		}

		poolFill := ToPoolFillFromEvent(ev, managed)
		sp.OnFill(poolFill)

		if err := a.store.SaveFill(ctx, poolFill); err != nil {
			return fmt.Errorf("aggregator: save fill: %w", err)
		}

		delete(a.orders, broker.OrderID(ev.OrderID))
		a.lastFillEventID = ev.ID
	}

	if len(events) > 0 {
		if err := a.store.SaveLastFillEventID(ctx, a.lastFillEventID); err != nil {
			return fmt.Errorf("aggregator: save last fill event id: %w", err)
		}
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
