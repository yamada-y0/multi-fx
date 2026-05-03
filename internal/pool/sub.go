package pool

import (
	"time"

	"github.com/shopspring/decimal"
	"github.com/yamada/multi-fx/pkg/currency"
	pkgorder "github.com/yamada/multi-fx/pkg/order"
)

// SubPoolID はシステム全体でユニークな SubPool 識別子
type SubPoolID string

// Side, OrderType, OrderIntent は pkg/order の型を再エクスポートせず、
// pool 固有の概念（OrderIntent など）のみここで定義する

// OrderIntent は新規建てか決済かを表す（Aggregator が管理する内部概念）
type OrderIntent int

const (
	OrderIntentOpen  OrderIntent = iota // 新規建て
	OrderIntentClose                    // 決済（ClosePositionID で指定したポジションを閉じる）
)

// OrderRequest は Agent から Aggregator への発注依頼
// Aggregator が pkg/order.Order に変換して Broker へ渡す
type OrderRequest struct {
	SubPoolID       SubPoolID
	Pair            currency.Pair
	Side            pkgorder.Side
	Lots            decimal.Decimal
	OrderType       pkgorder.OrderType
	OrderIntent     OrderIntent
	StopLoss        decimal.Decimal // 必須。逆指値なしの発注は打たない設計。
	LimitPrice      decimal.Decimal // OrderTypeLimit のときのみ有効
	ClosePositionID string          // OrderIntentClose のときのみ有効
	RequestedAt     time.Time
}

// Fill は Aggregator から SubPool への約定通知（内部概念を含む）
type Fill struct {
	BrokerOrderID   string
	PositionID      string // 新規建て時にBrokerが払い出したPositionID
	SubPoolID       SubPoolID
	Pair            currency.Pair
	Side            pkgorder.Side
	Lots            decimal.Decimal
	FilledPrice     decimal.Decimal
	FilledAt        time.Time
	Intent          OrderIntent
	ClosePositionID string // Intent==Close のときのみ有効
}

// Position は SubPool が保有する1ポジション（pkg/order.Position を内包）
type Position struct {
	pkgorder.Position
	SubPoolID SubPoolID // どの SubPool のポジションか
}

// PendingOrder は SubPool が把握している未約定注文
type PendingOrder struct {
	BrokerOrderID string
	Req           OrderRequest
}

// SubPoolSnapshot は永続化・Commander への報告に使う値オブジェクト
type SubPoolSnapshot struct {
	ID             SubPoolID
	State          LifecycleState
	InitialBalance decimal.Decimal // フロアルール判定基準（生成時に確定、以後不変）
	CurrentBalance decimal.Decimal
	UnrealizedPnL  decimal.Decimal
	RealizedPnL    decimal.Decimal
	Positions      []Position
	PendingOrders  []PendingOrder
	StrategyName   string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// EquityBalance は含み損益を含む実質残高を返す
func (s SubPoolSnapshot) EquityBalance() decimal.Decimal {
	return s.CurrentBalance.Add(s.UnrealizedPnL)
}

// IsFloorRuleBreached はフロアルール（実質残高が初期割り当て未満）に抵触しているかを返す
func (s SubPoolSnapshot) IsFloorRuleBreached() bool {
	return s.EquityBalance().LessThan(s.InitialBalance)
}

// SubPool は仮想口座のインターフェース
// 発注判断は Agent が担うため、SubPool は口座状態の管理のみを責務とする
type SubPool interface {
	ID() SubPoolID
	Snapshot() SubPoolSnapshot

	// Suspend は Active → Suspended へ遷移する（新規発注不可）
	Suspend() error

	// Terminate は Suspended → Terminated へ遷移し、返還額を返す
	Terminate() (returnAmount decimal.Decimal, err error)

	// OnRate はレート更新通知を受け取り、含み損益を再計算する
	OnRate(r currency.Rate)

	// OnFill は約定通知を受け取り、ポジションと残高を更新する。未約定注文も削除する。
	OnFill(fill Fill)

	// AddPendingOrder は未約定注文を登録する（Aggregator が SubmitOrder 後に呼ぶ）
	AddPendingOrder(order PendingOrder)

	// RemovePendingOrder は未約定注文を削除する（Aggregator が CancelOrder 後に呼ぶ）
	RemovePendingOrder(brokerOrderID string)
}
