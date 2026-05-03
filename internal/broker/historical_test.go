package broker_test

import (
	"context"
	"testing"

	"github.com/yamada/multi-fx/internal/broker"
	"github.com/yamada/multi-fx/pkg/currency"
)

const testCSV = "../../testdata/usdjpy-h1-bid-2024-01-01-2024-01-07.csv"

func TestHistoricalBroker_Advance(t *testing.T) {
	b, err := broker.NewHistoricalBroker(currency.USDJPY, testCSV)
	if err != nil {
		t.Fatalf("NewHistoricalBroker: %v", err)
	}

	first := b.CurrentTime()

	if !b.Advance() {
		t.Fatal("Advance() should return true on first call")
	}
	if !b.CurrentTime().After(first) {
		t.Errorf("CurrentTime should advance: got %v, want after %v", b.CurrentTime(), first)
	}
}

func TestHistoricalBroker_FetchRate(t *testing.T) {
	b, err := broker.NewHistoricalBroker(currency.USDJPY, testCSV)
	if err != nil {
		t.Fatalf("NewHistoricalBroker: %v", err)
	}

	rate, err := b.FetchRate(context.Background(), currency.USDJPY)
	if err != nil {
		t.Fatalf("FetchRate: %v", err)
	}
	if rate.Pair != currency.USDJPY {
		t.Errorf("pair = %v, want %v", rate.Pair, currency.USDJPY)
	}
	if rate.Bid.IsZero() {
		t.Error("Bid should not be zero")
	}
	// Dukascopy は bid のみなので Bid=Ask
	if !rate.Bid.Equal(rate.Ask) {
		t.Errorf("Bid(%v) != Ask(%v)", rate.Bid, rate.Ask)
	}
}

func TestHistoricalBroker_AdvanceToEnd(t *testing.T) {
	b, err := broker.NewHistoricalBroker(currency.USDJPY, testCSV)
	if err != nil {
		t.Fatalf("NewHistoricalBroker: %v", err)
	}

	count := 1
	for b.Advance() {
		count++
	}
	if count == 0 {
		t.Error("should have at least one row")
	}
	// 終端で再度 Advance しても false
	if b.Advance() {
		t.Error("Advance() after end should return false")
	}
}

func TestHistoricalBroker_UnsupportedPair(t *testing.T) {
	b, err := broker.NewHistoricalBroker(currency.USDJPY, testCSV)
	if err != nil {
		t.Fatalf("NewHistoricalBroker: %v", err)
	}

	_, err = b.FetchRate(context.Background(), currency.EURUSD)
	if err == nil {
		t.Error("FetchRate with unsupported pair should return error")
	}
}
