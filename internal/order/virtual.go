package order

import (
	"github.com/yamada/multi-fx/internal/pool"
	"github.com/yamada/multi-fx/pkg/currency"
)

// VirtualFillEngine は各 SubPool への仮想約定価格を算出する
//
// TODO: 実装未定。以下の要素を考慮する予定:
//   - Broker の実約定価格をベースにする
//   - SubPool ごとのスプレッド・スリッページモデル（バックテスト精度向上）
//   - HistoricalBroker 使用時は Bid/Ask を使い分ける
type VirtualFillEngine interface {
	// ComputeFill は NetOrder の約定結果と各 SubPool の発注依頼から
	// SubPool ごとの仮想 Fill を算出する
	ComputeFill(
		netFill pool.Fill,
		sources []pool.OrderRequest,
		rates map[currency.Pair]currency.Rate,
	) []pool.Fill
}
