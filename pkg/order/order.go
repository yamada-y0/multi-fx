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

// AccountInfo は口座の残高・証拠金情報
type AccountInfo struct {
	Balance       decimal.Decimal // 口座残高
	UnrealizedPnL decimal.Decimal // 未実現損益
	MarginUsed    decimal.Decimal // 使用証拠金
	MarginAvail   decimal.Decimal // 利用可能証拠金
}


// CalendarEvent は経済指標カレンダーのイベント（ForexFactory）
type CalendarEvent struct {
	Title    string    // 指標名
	Country  string    // 通貨コード（"USD", "JPY" 等）
	Date     time.Time // 発表予定時刻（UTC）
	Impact   string    // "High", "Medium", "Low", "Holiday"
	Forecast string    // 予想値
	Previous string    // 前回値
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
