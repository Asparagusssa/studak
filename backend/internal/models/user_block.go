package models

import "time"

type UserBlock struct {
	BlockerID uint      `gorm:"primaryKey;autoIncrement:false" json:"-"`
	BlockedID uint      `gorm:"primaryKey;autoIncrement:false;index;check:chk_user_blocks_not_self,blocker_id <> blocked_id" json:"-"`
	Blocker   *User     `gorm:"foreignKey:BlockerID;constraint:OnDelete:CASCADE" json:"-"`
	Blocked   *User     `gorm:"foreignKey:BlockedID;constraint:OnDelete:CASCADE" json:"-"`
	CreatedAt time.Time `json:"-"`
}
