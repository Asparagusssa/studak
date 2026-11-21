package crud

import (
	"gorm.io/gorm"

	"studak/internal/models"
)

func profileViewer(db *gorm.DB, userID uint) (*models.User, error) {
	var viewer models.User
	if err := db.First(&viewer, userID).Error; err != nil {
		return nil, wrapNotFound(err)
	}
	if viewer.Banned {
		return nil, ErrBanned
	}
	if viewer.Platform() == "max" {
		if _, valid := models.NormalizeMaxContact(viewer.MaxContact); !valid {
			return nil, ErrContactRequired
		}
	}
	return &viewer, nil
}

// samePlatformProfiles applies the platform boundary independently of readiness.
func samePlatformProfiles(db *gorm.DB, viewer *models.User) *gorm.DB {
	switch viewer.Platform() {
	case "telegram":
		return db.Where("users.telegram_id IS NOT NULL AND users.max_id IS NULL")
	case "max":
		return db.Where("users.max_id IS NOT NULL AND users.telegram_id IS NULL")
	case "web":
		return db.Where("users.max_id IS NULL AND users.telegram_id IS NULL")
	default:
		return db.Where("1 = 0")
	}
}

func eligibleProfiles(db *gorm.DB, viewer *models.User) *gorm.DB {
	db = samePlatformProfiles(db, viewer).Where("users.banned = ?", false)
	if viewer.Platform() == "max" {
		db = db.Where("users.max_contact <> ''")
	}
	return db
}
