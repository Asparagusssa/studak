package crud

import (
	"gorm.io/gorm"
	"studak/internal/models"
)

// DeliverPendingMatch keeps reciprocal likes only as a temporary delivery task.
// Each recipient is committed separately, so retrying the second delivery does
// not resend the first. No match remains after both messages are sent.
func DeliverPendingMatch(db *gorm.DB, a, b uint, send func(*models.User, *models.User) error) error {
	for _, recipient := range []uint{a, b} {
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := requireDatingStarted(tx); err != nil {
				return err
			}
			users, err := lockUserPair(tx, a, b)
			if err != nil {
				return err
			}
			if users[0].Banned || users[1].Banned {
				return ErrBanned
			}
			if users[0].Platform() == "" || users[0].Platform() != users[1].Platform() {
				return ErrNotFound
			}
			var pair []models.Like
			if err := tx.Where("((from_user_id = ? AND to_user_id = ?) OR (from_user_id = ? AND to_user_id = ?)) AND is_like = ? AND is_mutual = ?", a, b, b, a, true, true).Find(&pair).Error; err != nil {
				return err
			}
			if len(pair) != 2 {
				return nil
			}
			for i := range pair {
				row := &pair[i]
				if row.FromUserID != recipient || row.MatchNotified {
					continue
				}
				me, peer := &users[0], &users[1]
				if me.ID != recipient {
					me, peer = peer, me
				}
				if err := send(me, peer); err != nil {
					return err
				}
				if err := tx.Model(row).Update("match_notified", true).Error; err != nil {
					return err
				}
				row.MatchNotified = true
			}
			if pair[0].MatchNotified && pair[1].MatchNotified {
				return tx.Where("id IN ?", []uint{pair[0].ID, pair[1].ID}).Delete(&models.Like{}).Error
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func PendingMatches(db *gorm.DB, channel string) ([]models.Like, error) {
	var viewer models.User
	one := int64(1)
	if channel == "telegram" {
		viewer.TelegramID = &one
	} else {
		viewer.MaxID = &one
	}
	var rows []models.Like
	err := samePlatformProfiles(db.Table("likes").Select("likes.*").Joins("JOIN users ON users.id = likes.from_user_id"), &viewer).
		Where("likes.is_mutual = ? AND likes.match_notified = ? AND users.banned = ?", true, false, false).
		Order("likes.updated_at, likes.id").Limit(20).Scan(&rows).Error
	return rows, err
}
