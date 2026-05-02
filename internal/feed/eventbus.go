package feed

import "time"

// EventType はシステム内部イベントの種別
type EventType string

const (
	EventSubPoolCreated    EventType = "subpool.created"
	EventSubPoolSuspended  EventType = "subpool.suspended"
	EventSubPoolTerminated EventType = "subpool.terminated"
	EventOrderSubmitted    EventType = "order.submitted"
	EventOrderFilled       EventType = "order.filled"
	EventOriginRuleFired   EventType = "rule.origin_fired" // 原点ルール発動
)

// Event はシステム内部を流れるイベント
type Event struct {
	Type       EventType
	Payload    any
	OccurredAt time.Time
}

// EventBus はシステム全体のイベント伝播バス（pub/sub）
type EventBus interface {
	// Publish はイベントを全購読者へ配信する
	Publish(e Event)

	// Subscribe は指定種別のイベントハンドラを登録する
	// 戻り値の関数を呼ぶと購読解除される
	Subscribe(eventType EventType, handler func(Event)) (unsubscribe func())
}
