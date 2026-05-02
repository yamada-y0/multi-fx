package feed

import "github.com/yamada/multi-fx/pkg/currency"

// RateFeed は通貨ペアのレートを購読者へ配信する
type RateFeed interface {
	// Subscribe は指定通貨ペアのレート更新を ch へ送信するよう登録する
	// 戻り値の関数を呼ぶと購読解除される
	Subscribe(pair currency.Pair, ch chan<- currency.Rate) (unsubscribe func())

	// Publish は新しいレートを全購読者へ配信する
	Publish(r currency.Rate)
}
