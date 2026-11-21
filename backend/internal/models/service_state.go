package models

import "time"

// ServiceState is the single durable launch switch shared by both messengers.
type ServiceState struct {
	ID        uint `gorm:"primaryKey;check:chk_service_state_singleton,id = 1"`
	Started   bool `gorm:"not null;default:false"`
	UpdatedAt time.Time
}
