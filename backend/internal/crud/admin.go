package crud

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"studak/internal/models"
)

func DatingStarted(db *gorm.DB) (bool, error) {
	var state models.ServiceState
	if err := db.First(&state, 1).Error; err != nil {
		return false, err
	}
	return state.Started, nil
}

func requireDatingStarted(db *gorm.DB) error {
	started, err := DatingStarted(db)
	if err != nil {
		return err
	}
	if !started {
		return ErrNotLaunched
	}
	return nil
}

func StartDating(db *gorm.DB) error {
	result := db.Model(&models.ServiceState{}).Where("id = ?", 1).Update("started", true)
	if result.Error == nil && result.RowsAffected == 0 {
		return ErrNotFound
	}
	return result.Error
}

// SetUserBanned serializes moderation against reactions and notifications.
func SetUserBanned(db *gorm.DB, id uint, banned bool) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var user models.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, id).Error; err != nil {
			return wrapNotFound(err)
		}
		if err := tx.Model(&user).Update("banned", banned).Error; err != nil {
			return err
		}
		if banned {
			// Undelivered mutuality notifications must not disclose banned profiles.
			return tx.Where("is_mutual = ? AND (from_user_id = ? OR to_user_id = ?)", true, id, id).Delete(&models.Like{}).Error
		}
		return nil
	})
}

// ResetSkips deletes only the selected user's outgoing negative reactions.
// A zero user ID targets all users. Likes and personal blocks are preserved.
func ResetSkips(db *gorm.DB, userID uint) (int64, error) {
	query := db.Where("is_like = ?", false)
	if userID != 0 {
		query = query.Where("from_user_id = ?", userID)
	}
	result := query.Delete(&models.Like{})
	return result.RowsAffected, result.Error
}

func AdminUsers(db *gorm.DB, offset, limit int) ([]models.User, error) {
	var users []models.User
	err := db.Preload("MyUniversity").Order("users.id").Offset(offset).Limit(limit).Find(&users).Error
	return users, err
}

type RegistrationStats struct{ Total, Telegram, Max, Web, Banned int64 }

func RegisteredUsers(db *gorm.DB) (RegistrationStats, error) {
	var stats RegistrationStats
	err := db.Model(&models.User{}).Select(`COUNT(*) AS total,
 COALESCE(SUM(CASE WHEN telegram_id IS NOT NULL AND max_id IS NULL THEN 1 ELSE 0 END),0) AS telegram,
 COALESCE(SUM(CASE WHEN max_id IS NOT NULL AND telegram_id IS NULL THEN 1 ELSE 0 END),0) AS max,
 COALESCE(SUM(CASE WHEN max_id IS NULL AND telegram_id IS NULL THEN 1 ELSE 0 END),0) AS web,
 COALESCE(SUM(CASE WHEN banned THEN 1 ELSE 0 END),0) AS banned`).Scan(&stats).Error
	return stats, err
}

// UniversityRegistrations counts registered profiles in one messenger, including
// paused profiles. Banned accounts are excluded; feed filters are not applied.
func UniversityRegistrations(db *gorm.DB, channel string, ids []uint) (map[uint]int64, error) {
	counts := make(map[uint]int64)
	if len(ids) == 0 {
		return counts, nil
	}
	var viewer models.User
	one := int64(1)
	switch channel {
	case "telegram":
		viewer.TelegramID = &one
	case "max":
		viewer.MaxID = &one
	default:
		return nil, ErrInvalidInput
	}
	var rows []struct {
		MyUniversityID uint
		Count          int64
	}
	err := samePlatformProfiles(db.Model(&models.User{}), &viewer).Select("my_university_id, COUNT(*) AS count").
		Where("banned = ? AND my_university_id IN ?", false, ids).Group("my_university_id").Scan(&rows).Error
	for _, row := range rows {
		counts[row.MyUniversityID] = row.Count
	}
	return counts, err
}
