package broker

import (
	"context"
	"time"

	"github.com/yamada/multi-fx/pkg/currency"
	"github.com/yamada/multi-fx/pkg/market"
	pkgorder "github.com/yamada/multi-fx/pkg/order"
)

// OrderID は Broker が発行するオーダー識別子
type OrderID string

// Broker は発注・オーダー管理・レート取得の抽象インターフェース
// pool など内部概念を知らず、pkg/order の汎用型のみを扱う
type Broker interface {
	// SubmitOrder はオーダーを受け付けて OrderID を返す
	SubmitOrder(ctx context.Context, order pkgorder.Order) (OrderID, error)

	// FetchFills は前回呼び出し以降に約定した Fill を返す
	// 返した Fill は既読扱いになり次回以降は含まれない
	FetchFills(ctx context.Context) ([]pkgorder.Fill, error)

	// CancelOrder は PENDING のオーダーをキャンセルする
	CancelOrder(ctx context.Context, id OrderID) error

	// FetchPositions は現在保有中のポジション一覧を返す
	// FetchFills と異なり既読後クリアしない（決済されるまで保持）
	FetchPositions(ctx context.Context) ([]pkgorder.Position, error)

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
}
