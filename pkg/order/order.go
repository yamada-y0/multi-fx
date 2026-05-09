package order

import (
	"time"

	"github.com/shopspring/decimal"
	"github.com/yamada/fxd/pkg/currency"
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

// PendingOrder はブローカーが保持する未約定注文
type PendingOrder struct {
	ID    string
	Order Order
}

// FillEvent はブローカーが検出した約定イベント
// Historical: pending→約定の変化から生成
// OANDA: GET /v1/accounts/:id/transactions から取得
type FillEvent struct {
	ID          string // ブローカー固有のイベントID（sinceID として使う）
	OrderID     string
	PositionID  string
	Intent      OrderIntent
	Pair        currency.Pair
	Side        Side
	Lots        decimal.Decimal
	FilledPrice decimal.Decimal
	FilledAt    time.Time
}

// Fill はブローカーからの約定通知
type Fill struct {
	OrderID     string
	PositionID  string // 新規建て時にBrokerが払い出すPositionID
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
