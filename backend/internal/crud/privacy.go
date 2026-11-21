package crud

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"studak/internal/models"
)

const pairBlock = "(blocker_id = ? AND blocked_id = ?) OR (blocker_id = ? AND blocked_id = ?)"

// Lock in ID order so a reaction cannot race with a block for the same pair.
func lockUserPair(tx *gorm.DB, a, b uint) ([]models.User, error) {
	var users []models.User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id", "paused").Where("id IN ?", []uint{a, b}).Order("id").Find(&users).Error; err != nil {
		return nil, err
	}
	if len(users) != 2 {
		return nil, ErrNotFound
	}
	return users, nil
}

func IsPairBlocked(db *gorm.DB, a, b uint) (bool, error) {
	var count int64
	err := db.Model(&models.UserBlock{}).Where(pairBlock, a, b, b, a).Count(&count).Error
	return count > 0, err
}

func excludeBlocked(db *gorm.DB, viewer uint) *gorm.DB {
	return db.Where(`NOT EXISTS (
		SELECT 1 FROM user_blocks AS b
		WHERE (b.blocker_id = ? AND b.blocked_id = users.id) OR (b.blocked_id = ? AND b.blocker_id = users.id)
	)`, viewer, viewer)
}

func BlockUser(db *gorm.DB, actor, peer uint) error {
	if actor == 0 || peer == 0 || actor == peer {
		return ErrInvalidInput
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if _, err := lockUserPair(tx, actor, peer); err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&models.UserBlock{BlockerID: actor, BlockedID: peer}).Error; err != nil {
			return err
		}
		return tx.Where("(from_user_id = ? AND to_user_id = ?) OR (from_user_id = ? AND to_user_id = ?)", actor, peer, peer, actor).
			Delete(&models.Like{}).Error
	})
}

func UnblockUser(db *gorm.DB, actor, peer uint) error {
	return db.Where("blocker_id = ? AND blocked_id = ?", actor, peer).Delete(&models.UserBlock{}).Error
}

func ListBlockedUsers(db *gorm.DB, actor uint, offset, limit int) ([]models.User, error) {
	var users []models.User
	err := db.Joins("JOIN user_blocks AS blocked ON blocked.blocked_id = users.id").
		Where("blocked.blocker_id = ?", actor).
		Order("blocked.created_at DESC, blocked.blocked_id DESC").Offset(offset).Limit(limit).
		Find(&users).Error
	return users, err
}

func SetProfilePaused(db *gorm.DB, userID uint, paused bool) error {
	result := db.Model(&models.User{}).Where("id = ?", userID).Update("paused", paused)
	if result.Error == nil && result.RowsAffected == 0 {
		return ErrNotFound
	}
	return result.Error
}

func DeleteMaxProfile(db *gorm.DB, userID uint, maxID int64, confirmationStep string) error {
	return DeleteMessengerProfile(db, userID, maxID, "max", confirmationStep)
}

func DeleteMessengerProfile(db *gorm.DB, userID uint, externalID int64, channel, confirmationStep string) error {
	column := "max_id"
	if channel == "telegram" {
		column = "telegram_id"
	} else if channel != "max" {
		return ErrInvalidInput
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var user models.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND "+column+" = ?", userID, externalID).First(&user).Error; err != nil {
			return wrapNotFound(err)
		}
		confirmation := tx.Where("channel = ? AND external_id = ? AND step = ?", channel, externalID, confirmationStep).
			Delete(&models.BotSession{})
		if confirmation.Error != nil {
			return confirmation.Error
		}
		if confirmation.RowsAffected == 0 {
			return ErrNotFound
		}
		if err := tx.Where("user_id = ?", userID).Delete(&models.BotSession{}).Error; err != nil {
			return err
		}

		if err := tx.Model(&models.BotSession{}).Where("peer_user_id = ?", userID).Update("peer_user_id", nil).Error; err != nil {
			return err
		}
		// PostgreSQL cascades matching, block and university links.
		return tx.Delete(&user).Error
	})
}
