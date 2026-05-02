package agent

import (
	"context"

	"github.com/yamada/multi-fx/internal/pool"
	"github.com/yamada/multi-fx/pkg/currency"
)

// Strategy は SubPool に紐づく取引戦略のインターフェース
//
// TODO: シグネチャの詳細は未確定。戦略ごとのパラメータ設計が必要。
type Strategy interface {
	// Name は戦略識別子（ログ・メトリクス用）
	Name() string

	// OnTick は1ティックごとに呼ばれ、発注判断を行う
	// SubPool を通じて残高・ポジションを参照し、必要なら RequestOrder を呼ぶ
	OnTick(ctx context.Context, sp pool.SubPool, rates map[currency.Pair]currency.Rate) error

	// OnFill は自身の SubPool の約定通知を受け取る
	OnFill(ctx context.Context, sp pool.SubPool, fill pool.Fill) error

	// OnInstruction は Commander からの指示テキストを受け取り、戦略を調整する
	// LLM の非決定性はここで吸収し、内部の判断パラメータに変換する
	OnInstruction(ctx context.Context, instruction string) error
}

// StrategyFactory は設定マップから Strategy インスタンスを生成するファクトリ関数
type StrategyFactory func(cfg map[string]any) (Strategy, error)
