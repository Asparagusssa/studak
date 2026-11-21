package maxbot

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm/clause"

	"studak/internal/models"
)

// A single running backend serializes updates for a person. Separate people
// can use the bot concurrently. Durable IDs prevent replay after restart.
var updateLocks [128]sync.Mutex

func (b *Bot) handleOnce(u Update) error {
	var userID int64
	var identity string
	switch u.UpdateType {
	case "message_created":
		if u.Message != nil {
			userID = u.Message.Sender.UserID
			if u.Message.Body.Mid != "" {
				identity = "message:" + u.Message.Body.Mid
			}
		}
	case "message_callback":
		if u.Callback != nil {
			userID = u.Callback.User.UserID
			if u.Callback.CallbackID != "" {
				identity = "callback:" + u.Callback.CallbackID
			}
		}
	case "bot_started":
		if u.User != nil {
			userID = u.User.UserID
			if u.Timestamp > 0 {
				identity = fmt.Sprintf("start:%d:%d", userID, u.Timestamp)
			}
		}
	}
	lock := &updateLocks[uint64(userID)%uint64(len(updateLocks))]
	lock.Lock()
	defer lock.Unlock()

	var key string
	if identity != "" {
		sum := sha256.Sum256([]byte(b.channel() + ":" + identity))
		key = hex.EncodeToString(sum[:])
		var count int64
		if err := b.DB.Model(&models.MaxWebhookEvent{}).Where("key = ?", key).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
	}
	if err := b.handleUpdate(u); err != nil {
		return err
	}
	if key == "" {
		return nil
	}
	if err := b.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&models.MaxWebhookEvent{Key: key}).Error; err != nil {
		return err
	}
	return b.DB.Where("created_at < ?", time.Now().Add(-7*24*time.Hour)).Delete(&models.MaxWebhookEvent{}).Error
}
