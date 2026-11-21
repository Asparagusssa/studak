package models

import "time"

type User struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	TelegramID *int64 `gorm:"uniqueIndex;check:chk_users_telegram_id_positive,telegram_id IS NULL OR telegram_id > 0" json:"-"`
	MaxID      *int64 `gorm:"uniqueIndex;check:chk_users_max_id_positive,max_id IS NULL OR max_id > 0" json:"-"`
	WebUUID    string `gorm:"size:36;uniqueIndex;not null" json:"-"`

	Name             string `gorm:"size:100;not null" json:"name"`
	Age              int    `gorm:"not null;check:chk_users_age,age >= 16 AND age <= 100" json:"age"`
	Gender           string `gorm:"size:16" json:"gender"`
	Bio              string `gorm:"size:1000" json:"bio"`
	PhotoURL         string `gorm:"size:512" json:"photo_url"`
	Paused           bool   `gorm:"not null" json:"-"`
	TelegramPhotoID  string `gorm:"type:text;not null" json:"-"`
	MaxPhotoToken    string `gorm:"type:text;not null" json:"-"`
	TelegramUsername string `gorm:"size:64;not null;default:''" json:"-"`
	MaxProfileURL    string `gorm:"size:512;not null;default:''" json:"-"`

	MyUniversityID *uint       `gorm:"index" json:"my_university_id"`
	MyUniversity   *University `gorm:"constraint:OnUpdate:CASCADE,OnDelete:SET NULL" json:"my_university,omitempty"`

	SearchGender          string       `gorm:"size:16;not null" json:"-"`
	SearchMode            string       `gorm:"size:16;not null;default:all;check:chk_users_search_mode,search_mode IN ('my_only','selected','all')" json:"search_mode"`
	PreferredUniversities []University `gorm:"many2many:user_preferred_universities;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"preferred_universities"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
