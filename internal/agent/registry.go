package agent

import "fmt"

// Registry は戦略名から StrategyFactory を引けるレジストリ
type Registry struct {
	factories map[string]StrategyFactory
}

func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]StrategyFactory)}
}

// Register は戦略名と StrategyFactory を登録する
func (r *Registry) Register(name string, f StrategyFactory) {
	r.factories[name] = f
}

// Create は戦略名と設定から Strategy インスタンスを生成する
func (r *Registry) Create(name string, cfg map[string]any) (Strategy, error) {
	f, ok := r.factories[name]
	if !ok {
		return nil, fmt.Errorf("strategy %q not found in registry", name)
	}
	return f(cfg)
}
