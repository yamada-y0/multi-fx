package pool

import "fmt"

// LifecycleState は SubPool の状態を表す（一方向 FSM）
type LifecycleState int

const (
	StateActive     LifecycleState = iota // 運用中
	StateSuspended                        // 停止（新規発注不可、既存ポジション保持）
	StateTerminated                       // 解約済み（残高 Master 返還完了）
)

func (s LifecycleState) String() string {
	switch s {
	case StateActive:
		return "active"
	case StateSuspended:
		return "suspended"
	case StateTerminated:
		return "terminated"
	default:
		return "unknown"
	}
}

// validTransitions は合法な状態遷移の一覧
// Active → Suspended, Suspended → Terminated のみ許可
// Active → Terminated の直接遷移は禁止
var validTransitions = map[LifecycleState]LifecycleState{
	StateActive:    StateSuspended,
	StateSuspended: StateTerminated,
}

// ValidateTransition は遷移が合法かを確認する
func ValidateTransition(current, next LifecycleState) error {
	allowed, ok := validTransitions[current]
	if !ok || allowed != next {
		return fmt.Errorf("invalid lifecycle transition: %s -> %s", current, next)
	}
	return nil
}
