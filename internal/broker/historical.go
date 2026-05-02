package broker

// historicalBroker は HistoricalBroker の実装（バックテスト用）
//
// TODO: 実装予定
//   - CSV / OHLCV ファイルからレートデータを読み込む
//   - PlaceOrder は読み込んだ Bid/Ask から即時仮想約定する
//   - Advance() でティックを進め、RateFeed に Publish する
type historicalBroker struct {
	// TODO
}
