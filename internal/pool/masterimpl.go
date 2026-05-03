package pool

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type masterPool struct {
	totalFunds decimal.Decimal
	subPools   map[SubPoolID]SubPool
}

func NewMasterPool(totalFunds decimal.Decimal) MasterPool {
	return &masterPool{
		totalFunds: totalFunds,
		subPools:   make(map[SubPoolID]SubPool),
	}
}

// RestoreMasterPool は永続化された情報からMasterPoolを復元する
func RestoreMasterPool(totalFunds decimal.Decimal, snaps []SubPoolSnapshot) MasterPool {
	subPools := make(map[SubPoolID]SubPool, len(snaps))
	for _, snap := range snaps {
		subPools[snap.ID] = RestoreSubPool(snap)
	}
	return &masterPool{
		totalFunds: totalFunds,
		subPools:   subPools,
	}
}

func (m *masterPool) CreateSubPool(initialFunds decimal.Decimal, strategyName string) (SubPool, error) {
	if initialFunds.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("masterpool: initialFunds must be positive")
	}
	if initialFunds.GreaterThan(m.FreeFunds()) {
		return nil, fmt.Errorf("masterpool: insufficient free funds: need %s, have %s", initialFunds, m.FreeFunds())
	}

	id := SubPoolID(uuid.New().String())
	sp := NewSubPool(id, initialFunds, strategyName, time.Now())
	m.subPools[id] = sp
	return sp, nil
}

func (m *masterPool) ReceiveFunds(from SubPoolID, amount decimal.Decimal) error {
	sp, ok := m.subPools[from]
	if !ok {
		return fmt.Errorf("masterpool: subpool not found: %s", from)
	}
	if sp.Snapshot().State != StateTerminated {
		return fmt.Errorf("masterpool: subpool %s is not terminated", from)
	}
	delete(m.subPools, from)
	return nil
}

func (m *masterPool) TotalFunds() decimal.Decimal {
	return m.totalFunds
}

func (m *masterPool) AllocatedFunds() decimal.Decimal {
	total := decimal.Zero
	for _, sp := range m.subPools {
		snap := sp.Snapshot()
		total = total.Add(snap.CurrentBalance).Add(snap.UnrealizedPnL)
	}
	return total
}

func (m *masterPool) FreeFunds() decimal.Decimal {
	return m.totalFunds.Sub(m.AllocatedFunds())
}

func (m *masterPool) AllSnapshots() []SubPoolSnapshot {
	result := make([]SubPoolSnapshot, 0, len(m.subPools))
	for _, sp := range m.subPools {
		result = append(result, sp.Snapshot())
	}
	return result
}

func (m *masterPool) GetSubPool(id SubPoolID) (SubPool, bool) {
	sp, ok := m.subPools[id]
	return sp, ok
}
