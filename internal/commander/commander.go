package commander

import (
	"context"

	"github.com/shopspring/decimal"
	"github.com/yamada/multi-fx/internal/agent"
	"github.com/yamada/multi-fx/internal/pool"
)

// MasterPoolAction は Commander が MasterPool に対して直接実行する操作
// Agent への指令（Suspend・SendInstruction）は agent.Instruction を使う
type MasterPoolAction struct {
	CreateSubPool   *CreateSubPoolParams   // 非 nil なら SubPool を生成する
	TerminateSubPool *TerminateSubPoolParams // 非 nil なら SubPool を解約する
}

type CreateSubPoolParams struct {
	Funds        decimal.Decimal
	StrategyName string
	Rationale    string
}

type TerminateSubPoolParams struct {
	TargetID  pool.SubPoolID
	Rationale string
}

// Commander は MasterPool の状態を把握し、SubPool のライフサイクルを制御する
// N ティックに1回 LLM に問い合わせ、MasterPool操作と Agent への指令を行う
type Commander interface {
	Tick(ctx context.Context, master pool.MasterPool, instructions chan<- agent.Instruction) error
}

// LLMChannel は LLM への問い合わせを抽象化する（非決定性の境界）
type LLMChannel interface {
	// Request は MasterPool の状態を LLM に提示し、操作指令を返す
	Request(ctx context.Context, snapshots []pool.SubPoolSnapshot) ([]MasterPoolAction, []agent.Instruction, error)
}
