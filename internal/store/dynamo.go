package store

// dynamoStore は StateStore の DynamoDB 実装
//
// TODO: 実装予定
//   - テーブル設計: PK=SubPoolID, SK=バージョン or 単一レコード
//   - MasterPool 残高は別テーブル or 固定 PK でシングルレコード管理
//   - aws-sdk-go-v2 を使用予定
type dynamoStore struct {
	// TODO: dynamodb.Client など
}
