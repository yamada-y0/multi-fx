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
	InstructionSuspend         InstructionType = iota // SubPool を停止する
	InstructionSendInstruction                        // 戦略パラメータの調整指示
)

// Instruction は Commander から Agent への指令
// MasterPool操作（SubPool生成・解約）はCommanderが直接実行するため含まない
type Instruction struct {
	Type      InstructionType
	Text      string // InstructionSendInstruction のときの指示テキスト
	Rationale string // LLM の判断理由（監査ログ用）
}

// Agent は SubPool を1つ担当するエンティティ。
// LLMを頭脳として持つ実装（LLMAgent）とルールベース実装（DummyAgent）がある。
// 起動判断・発注依頼・ウェイクアップ管理はすべてAgent内部に閉じる。
type Agent interface {
	// ID は担当 SubPool の ID を返す
	ID() pool.SubPoolID

	// SubPool は担当 SubPool を返す
	SubPool() pool.SubPool

	// ShouldWakeup は現在のレートと時刻をもとに、今ティックで Tick を呼ぶべきかを判断する
	ShouldWakeup(ctx context.Context, rate currency.Rate) (bool, error)

	// Tick は現在の状況を判断し、発注依頼を返す。ウェイクアップ条件も内部で更新する
	Tick(ctx context.Context, mkt market.MarketContext) ([]pool.OrderRequest, error)

	// ApplyInstruction は Commander からの Instruction を自身に適用する
	ApplyInstruction(ctx context.Context, inst Instruction) error
}
