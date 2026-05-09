package agent

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
	"github.com/yamada/fxd/pkg/currency"
)

// WakeupStore はウェイクアップ条件を永続化する
type WakeupStore interface {
	Save(ctx context.Context, cond WakeupCondition) error
	Load(ctx context.Context) (WakeupCondition, bool, error)
	Delete(ctx context.Context) error
}

// WakeupCondition は Agent が「次に起こしてほしい条件」を宣言する型。
// 複数フィールドを指定した場合はいずれかを満たしたとき（OR）に起動する。
// nil フィールドは評価されない。
type WakeupCondition struct {
	After    *time.Time
	PriceGTE map[currency.Pair]decimal.Decimal
	PriceLTE map[currency.Pair]decimal.Decimal
	AnyFill  bool
}

// IsMet は現在の時刻・レートに対してウェイクアップ条件を満たすかを返す
func (w WakeupCondition) IsMet(now time.Time, rates map[currency.Pair]decimal.Decimal, filled bool) bool {
	if w.After != nil && !now.Before(*w.After) {
		return true
	}
	for pair, threshold := range w.PriceGTE {
		if rate, ok := rates[pair]; ok && rate.GreaterThanOrEqual(threshold) {
			return true
		}
	}
	for pair, threshold := range w.PriceLTE {
		if rate, ok := rates[pair]; ok && rate.LessThanOrEqual(threshold) {
			return true
		}
	}
	if w.AnyFill && filled {
		return true
	}
	return false
}
