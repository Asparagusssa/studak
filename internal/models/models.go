package models

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	SearchModeMyOnly   = "my_only"
	SearchModeSelected = "selected"
	SearchModeAll      = "all"
)

func IsValidSearchMode(mode string) bool {
	switch mode {
	case SearchModeMyOnly, SearchModeSelected, SearchModeAll:
		return true
	}
	return false
}

type University struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	Name      string `gorm:"size:255;not null;uniqueIndex" json:"name"`
	ShortName string `gorm:"size:64" json:"short_name"`
	// SQLite's LIKE is case-insensitive only for ASCII, so Cyrillic search
	// goes through a pre-lowercased column.
	SearchKey string `gorm:"size:512;index" json:"-"`
}

func (u *University) BeforeSave(_ *gorm.DB) error {
	u.SearchKey = strings.ToLower(u.ShortName + " " + u.Name)
	return nil
}

type User struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	TelegramID *int64 `gorm:"uniqueIndex" json:"telegram_id,omitempty"`
	MaxID      *int64 `gorm:"uniqueIndex" json:"max_id,omitempty"`
	WebUUID    string `gorm:"size:36;uniqueIndex;not null" json:"web_uuid"`

	Name     string `gorm:"size:100;not null" json:"name"`
	Age      int    `gorm:"not null" json:"age"`
	Gender   string `gorm:"size:16" json:"gender"`
	Bio      string `gorm:"size:1000" json:"bio"`
	PhotoURL string `gorm:"size:512" json:"photo_url"`

	MyUniversityID *uint       `gorm:"index" json:"my_university_id"`
	MyUniversity   *University `gorm:"constraint:OnUpdate:CASCADE,OnDelete:SET NULL" json:"my_university,omitempty"`

	SearchMode            string       `gorm:"size:16;not null;default:all" json:"search_mode"`
	PreferredUniversities []University `gorm:"many2many:user_preferred_universities" json:"preferred_universities"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Like stores any reaction of one user to another. IsLike=false is a dislike;
// both kinds exclude the target from the user's feed.
type Like struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	FromUserID uint      `gorm:"not null;uniqueIndex:idx_like_pair" json:"from_user_id"`
	ToUserID   uint      `gorm:"not null;uniqueIndex:idx_like_pair;index" json:"to_user_id"`
	IsLike     bool      `gorm:"not null;default:true" json:"is_like"`
	IsMutual   bool      `gorm:"not null;default:false" json:"is_mutual"`
	FromUser   *User     `gorm:"constraint:OnDelete:CASCADE" json:"-"`
	ToUser     *User     `gorm:"constraint:OnDelete:CASCADE" json:"-"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
