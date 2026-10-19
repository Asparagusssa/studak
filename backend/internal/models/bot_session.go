package models

import "time"

// BotSession is the in-chat wizard state for a messenger user (Max or later Telegram).
type BotSession struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	Channel    string `gorm:"size:16;uniqueIndex:idx_bot_sess,priority:1;not null" json:"channel"`
	ExternalID int64  `gorm:"uniqueIndex:idx_bot_sess,priority:2;not null;check:chk_bot_sessions_external_id_positive,external_id > 0" json:"external_id"`
	UserID     *uint  `gorm:"index" json:"user_id,omitempty"`
	Step       string `gorm:"size:32;not null" json:"step"`

	DraftName         string `gorm:"size:100" json:"draft_name"`
	DraftAge          int    `json:"draft_age"`
	DraftGender       string `gorm:"size:16" json:"draft_gender"`
	DraftBio          string `gorm:"size:1000" json:"draft_bio"`
	DraftUniversityID *uint  `json:"draft_university_id,omitempty"`
	DraftSearchMode   string `gorm:"size:16" json:"draft_search_mode"`
	DraftPreferred    string `gorm:"size:255" json:"draft_preferred"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
