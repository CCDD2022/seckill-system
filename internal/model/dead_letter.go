package model

import "time"

// DeadLetter preserves failed deliveries before the DLQ consumer acknowledges
// them. DeliveryKey makes redelivery after a database commit idempotent.
type DeadLetter struct {
	ID             int64      `gorm:"column:id;primaryKey;autoIncrement"`
	DeliveryKey    string     `gorm:"column:delivery_key;size:64;not null;uniqueIndex"`
	MessageID      string     `gorm:"column:message_id;type:text"`
	Exchange       string     `gorm:"column:exchange;size:255"`
	RoutingKey     string     `gorm:"column:routing_key;size:255"`
	Body           []byte     `gorm:"column:body;type:longblob;not null"`
	ReplayCount    int64      `gorm:"column:replay_count;not null;default:0"`
	LastReplayedAt *time.Time `gorm:"column:last_replayed_at"`
	CompensatedAt  *time.Time `gorm:"column:compensated_at"`
	CreatedAt      time.Time  `gorm:"column:created_at;autoCreateTime"`
}

func (DeadLetter) TableName() string { return "dead_letters" }
