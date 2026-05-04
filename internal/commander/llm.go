package commander

// llmChannel は LLMChannel の実装
//
// TODO: 実装予定
//   - Claude API (claude-opus-4-6 / claude-sonnet-4-6) を使用予定
//   - MasterPool のスナップショットを JSON でシリアライズしてプロンプトに渡す
//   - レスポンスは structured output (JSON) で MasterPoolAction/Instruction にパースする
//   - 呼び出し頻度はコスト考慮で間引く設計（毎ティックではなく N ティックに1回など）
type llmChannel struct {
	// TODO: anthropic.Client など
}
