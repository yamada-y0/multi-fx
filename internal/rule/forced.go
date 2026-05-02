package rule

import "github.com/yamada/multi-fx/internal/pool"

// Action は強制アクションの種別（決定論的ルールエンジンの出力）
type Action int

const (
	ActionNone      Action = iota // 何もしない
	ActionSuspend                 // SubPool を強制停止
	ActionTerminate               // SubPool を強制解約（Suspend 後に呼ぶ）
)

// Rule は SubPool に対して決定論的に評価するルール
type Rule interface {
	// Name はルール識別子（ログ用）
	Name() string

	// Evaluate は SubPool のスナップショットを評価し、
	// 強制アクションが必要なら該当の Action を返す
	Evaluate(snap pool.SubPoolSnapshot) Action
}

// OriginRule は「実質残高が初期割り当てを下回ったら強制停止」の原点ルール
// これが全戦略共通の安全装置
type OriginRule struct {
	// ThresholdRatio: デフォルト 1.0（初期残高と同額で発動）
	// 0.9 なら初期残高の 90% を下回ったときに発動
	ThresholdRatio float64
}

func (r OriginRule) Name() string { return "origin_rule" }

func (r OriginRule) Evaluate(snap pool.SubPoolSnapshot) Action {
	threshold := snap.InitialBalance * r.ThresholdRatio
	if snap.EquityBalance() < threshold {
		return ActionSuspend
	}
	return ActionNone
}

// RuleEngine は登録された全ルールを評価し、最も強い Action を返す（決定論的）
type RuleEngine struct {
	rules []Rule
}

func NewRuleEngine() *RuleEngine {
	return &RuleEngine{}
}

func (e *RuleEngine) Register(rule Rule) {
	e.rules = append(e.rules, rule)
}

// Evaluate は全ルールを評価し、最も強いアクションを返す
// ActionTerminate > ActionSuspend > ActionNone の優先順位
func (e *RuleEngine) Evaluate(snap pool.SubPoolSnapshot) Action {
	result := ActionNone
	for _, rule := range e.rules {
		a := rule.Evaluate(snap)
		if a > result {
			result = a
		}
	}
	return result
}
