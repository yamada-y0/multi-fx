package broker

import (
	"context"
	"time"

	"github.com/yamada/multi-fx/internal/order"
	"github.com/yamada/multi-fx/internal/pool"
	"github.com/yamada/multi-fx/pkg/currency"
)

// Broker は発注・レート取得の抽象インターフェース
// HistoricalDataBroker と RealApiBroker を同一インターフェースで扱う
type Broker interface {
	// PlaceOrder は集約注文を送信して約定結果を返す
	PlaceOrder(ctx context.Context, o order.NetOrder) (pool.Fill, error)

	// FetchRate は指定通貨ペアの現在レートを返す
	FetchRate(ctx context.Context, pair currency.Pair) (currency.Rate, error)

	// Name はブローカー識別子（ログ・メトリクス用）
	Name() string
}

// HistoricalBroker は過去データを再生するバックテスト用 Broker
type HistoricalBroker interface {
	Broker

	// Advance は次のティックへ進める。データ終端に達したら false を返す。
	Advance() bool

	// CurrentTime は現在再生中の時刻を返す
	CurrentTime() time.Time
}
