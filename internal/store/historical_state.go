package store

import (
	"encoding/json"
	"fmt"
	"os"
)

// HistoricalState は historical モードのバックテスト進行状態を保持する
type HistoricalState struct {
	Cursor int `json:"cursor"` // 現在再生中の CSV 行インデックス
}

// LoadHistoricalState は path から HistoricalState を読み込む
// ファイルが存在しない場合はゼロ値を返す
func LoadHistoricalState(path string) (HistoricalState, error) {
	var s HistoricalState
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("historical state: read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("historical state: unmarshal %s: %w", path, err)
	}
	return s, nil
}

// SaveHistoricalState は path に HistoricalState を書き込む
func SaveHistoricalState(path string, s HistoricalState) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("historical state: marshal: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("historical state: write %s: %w", path, err)
	}
	return nil
}
