package order

// PositionIDMapper はシステム内部のPositionIDとBroker側のPositionIDを相互変換する
// 初期実装はBrokerのIDをそのまま流用する恒等変換
type PositionIDMapper interface {
	// ToBrokerID はシステム内部のPositionIDをBroker側のIDに変換する
	ToBrokerID(systemID string) (brokerID string, ok bool)

	// Register はBrokerが払い出したPositionIDをシステム内部IDと紐付けて登録する
	Register(systemID, brokerID string)
}

// IdentityMapper はBrokerのPositionIDをそのままシステム内部IDとして流用する恒等変換
// BrokerのIDがシステム全体でユニークであることを前提とする
type IdentityMapper struct{}

func NewIdentityMapper() *IdentityMapper { return &IdentityMapper{} }

func (m *IdentityMapper) ToBrokerID(systemID string) (string, bool) {
	return systemID, true
}

func (m *IdentityMapper) Register(systemID, brokerID string) {
	// 恒等変換なのでマッピング不要（systemID == brokerID を前提とする）
}
