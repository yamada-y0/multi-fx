package agent

import (
	"context"

	"github.com/yamada/multi-fx/internal/pool"
	"github.com/yamada/multi-fx/pkg/currency"
	"github.com/yamada/multi-fx/pkg/market"
)

// InstructionType は Commander から Agent への指令種別
type InstructionType int

const (
	InstructionSuspend        InstructionType = iota // SubPool を停止する
	InstructionSendInstruction                        // 戦略パラメータの調整指示
)

// Instruction は Commander から Agent への指令
// MasterPool操作（SubPool生成・解約）はCommanderが直接実行するため含まない
type Instruction struct {
	Type    InstructionType
	Text    string // InstructionSendInstruction のときの指示テキスト
	Rationale string // LLM の判断理由（監査ログ用）
}

// Strategy は発注判断ロジックのインターフェース
// 副作用を持たず、TickResult を返すだけ
type Strategy interface {
	// Name は戦略識別子（ログ・メトリクス用）
	Name() string

	// OnTick は起動条件を満たしたときに呼ばれ、発注依頼とウェイクアップ条件を返す
	// Wakeup が非 nil のとき、Agent は条件を満たすまで次回以降の OnTick をスキップする
	OnTick(ctx context.Context, snap pool.SubPoolSnapshot, mkt market.MarketContext) (TickResult, error)

	// OnInstruction は Commander からの指示テキストを受け取り、戦略パラメータを調整する
	// LLM の非決定性はここで吸収する
	OnInstruction(ctx context.Context, instruction string) error
}

// StrategyFactory は設定マップから Strategy インスタンスを生成するファクトリ関数
type StrategyFactory func(cfg map[string]any) (Strategy, error)

// Agent は SubPool を1つ担当し、起動判断・発注依頼・ウェイクアップ管理を行うエンティティ
type Agent interface {
	// ID は担当 SubPool の ID を返す
	ID() pool.SubPoolID

	// SubPool は担当 SubPool を返す
	SubPool() pool.SubPool

	// ShouldWakeup は現在のレートと時刻をもとに、今ティックで OnTick を呼ぶべきかを判断する
	ShouldWakeup(ctx context.Context, rate currency.Rate) (bool, error)

	// Tick は OnTick を呼び出し、発注依頼を返す。ウェイクアップ条件も内部で更新する
	Tick(ctx context.Context, mkt market.MarketContext) ([]pool.OrderRequest, error)

	// ApplyInstruction は Commander からの Instruction を自身に適用する
	ApplyInstruction(ctx context.Context, inst Instruction) error
}
