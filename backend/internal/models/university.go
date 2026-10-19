package models

import (
	"strings"

	"gorm.io/gorm"
)

type University struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	Name      string `gorm:"size:255;not null;uniqueIndex" json:"name"`
	ShortName string `gorm:"size:64" json:"short_name"`
	// Search uses a normalized column so Cyrillic matching behaves consistently.
	SearchKey string `gorm:"size:512;index" json:"-"`
}

func (u *University) BeforeSave(_ *gorm.DB) error {
	u.SearchKey = strings.ToLower(u.ShortName + " " + u.Name)
	return nil
}
