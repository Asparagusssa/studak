package models

import "time"

// No message text is retained, only a hash of the platform event identifier.
type MaxWebhookEvent struct {
	Key       string    `gorm:"size:64;primaryKey"`
	CreatedAt time.Time `gorm:"index"`
}
