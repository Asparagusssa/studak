package crud

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"studak/internal/models"
)

type LikeResult struct {
	Like    models.Like `json:"like"`
	IsMatch bool        `json:"is_match"`
}

// SetLike records a like (isLike=true) or dislike (isLike=false) from one user
// to another. Re-reacting overwrites the previous reaction. When both users
// like each other, IsMutual is set on both rows; a dislike breaks the match.
func SetLike(db *gorm.DB, fromUserID, toUserID uint, isLike bool) (*LikeResult, error) {
	if fromUserID == 0 || toUserID == 0 {
		return nil, invalid("from_user_id and to_user_id are required")
	}
	if fromUserID == toUserID {
		return nil, invalid("cannot like yourself")
	}

	var result LikeResult
	err := db.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&models.User{}).Where("id IN ?", []uint{fromUserID, toUserID}).Count(&count).Error; err != nil {
			return err
		}
		if count != 2 {
			return ErrNotFound
		}

		mutual := false
		if isLike {
			var reverse int64
			err := tx.Model(&models.Like{}).
				Where("from_user_id = ? AND to_user_id = ? AND is_like = ?", toUserID, fromUserID, true).
				Count(&reverse).Error
			if err != nil {
				return err
			}
			mutual = reverse > 0
		}

		like := models.Like{FromUserID: fromUserID, ToUserID: toUserID, IsLike: isLike, IsMutual: mutual}
		err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "from_user_id"}, {Name: "to_user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"is_like", "is_mutual", "updated_at"}),
		}).Create(&like).Error
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
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}
