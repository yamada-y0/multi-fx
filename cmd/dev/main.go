package main

// ローカル開発用エントリポイント
//
// HistoricalDataBroker を使ってバックテストを実行する。
// Lambda を使わずローカルバイナリとして動かすため、
// StateStore はインメモリ実装に差し替えて使う想定。
//
// 使い方（予定）:
//   go run ./cmd/dev -data ./testdata/USDJPY_2024.csv -from 2024-01-01 -to 2024-12-31

func main() {
	// TODO: バックテスト実行ループ
}
