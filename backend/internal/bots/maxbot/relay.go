package maxbot

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"studak/internal/crud"
	"studak/internal/models"
)

const relayTextLimit = 1200

func writeKeyboard() [][]Button {
	return [][]Button{{CallbackButton("Отмена", "cancel_write:")}}
}

func (b *Bot) beginWrite(s *models.BotSession, raw string) error {
	_, peer, err := b.findMatch(s, raw)
	if errors.Is(err, crud.ErrNotFound) {
		return b.matchGone(s)
	}
	if err != nil {
		return err
	}
	if !b.canWrite(peer) {
		if err := b.leaveWrite(s); err != nil {
			return err
		}
		return b.send(s.ExternalID, b.matchText(peer), b.matchKeyboard(peer))
	}

	s.Step = stepWrite
	s.PeerUserID = &peer.ID
	if err := b.saveSession(s); err != nil {
		return err
	}
	text := fmt.Sprintf("Сообщение для %s 💬\n\nНапиши один текст — я передам его. Твой номер и личный профиль не раскрываются автоматически.", peer.Name)
	return b.send(s.ExternalID, text, writeKeyboard())
}

func (b *Bot) relayMessage(s *models.BotSession, body Body) error {
	if s.PeerUserID == nil {
		return b.matchGone(s)
	}
	text := strings.TrimSpace(body.Text)
	if text == "" || utf8.RuneCountInString(text) > relayTextLimit || len(body.Attachments) > 0 {
		return b.send(s.ExternalID, "Отправь текст до 1200 символов, без вложений.", writeKeyboard())
	}
	if body.Mid == "" || len(body.Mid) > 128 {
		return b.send(s.ExternalID, "Не удалось прочитать это сообщение. Отправь его ещё раз.", writeKeyboard())
	}

	me, peer, err := b.findMatch(s, strconv.FormatUint(uint64(*s.PeerUserID), 10))
	if errors.Is(err, crud.ErrNotFound) {
		return b.matchGone(s)
	}
	if err != nil {
		return err
	}
	if !b.canWrite(peer) {
		return b.matchGone(s)
	}

	claimed := false
	err = b.DB.Transaction(func(tx *gorm.DB) error {
		if err := crud.LockMatch(tx, me.ID, peer.ID); err != nil {
			return err
		}
		if err := tx.Where("created_at < ?", time.Now().Add(-7*24*time.Hour)).Delete(&models.MaxRelay{}).Error; err != nil {
			return err
		}
		entry := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&models.MaxRelay{MessageID: body.Mid})
		if entry.Error != nil || entry.RowsAffected == 0 {
			return entry.Error
		}
		change := tx.Model(&models.BotSession{}).
			Where("id = ? AND step = ? AND peer_user_id = ?", s.ID, stepWrite, peer.ID).
			Updates(map[string]any{"step": stepMenu, "peer_user_id": nil})
		claimed = change.RowsAffected > 0
		return change.Error
	})
	if errors.Is(err, crud.ErrNotFound) {
		return b.matchGone(s)
	}
	if err != nil || !claimed {
		return err
	}

	received := fmt.Sprintf("💬 Сообщение от %s, %d\n\n%s", me.Name, me.Age, text)
	err = b.notify(peer, received, [][]Button{
		{CallbackButton("↩️ Ответить", fmt.Sprintf("write:%d", me.ID))},
		{moreButton(me.ID), CallbackButton("Мои мэтчи", "matches:0")},
	})
	sent := "Сообщение отправлено."
	if err != nil {
		sent = "Не удалось подтвердить отправку сообщения. Если ответа нет, попробуй написать позже."
	}
	return b.send(s.ExternalID, sent, [][]Button{
		{CallbackButton("Написать ещё", fmt.Sprintf("write:%d", peer.ID)), CallbackButton("Мои мэтчи", "matches:0")},
		{CallbackButton("В меню", "menu:home")},
	})
}

func (b *Bot) relaySeen(mid string) (bool, error) {
	if len(mid) > 128 {
		return false, nil
	}
	var count int64
	err := b.DB.Model(&models.MaxRelay{}).Where("message_id = ?", mid).Count(&count).Error
	return count > 0, err
}

func (b *Bot) leaveWrite(s *models.BotSession) error {
	if s.Step == stepMenu && s.PeerUserID == nil {
		return nil
	}
	s.Step = stepMenu
	s.PeerUserID = nil
	return b.saveSession(s)
}

func (b *Bot) cancelWrite(s *models.BotSession) error {
	if err := b.leaveWrite(s); err != nil {
		return err
	}
	return b.send(s.ExternalID, "Сообщение не отправлено.", menuKeyboard())
}

func (b *Bot) matchGone(s *models.BotSession) error {
	if err := b.leaveWrite(s); err != nil {
		return err
	}
	return b.send(s.ExternalID, "Этот диалог сейчас недоступен. Написать можно только при взаимной симпатии.", [][]Button{
		{CallbackButton("Мои мэтчи", "matches:0"), CallbackButton("В меню", "menu:home")},
	})
}
