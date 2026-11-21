package models

import "time"

// Like stores any reaction of one user to another. IsLike=false is a dislike;
// likes exclude the target indefinitely, while dislikes hide it for a cooldown.
type Like struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	FromUserID uint      `gorm:"not null;uniqueIndex:idx_like_pair;check:chk_likes_not_self,from_user_id <> to_user_id" json:"from_user_id"`
	ToUserID   uint      `gorm:"not null;uniqueIndex:idx_like_pair;index" json:"to_user_id"`
	IsLike     bool      `gorm:"not null;default:true" json:"is_like"`
	IsMutual   bool      `gorm:"not null;default:false" json:"is_mutual"`
	FromUser   *User     `gorm:"constraint:OnDelete:CASCADE" json:"-"`
	ToUser     *User     `gorm:"constraint:OnDelete:CASCADE" json:"-"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
