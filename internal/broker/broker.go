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

// TradingBroker は発注・ポジション管理・約定履歴取得の抽象インターフェース
type TradingBroker interface {
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

	// FetchAccount は口座残高・証拠金情報を返す
	FetchAccount(ctx context.Context) (pkgorder.AccountInfo, error)

	// Name はブローカー識別子（ログ・メトリクス用）
	Name() string
}

// MarketBroker はレート・ローソク足・経済指標カレンダー取得の抽象インターフェース
type MarketBroker interface {
	// FetchRate は現在のレートを返す
	FetchRate(ctx context.Context, pair currency.Pair) (currency.Rate, error)

	// FetchCandles は指定ペア・granularity で直近 count 本のローソク足を新しい順で返す
	// granularity は "M1"/"M5"/"H1" 等（OANDA形式）
	FetchCandles(ctx context.Context, pair currency.Pair, granularity string, count int) ([]market.Candle, error)

	// FetchCalendar は今週の経済指標カレンダーを返す（ForexFactory）
	// currencies で対象通貨を絞る（例: ["USD","JPY"]）。空のとき全通貨を返す。
	FetchCalendar(ctx context.Context, currencies []string) ([]pkgorder.CalendarEvent, error)

	// Name はブローカー識別子（ログ・メトリクス用）
	Name() string
}

// Broker は TradingBroker と MarketBroker を統合したインターフェース（historical など単一実装向け）
type Broker interface {
	TradingBroker
	MarketBroker
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
