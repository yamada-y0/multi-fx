package broker

import (
	"context"
	"time"

	"github.com/yamada/multi-fx/internal/pool"
	"github.com/yamada/multi-fx/pkg/currency"
)

// OrderID は Broker が発行するオーダー識別子
type OrderID string

// OrderStatus はオーダーの状態
type OrderStatus int

const (
	OrderPending   OrderStatus = iota // 発注済み・未約定
	OrderFilled                       // 約定済み
	OrderCancelled                    // キャンセル済み
)

// BrokerOrder は Broker が保持するオーダー
type BrokerOrder struct {
	ID        OrderID
	Req       pool.OrderRequest
	Status    OrderStatus
	CreatedAt time.Time
}

// Broker は発注・オーダー管理・レート取得の抽象インターフェース
// オーダーの保持と約定判定は Broker の責務
type Broker interface {
	// SubmitOrder はオーダーを受け付けて OrderID を返す
	// オーダーは Broker 内で PENDING として保持される
	SubmitOrder(ctx context.Context, req pool.OrderRequest) (OrderID, error)

	// FetchFills は前回呼び出し以降に約定したFillを返す
	// 返したFillは既読扱いになり次回以降は含まれない
	FetchFills(ctx context.Context) ([]pool.Fill, error)

	// CancelOrder は PENDING のオーダーをキャンセルする
	CancelOrder(ctx context.Context, id OrderID) error

	// FetchPositions は現在保有中のポジション一覧を返す
	// FetchFills と異なり既読後クリアしない（決済されるまで保持）
	// 本番では証券会社 API を叩いて返す。HistoricalBroker では内部スライスを返す。
	FetchPositions(ctx context.Context) ([]pool.Position, error)

	// FetchRate は現在のレートを返す
	FetchRate(ctx context.Context, pair currency.Pair) (currency.Rate, error)

	// Name はブローカー識別子（ログ・メトリクス用）
	Name() string
}

// HistoricalBroker は過去データを再生するバックテスト用 Broker
type HistoricalBroker interface {
	Broker

	// Advance は次のティックへ進める
	// ティック進行時に PENDING オーダーの約定判定を行う
	// データ終端に達したら false を返す
	Advance() bool

	// CurrentTime は現在再生中の時刻を返す
	CurrentTime() time.Time
}
