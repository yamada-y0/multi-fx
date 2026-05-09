package broker

import (
	"context"
	"time"

	"github.com/yamada/fxd/pkg/currency"
	"github.com/yamada/fxd/pkg/market"
	pkgorder "github.com/yamada/fxd/pkg/order"
)

// OrderID は Broker が発行するオーダー識別子
type OrderID string

// Broker は発注・オーダー管理・レート取得の抽象インターフェース
// pool など内部概念を知らず、pkg/order の汎用型のみを扱う
type Broker interface {
	// SubmitOrder はオーダーを受け付けて OrderID を返す
	SubmitOrder(ctx context.Context, order pkgorder.Order) (OrderID, error)

	// CancelOrder は PENDING のオーダーをキャンセルする
	CancelOrder(ctx context.Context, id OrderID) error

	// FetchPositions は現在保有中のポジション一覧を返す（冪等）
	FetchPositions(ctx context.Context) ([]pkgorder.Position, error)

	// FetchOrders は現在 PENDING 状態のオーダー一覧を返す（冪等）
	FetchOrders(ctx context.Context) ([]pkgorder.PendingOrder, error)

	// FetchFillEvents は sinceID より新しい約定イベントを古い順で返す（冪等）
	// sinceID="" のとき全件を返す。返された最後のイベントのIDを次回の sinceID として使う。
	FetchFillEvents(ctx context.Context, sinceID string) ([]pkgorder.FillEvent, error)

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

	// FetchCandles は現在ティックから遡って最大 n 本の足データを返す（新しい順）
	FetchCandles(pair currency.Pair, n int) ([]market.Candle, error)

	// Snapshot は現在の状態をスナップショットとして返す
	Snapshot() HistoricalBrokerSnapshot

	// Restore はスナップショットから状態を復元する（CSV rows は別途渡す）
	Restore(snap HistoricalBrokerSnapshot)
}

// PendingOrderSnapshot は永続化用の未約定注文
type PendingOrderSnapshot struct {
	ID    string        `json:"id"`
	Order pkgorder.Order `json:"order"`
}

// HistoricalBrokerSnapshot は HistoricalBroker の永続化可能な状態
type HistoricalBrokerSnapshot struct {
	Cursor          int                    `json:"cursor"`
	Pending         []PendingOrderSnapshot `json:"pending"`
	Positions       []pkgorder.Position    `json:"positions"`
	LastFillEventID string                 `json:"last_fill_event_id"`
}
