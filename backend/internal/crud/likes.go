package crud

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"studak/internal/models"
)

const SkippedProfileCooldown = 2 * time.Hour

// excludedReactions hides likes indefinitely and skips until the cooldown ends.
func excludedReactions(db *gorm.DB, userID uint) *gorm.DB {
	return db.Model(&models.Like{}).Select("to_user_id").
		Where("from_user_id = ?", userID).
		Where("is_like = ? OR updated_at > ?", true, db.NowFunc().Add(-SkippedProfileCooldown))
}

type LikeResult struct {
	Like     models.Like `json:"like"`
	IsMatch  bool        `json:"is_match"`
	NewLike  bool        `json:"-"`
	NewMatch bool        `json:"-"`
}

// SetLike records a like (isLike=true) or dislike (isLike=false) from one user
// to another. Re-reacting overwrites the previous reaction. When both users
// like each other, IsMutual is set on both rows; a dislike breaks the match.
func SetLike(db *gorm.DB, fromUserID, toUserID uint, isLike bool) (*LikeResult, error) {
	return setLike(db, fromUserID, toUserID, isLike, false, false)
}

// ReactOnce protects the bot against old buttons. A reaction to an inbox card
// is accepted only while the other person still has a positive reaction.
func ReactOnce(db *gorm.DB, fromUserID, toUserID uint, isLike, inbox bool) (*LikeResult, error) {
	return setLike(db, fromUserID, toUserID, isLike, true, inbox)
}

func setLike(db *gorm.DB, fromUserID, toUserID uint, isLike, once, inbox bool) (*LikeResult, error) {
	if fromUserID == 0 || toUserID == 0 {
		return nil, invalid("from_user_id and to_user_id are required")
	}
	if fromUserID == toUserID {
		return nil, invalid("cannot like yourself")
	}

	var result LikeResult
	err := db.Transaction(func(tx *gorm.DB) error {
		users, err := lockUserPair(tx, fromUserID, toUserID)
		if err != nil {
			return err
		}
		blocked, err := IsPairBlocked(tx, fromUserID, toUserID)
		if err != nil {
			return err
		}
		if blocked {
			return ErrNotFound
		}
		for _, user := range users {
			if user.Paused {
				if user.ID == fromUserID {
					return ErrProfilePaused
				}
				return ErrNotFound
			}
		}

		var previous models.Like
		err = tx.Where("from_user_id = ? AND to_user_id = ?", fromUserID, toUserID).Take(&previous).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		now := tx.NowFunc()
		if once && err == nil && (previous.IsLike || previous.UpdatedAt.After(now.Add(-SkippedProfileCooldown))) {
			return ErrAlreadyExists
		}

		var reverse int64
		if isLike || inbox {
			if err := tx.Model(&models.Like{}).
				Where("from_user_id = ? AND to_user_id = ? AND is_like = ?", toUserID, fromUserID, true).
				Count(&reverse).Error; err != nil {
				return err
			}
		}
		if inbox && reverse == 0 {
			return ErrNotFound
		}
		mutual := isLike && reverse > 0
		// A map writes false explicitly instead of applying IsLike's true default.
		err = tx.Model(&models.Like{}).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "from_user_id"}, {Name: "to_user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"is_like", "is_mutual", "updated_at"}),
		}).Create(map[string]any{
			"from_user_id": fromUserID, "to_user_id": toUserID,
			"is_like": isLike, "is_mutual": mutual,
			"created_at": now, "updated_at": now,
		}).Error
		if err != nil {
			return err
		}

		// Keep the reverse row in sync: it becomes mutual on a match and loses
		// mutuality if this user switched to a dislike.
		err = tx.Model(&models.Like{}).
			Where("from_user_id = ? AND to_user_id = ? AND is_like = ?", toUserID, fromUserID, true).
			Update("is_mutual", mutual).Error
		if err != nil {
			return err
		}

		if err := tx.Where("from_user_id = ? AND to_user_id = ?", fromUserID, toUserID).First(&result.Like).Error; err != nil {
			return err
		}
		result.IsMatch = mutual
		result.NewLike = isLike && !previous.IsLike
		result.NewMatch = mutual && !previous.IsMutual
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// NextIncomingLike lists positive incoming reactions. Skipped senders can
// reappear after the cooldown while their incoming like remains active.
func NextIncomingLike(db *gorm.DB, viewerID uint) (*models.User, error) {
	var viewer models.User
	if err := db.Select("id", "paused").First(&viewer, viewerID).Error; err != nil {
		return nil, wrapNotFound(err)
	}
	if viewer.Paused {
		return nil, ErrProfilePaused
	}
	answered := excludedReactions(db, viewerID)
	var user models.User
	err := excludeBlocked(db, viewerID).Preload("MyUniversity").
		Where("users.paused = ?", false).
		Joins("JOIN likes AS incoming ON incoming.from_user_id = users.id").
		Where("incoming.to_user_id = ? AND incoming.is_like = ?", viewerID, true).
		Where("users.id NOT IN (?)", answered).
		Order("incoming.created_at DESC, incoming.id DESC").
		Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNoProfiles
	}
	return &user, err
}

func ListMatches(db *gorm.DB, userID uint, offset, limit int) ([]models.User, error) {
	var users []models.User
	err := excludeBlocked(db, userID).
		Joins("JOIN likes AS outgoing ON outgoing.to_user_id = users.id").
		Where("outgoing.from_user_id = ? AND outgoing.is_like = ? AND outgoing.is_mutual = ?", userID, true, true).
		Order("outgoing.updated_at DESC, outgoing.id DESC").Offset(offset).Limit(limit).
		Find(&users).Error
	return users, err
}

func MatchedUser(db *gorm.DB, userID, peerID uint) (*models.User, error) {
	var user models.User
	err := excludeBlocked(db, userID).
		Joins("JOIN likes AS outgoing ON outgoing.to_user_id = users.id").
		Where("users.id = ? AND outgoing.from_user_id = ? AND outgoing.is_like = ? AND outgoing.is_mutual = ?", peerID, userID, true, true).
		Take(&user).Error
	return &user, wrapNotFound(err)
}
