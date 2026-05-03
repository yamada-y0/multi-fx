package pool

import (
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	"github.com/yamada/multi-fx/pkg/currency"
	pkgorder "github.com/yamada/multi-fx/pkg/order"
)

type subPool struct {
	id             SubPoolID
	state          LifecycleState
	initialBalance decimal.Decimal
	currentBalance decimal.Decimal
	unrealizedPnL  decimal.Decimal
	realizedPnL    decimal.Decimal
	positions      map[string]Position     // key: PositionID
	pendingOrders  map[string]PendingOrder // key: BrokerOrderID
	strategyName   string
	createdAt      time.Time
	updatedAt      time.Time
}

func NewSubPool(id SubPoolID, initialBalance decimal.Decimal, strategyName string, now time.Time) SubPool {
	return &subPool{
		id:             id,
		state:          StateActive,
		initialBalance: initialBalance,
		currentBalance: initialBalance,
		positions:      make(map[string]Position),
		pendingOrders:  make(map[string]PendingOrder),
		strategyName:   strategyName,
		createdAt:      now,
		updatedAt:      now,
	}
}

func (s *subPool) ID() SubPoolID { return s.id }

func (s *subPool) Snapshot() SubPoolSnapshot {
	positions := make([]Position, 0, len(s.positions))
	for _, p := range s.positions {
		positions = append(positions, p)
	}
	pendingOrders := make([]PendingOrder, 0, len(s.pendingOrders))
	for _, o := range s.pendingOrders {
		pendingOrders = append(pendingOrders, o)
	}
	return SubPoolSnapshot{
		ID:             s.id,
		State:          s.state,
		InitialBalance: s.initialBalance,
		CurrentBalance: s.currentBalance,
		UnrealizedPnL:  s.unrealizedPnL,
		RealizedPnL:    s.realizedPnL,
		Positions:      positions,
		PendingOrders:  pendingOrders,
		StrategyName:   s.strategyName,
		CreatedAt:      s.createdAt,
		UpdatedAt:      s.updatedAt,
	}
}

func (s *subPool) AddPendingOrder(order PendingOrder) {
	s.pendingOrders[order.BrokerOrderID] = order
	s.updatedAt = time.Now()
}

func (s *subPool) RemovePendingOrder(brokerOrderID string) {
	delete(s.pendingOrders, brokerOrderID)
	s.updatedAt = time.Now()
}

func (s *subPool) Suspend() error {
	if err := ValidateTransition(s.state, StateSuspended); err != nil {
		return fmt.Errorf("subpool %s: %w", s.id, err)
	}
	s.state = StateSuspended
	s.updatedAt = time.Now()
	return nil
}

func (s *subPool) Terminate() (decimal.Decimal, error) {
	if err := ValidateTransition(s.state, StateTerminated); err != nil {
		return decimal.Zero, fmt.Errorf("subpool %s: %w", s.id, err)
	}
	if len(s.positions) > 0 {
		return decimal.Zero, fmt.Errorf("subpool %s: %d position(s) not closed", s.id, len(s.positions))
	}
	s.state = StateTerminated
	s.updatedAt = time.Now()
	returnAmount := s.currentBalance
	s.currentBalance = decimal.Zero
	return returnAmount, nil
}

func (s *subPool) OnRate(r currency.Rate) {
	total := decimal.Zero
	for _, p := range s.positions {
		if p.Pair != r.Pair {
			continue
		}
		diff := r.Mid().Sub(p.OpenPrice)
		if p.Side == pkgorder.Short {
			diff = diff.Neg()
		}
		total = total.Add(diff.Mul(p.Lots))
	}
	s.unrealizedPnL = total
	s.updatedAt = time.Now()
}

func (s *subPool) OnFill(fill Fill) {
	delete(s.pendingOrders, fill.BrokerOrderID)

	switch fill.Intent {
	case OrderIntentOpen:
		pos := Position{
			Position: pkgorder.Position{
				ID:        fill.PositionID,
				Pair:      fill.Pair,
				Side:      fill.Side,
				Lots:      fill.Lots,
				OpenPrice: fill.FilledPrice,
				OpenedAt:  fill.FilledAt,
			},
			SubPoolID: s.id,
		}
		s.positions[pos.ID] = pos

	case OrderIntentClose:
		pos, ok := s.positions[fill.ClosePositionID]
		if !ok {
			return
		}
		diff := fill.FilledPrice.Sub(pos.OpenPrice)
		if pos.Side == pkgorder.Short {
			diff = diff.Neg()
		}
		pnl := diff.Mul(pos.Lots)
		s.realizedPnL = s.realizedPnL.Add(pnl)
		s.currentBalance = s.currentBalance.Add(pnl)
		delete(s.positions, fill.ClosePositionID)
	}

	s.updatedAt = time.Now()
}
