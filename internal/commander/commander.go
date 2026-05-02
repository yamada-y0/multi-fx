package commander

import (
	"context"

	"github.com/yamada/multi-fx/internal/pool"
	"github.com/yamada/multi-fx/internal/rule"
)

// ActionType は Commander が SubPool に対して実行するアクションの種別
type ActionType int

const (
	ActionNoOp          ActionType = iota
	ActionCreateSubPool            // 新規 SubPool 生成（MasterPool から資金割り当て）
	ActionSuspendSubPool           // SubPool を一時停止
	ActionTerminateSubPool         // SubPool を解約（Masterへ返還）
	ActionSendInstruction          // SubAgent へ指示テキストを送る
)

// Directive は Commander が1ティックに生成する操作指令
// LLM の出力（非決定的）をここに閉じ込め、以降は決定論的に処理する
type Directive struct {
	Action       ActionType
	TargetID     pool.SubPoolID // ActionCreate のときは空
	Funds        float64        // ActionCreate のときの初期割り当て額
	StrategyName string         // ActionCreate のときの戦略名
	Instruction  string         // ActionSendInstruction のときの指示テキスト
	Rationale    string         // LLM の判断理由（監査ログ用）
}

// Commander は MasterPool の状態を把握し、SubPool のライフサイクルを制御する
type Commander interface {
	// Tick は1ティックごとに呼ばれる
	// 1. MasterPool のスナップショットを取得
	// 2. InstructionChannel 経由で LLM に問い合わせ（任意）
	// 3. RuleEngine で強制アクションを確認
	// 4. Directive を実行
	Tick(ctx context.Context, master pool.MasterPool) error
}

// InstructionChannel は LLM への問い合わせを抽象化する（非決定性の境界）
//
// TODO: プロンプト設計・JSON スキーマは別途定義
// TODO: モデル選定・呼び出し頻度は未決定
type InstructionChannel interface {
	// Request は MasterPool の状態を LLM に提示し、Directive の候補を返す
	// 戻り値は LLM の提案であり、強制アクション（RuleEngine の判断）は上書きされる
	Request(ctx context.Context, snapshots []pool.SubPoolSnapshot) ([]Directive, error)
}

// DirectiveExecutor は Directive を受け取って実際の副作用を実行する
// （テストで差し替えられるよう分離）
type DirectiveExecutor interface {
	Execute(ctx context.Context, master pool.MasterPool, d Directive, engine *rule.RuleEngine) error
}
