package clock

import "time"

// Clock は現在時刻を返す抽象。テスト時に差し替えられる。
type Clock interface {
	Now() time.Time
}

// RealClock は time.Now() を使う本番実装
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }

// FixedClock はテスト用の固定時刻実装
type FixedClock struct{ T time.Time }

func (f FixedClock) Now() time.Time { return f.T }
