package pool

// MasterPool は全体資金を管理し、SubPool の生成・回収を担う
type MasterPool interface {
	// CreateSubPool は資金を割り当てて新しい SubPool を生成する
	// strategyName は agent.Registry での戦略識別子
	CreateSubPool(initialFunds float64, strategyName string) (SubPool, error)

	// ReceiveFunds は解約した SubPool から返還資金を受け取る
	ReceiveFunds(from SubPoolID, amount float64) error

	// 資金サマリ
	TotalFunds() float64
	AllocatedFunds() float64
	FreeFunds() float64

	// SubPool アクセス
	AllSnapshots() []SubPoolSnapshot
	GetSubPool(id SubPoolID) (SubPool, bool)
}
