package model

import "time"

// OutboxEvent is committed in the same MySQL transaction as an order state
// transition. A relay publishes it with a stable EventID and marks it sent only
// after RabbitMQ confirms delivery to a queue.
type OutboxEvent struct {
	ID            int64      `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	EventID       string     `gorm:"column:event_id;size:191;not null;uniqueIndex" json:"event_id"`
	RoutingKey    string     `gorm:"column:routing_key;size:191;not null" json:"routing_key"`
	Payload       string     `gorm:"column:payload;type:longtext;not null" json:"payload"`
	Attempts      int64      `gorm:"column:attempts;not null;default:0" json:"attempts"`
	LastError     string     `gorm:"column:last_error;type:text" json:"last_error,omitempty"`
	NextAttemptAt *time.Time `gorm:"column:next_attempt_at;index" json:"next_attempt_at,omitempty"`
	PublishedAt   *time.Time `gorm:"column:published_at;index" json:"published_at,omitempty"`
	CreatedAt     time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

func (OutboxEvent) TableName() string { return "outbox_events" }
