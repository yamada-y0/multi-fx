package agent

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
	"github.com/yamada/multi-fx/internal/pool"
	"github.com/yamada/multi-fx/pkg/currency"
)

// WakeupStore は SubPool ごとのウェイクアップ条件を永続化する
type WakeupStore interface {
	Save(ctx context.Context, id pool.SubPoolID, cond WakeupCondition) error
	Load(ctx context.Context, id pool.SubPoolID) (WakeupCondition, bool, error)
	Delete(ctx context.Context, id pool.SubPoolID) error
}

// WakeupCondition は Agent が「次に起こしてほしい条件」を宣言する型。
// 複数フィールドを指定した場合はいずれかを満たしたとき（OR）に起動する。
// nil フィールドは評価されない。
type WakeupCondition struct {
	// After はこの時刻以降になったら起動する
	After *time.Time

	// PriceGTE は指定ペアのレートが価格以上になったら起動する
	PriceGTE map[currency.Pair]decimal.Decimal

	// PriceLTE は指定ペアのレートが価格以下になったら起動する
	PriceLTE map[currency.Pair]decimal.Decimal

	// AnyFill は未約定注文がこのティックで1件でも約定したら起動する
	AnyFill bool
}

// IsMet は現在の時刻・レートに対してウェイクアップ条件を満たすかを返す
// filled は今ティックで約定が発生したかどうか
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
