package order

import (
	"context"
	"time"

	"github.com/yamada/multi-fx/internal/pool"
	"github.com/yamada/multi-fx/pkg/currency"
)

// NetOrder は Aggregator がネッティング後に Broker へ渡す集約注文
type NetOrder struct {
	Pair     currency.Pair
	Side     pool.Side
	NetLots  float64 // ネッティング後の絶対ロット数
	Sources  []pool.OrderRequest
	CreatedAt time.Time
}

// Aggregator は全 SubPool の OrderRequest を受け取り、
// 通貨ペア単位でネッティングして Broker へ発注する
//
// ネッティングの考え方:
//   SubPool A: USDJPY Long 1.0 lot
//   SubPool B: USDJPY Short 0.3 lot
//   → NetOrder: USDJPY Long 0.7 lot として Broker へ
//
// 約定後、各 SubPool へは仮想約定価格で Fill を配送する
type Aggregator interface {
	// Submit は SubPool からの発注依頼を受け付ける
	Submit(req pool.OrderRequest) error

	// Flush は1ティック末に呼び出す
	// ネッティング → Broker 発注 → Fill を各 SubPool へ配送
	Flush(ctx context.Context) error
}
