package broker

import (
	"encoding/json"
	"fmt"
	"os"
)

// LoadHistoricalBrokerSnapshot は path から HistoricalBrokerSnapshot を読み込む
// ファイルが存在しない場合はゼロ値を返す
func LoadHistoricalBrokerSnapshot(path string) (HistoricalBrokerSnapshot, error) {
	var snap HistoricalBrokerSnapshot
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return snap, nil
	}
	if err != nil {
		return snap, fmt.Errorf("broker snapshot: read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &snap); err != nil {
		return snap, fmt.Errorf("broker snapshot: unmarshal %s: %w", path, err)
	}
	return snap, nil
}

// SaveHistoricalBrokerSnapshot は path に HistoricalBrokerSnapshot を書き込む
func SaveHistoricalBrokerSnapshot(path string, snap HistoricalBrokerSnapshot) error {
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("broker snapshot: marshal: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("broker snapshot: write %s: %w", path, err)
	}
	return nil
}
