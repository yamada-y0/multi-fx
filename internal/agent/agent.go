package agent

import (
	"context"

	"github.com/yamada/multi-fx/internal/pool"
	"github.com/yamada/multi-fx/pkg/market"
)

// Strategy は発注判断ロジックのインターフェース
// 副作用を持たず、OrderRequest を返すだけ
type Strategy interface {
	// Name は戦略識別子（ログ・メトリクス用）
	Name() string

	// OnTick は1ティックごとに呼ばれ、発注依頼を返す
	// SubPool の状態は Snapshot（値）として受け取る
	OnTick(ctx context.Context, snap pool.SubPoolSnapshot, mkt market.MarketContext) ([]pool.OrderRequest, error)

	// OnInstruction は Commander からの指示テキストを受け取り、戦略パラメータを調整する
	// LLM の非決定性はここで吸収する
	OnInstruction(ctx context.Context, instruction string) error
}

// StrategyFactory は設定マップから Strategy インスタンスを生成するファクトリ関数
type StrategyFactory func(cfg map[string]any) (Strategy, error)
