package pool

import (
	"time"

	"github.com/shopspring/decimal"
	"github.com/yamada/multi-fx/pkg/currency"
)

// SubPoolID はシステム全体でユニークな SubPool 識別子
type SubPoolID string

// Side はポジションの売買方向
type Side int

const (
	Long  Side = 1
	Short Side = -1
)

// Position は SubPool が保有する1ポジション
type Position struct {
	ID        string
	Pair      currency.Pair
	Side      Side
	Lots      decimal.Decimal
	OpenPrice decimal.Decimal
	OpenedAt  time.Time
}

// OrderRequest は SubPool から Order Aggregator への発注依頼
// （order パッケージとの循環参照を避けるため pool パッケージに定義）
type OrderRequest struct {
	SubPoolID   SubPoolID
	Pair        currency.Pair
	Side        Side
	Lots        decimal.Decimal
	StopLoss    decimal.Decimal // 必須。逆指値なしの発注は打たない設計。
	RequestedAt time.Time
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
type SubPool interface {
	ID() SubPoolID
	Snapshot() SubPoolSnapshot

	// RequestOrder は発注依頼を Order Aggregator へ送る
	// State が Active でない場合はエラーを返す
	RequestOrder(req OrderRequest) error

	// Suspend は Active → Suspended へ遷移する（新規発注不可）
	Suspend() error

	// Terminate は Suspended → Terminated へ遷移し、返還額を返す
	Terminate() (returnAmount decimal.Decimal, err error)

	// OnRate はレート更新通知を受け取り、含み損益を再計算する
	OnRate(r currency.Rate)

	// OnFill は約定通知を受け取り、ポジションと残高を更新する
	OnFill(fill Fill)
}

// Fill は約定結果（Order Aggregator から SubPool へ通知）
type Fill struct {
	RequestID   string
	Pair        currency.Pair
	Side        Side
	Lots        decimal.Decimal
	FilledPrice decimal.Decimal
	FilledAt    time.Time
}
