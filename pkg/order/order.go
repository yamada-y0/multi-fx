package order

import (
	"time"

	"github.com/shopspring/decimal"
	"github.com/yamada/multi-fx/pkg/currency"
)

// Side は売買方向
type Side int

const (
	Long  Side = 1
	Short Side = -1
)

// OrderType は注文の執行種別
type OrderType int

const (
	OrderTypeMarket OrderType = iota // 成行
	OrderTypeLimit                   // 指値
	OrderTypeStop                    // 逆指値
)

// OrderIntent は新規建てか決済かを表す
type OrderIntent int

const (
	OrderIntentOpen  OrderIntent = iota // 新規建て
	OrderIntentClose                    // 決済
)

// Order はブローカーへの発注パラメータ
type Order struct {
	Pair            currency.Pair
	Side            Side
	Lots            decimal.Decimal
	OrderType       OrderType
	Intent          OrderIntent
	StopLoss        decimal.Decimal // 逆指値価格
	LimitPrice      decimal.Decimal // 指値価格（OrderTypeLimit のときのみ有効）
	ClosePositionID string          // Intent==Close のとき、Broker側のPositionID
}

// Fill はブローカーからの約定通知
type Fill struct {
	OrderID     string
	Pair        currency.Pair
	Side        Side
	Lots        decimal.Decimal
	FilledPrice decimal.Decimal
	FilledAt    time.Time
}

// Position はブローカーが保持するポジション
type Position struct {
	ID        string
	Pair      currency.Pair
	Side      Side
	Lots      decimal.Decimal
	OpenPrice decimal.Decimal
	OpenedAt  time.Time
}
