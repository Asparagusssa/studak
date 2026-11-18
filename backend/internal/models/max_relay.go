package models

import "time"

// MaxRelay deduplicates MAX retries without storing private message text.
type MaxRelay struct {
	MessageID string    `gorm:"size:128;primaryKey" json:"-"`
	CreatedAt time.Time `gorm:"index" json:"-"`
}
