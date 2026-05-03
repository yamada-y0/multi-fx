package pool_test

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/yamada/multi-fx/internal/pool"
)

func newTestMaster(total float64) pool.MasterPool {
	return pool.NewMasterPool(d(total))
}

func TestMasterPool_InitialFunds(t *testing.T) {
	m := newTestMaster(1000000)

	if !m.TotalFunds().Equal(d(1000000)) {
		t.Errorf("TotalFunds = %v, want 1000000", m.TotalFunds())
	}
	if !m.AllocatedFunds().Equal(decimal.Zero) {
		t.Errorf("AllocatedFunds = %v, want 0", m.AllocatedFunds())
	}
	if !m.FreeFunds().Equal(d(1000000)) {
		t.Errorf("FreeFunds = %v, want 1000000", m.FreeFunds())
	}
}

func TestMasterPool_CreateSubPool(t *testing.T) {
	m := newTestMaster(1000000)

	sp, err := m.CreateSubPool(d(100000), "test")
	if err != nil {
		t.Fatalf("CreateSubPool: %v", err)
	}
	if sp == nil {
		t.Fatal("SubPool is nil")
	}
	if !m.AllocatedFunds().Equal(d(100000)) {
		t.Errorf("AllocatedFunds = %v, want 100000", m.AllocatedFunds())
	}
	if !m.FreeFunds().Equal(d(900000)) {
		t.Errorf("FreeFunds = %v, want 900000", m.FreeFunds())
	}
}

func TestMasterPool_CreateSubPool_InsufficientFunds(t *testing.T) {
	m := newTestMaster(100000)

	_, err := m.CreateSubPool(d(200000), "test")
	if err == nil {
		t.Error("CreateSubPool with insufficient funds should return error")
	}
}

func TestMasterPool_CreateSubPool_ZeroFunds(t *testing.T) {
	m := newTestMaster(1000000)

	_, err := m.CreateSubPool(decimal.Zero, "test")
	if err == nil {
		t.Error("CreateSubPool with zero funds should return error")
	}
}

func TestMasterPool_CreateMultipleSubPools(t *testing.T) {
	m := newTestMaster(1000000)

	m.CreateSubPool(d(300000), "strategy-a")
	m.CreateSubPool(d(300000), "strategy-b")

	if !m.AllocatedFunds().Equal(d(600000)) {
		t.Errorf("AllocatedFunds = %v, want 600000", m.AllocatedFunds())
	}
	if !m.FreeFunds().Equal(d(400000)) {
		t.Errorf("FreeFunds = %v, want 400000", m.FreeFunds())
	}
	if len(m.AllSnapshots()) != 2 {
		t.Errorf("AllSnapshots = %d, want 2", len(m.AllSnapshots()))
	}
}

func TestMasterPool_ReceiveFunds(t *testing.T) {
	m := newTestMaster(1000000)

	sp, _ := m.CreateSubPool(d(100000), "test")
	sp.Suspend()
	returnAmount, _ := sp.Terminate()

	err := m.ReceiveFunds(sp.ID(), returnAmount)
	if err != nil {
		t.Fatalf("ReceiveFunds: %v", err)
	}

	if !m.FreeFunds().Equal(d(1000000)) {
		t.Errorf("FreeFunds after receive = %v, want 1000000", m.FreeFunds())
	}
	if len(m.AllSnapshots()) != 0 {
		t.Errorf("AllSnapshots after receive = %d, want 0", len(m.AllSnapshots()))
	}
}

func TestMasterPool_ReceiveFunds_NotTerminated(t *testing.T) {
	m := newTestMaster(1000000)

	sp, _ := m.CreateSubPool(d(100000), "test")

	err := m.ReceiveFunds(sp.ID(), d(100000))
	if err == nil {
		t.Error("ReceiveFunds from non-terminated subpool should return error")
	}
}

func TestMasterPool_GetSubPool(t *testing.T) {
	m := newTestMaster(1000000)

	sp, _ := m.CreateSubPool(d(100000), "test")

	found, ok := m.GetSubPool(sp.ID())
	if !ok {
		t.Fatal("GetSubPool should return true")
	}
	if found.ID() != sp.ID() {
		t.Errorf("GetSubPool ID = %v, want %v", found.ID(), sp.ID())
	}
}

func TestMasterPool_Restore(t *testing.T) {
	m := newTestMaster(1000000)
	sp, _ := m.CreateSubPool(d(100000), "test")
	snap := sp.Snapshot()

	restored := pool.RestoreMasterPool(d(1000000), []pool.SubPoolSnapshot{snap})

	if !restored.AllocatedFunds().Equal(d(100000)) {
		t.Errorf("AllocatedFunds after restore = %v, want 100000", restored.AllocatedFunds())
	}
	if len(restored.AllSnapshots()) != 1 {
		t.Errorf("AllSnapshots after restore = %d, want 1", len(restored.AllSnapshots()))
	}
}
