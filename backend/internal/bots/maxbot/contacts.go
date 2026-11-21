package maxbot

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"studak/internal/crud"
	"studak/internal/models"
)

// rememberTelegramUsername runs after update deduplication and refreshes the
// authenticated actor's own contact, including removal of their username.
func (b *Bot) rememberTelegramUsername(update Update) error {
	if b.channel() != "telegram" {
		return nil
	}
	var actor *User
	switch update.UpdateType {
	case "message_created":
		if update.Message != nil {
			actor = &update.Message.Sender
		}
	case "message_callback":
		if update.Callback != nil {
			actor = &update.Callback.User
		}
	case "bot_started":
		actor = update.User
	}
	if actor == nil || actor.UserID <= 0 || actor.IsBot {
		return nil
	}
	username := strings.TrimSpace(actor.Username)
	if !models.IsValidTelegramUsername(username) {
		return nil
	}
	session, err := b.session(actor.UserID)
	if err != nil {
		return err
	}
	if session.MessengerUsername != username {
		session.MessengerUsername = username
		if err := b.saveSession(session); err != nil {
			return err
		}
	}
	return b.DB.Model(&models.User{}).
		Where("telegram_id = ? AND telegram_username <> ?", actor.UserID, username).
		Update("telegram_username", username).Error
}

func (b *Bot) onboardingPrompt(step int, text string) string {
	total := 8
	if b.channel() == channelMax {
		total = 9
	}
	return fmt.Sprintf("%d/%d · %s", step, total, text)
}

func (b *Bot) contactDetails(peer *models.User) (text, label, link string) {
	if peer.Platform() != b.channel() {
		return "Контакт этой анкеты недоступен.", "", ""
	}
	if b.channel() == channelMax {
		contact, valid := models.NormalizeMaxContact(peer.MaxContact)
		if !valid {
			return "Пользователь пока не указал контакт для связи.", "", ""
		}
		if strings.HasPrefix(contact, "+") {
			return "Телефон: " + contact, "", ""
		}
		if strings.HasPrefix(contact, "https://t.me/") {
			return "Telegram: " + contact, "Открыть Telegram", contact
		}
		return "Профиль MAX: " + contact, "Открыть MAX", contact
	}
	if peer.TelegramUsername != "" && models.IsValidTelegramUsername(peer.TelegramUsername) {
		return "Telegram: @" + peer.TelegramUsername, "Открыть Telegram", "https://t.me/" + peer.TelegramUsername
	}
	return "Username в Telegram не указан. Можно открыть профиль по кнопке ниже.", "Открыть Telegram", "tg://user?id=" + strconv.FormatInt(*peer.TelegramID, 10)
}

func (b *Bot) beginRegistrationContact(s *models.BotSession, intro string) error {
	if b.channel() != channelMax {
		return b.beginPhoto(s, stepPhoto)
	}
	s.Step, s.PeerUserID = stepRegistrationContact, nil
	if err := b.saveSession(s); err != nil {
		return err
	}
	return b.askMaxContact(s, intro)
}

func (b *Bot) handleContact(s *models.BotSession, action string) error {
	if b.channel() != channelMax {
		return b.showOwn(s)
	}
	if _, err := b.appUser(s); err != nil {
		if errors.Is(err, crud.ErrNotFound) {
			return b.onStart(s.ExternalID, "")
		}
		return err
	}
	switch action {
	case "edit":
		s.Step, s.PeerUserID = stepContact, nil
		if err := b.saveSession(s); err != nil {
			return err
		}
		return b.askMaxContact(s, "")
	case "remove":
		if s.Step == stepContact {
			return b.askMaxContact(s, "Контакт обязателен. Его можно заменить, но нельзя убрать.")
		}
	}
	return b.resume(s, staleButtonText)
}

func (b *Bot) askMaxContact(s *models.BotSession, intro string) error {
	text := "Укажи контакт для связи: номер с кодом страны, ссылку на Telegram или личную ссылку MAX.\n\nНапример: +79991234567, https://t.me/username или https://max.ru/u/…\n\nКонтакт обязателен и будет доступен собеседнику только после взаимного лайка."
	var rows [][]Button
	if s.Step == stepRegistrationContact {
		text = b.onboardingPrompt(8, text)
	} else {
		rows = [][]Button{{CallbackButton("Назад к анкете", "menu:profile")}}
	}
	return b.send(s.ExternalID, withIntro(intro, text), rows)
}

func (b *Bot) saveMaxContact(s *models.BotSession, body Body) error {
	contact, valid := models.NormalizeMaxContact(body.Text)
	if b.channel() != channelMax || len(body.Attachments) != 0 || !valid {
		return b.askMaxContact(s, "Не получилось распознать контакт. Пришли один номер телефона или личную ссылку на Telegram/MAX.")
	}
	if s.Step == stepRegistrationContact {
		s.DraftContact = contact
		return b.beginPhoto(s, stepPhoto)
	}
	return b.updateMaxContact(s, contact)
}

func (b *Bot) updateMaxContact(s *models.BotSession, contact string) error {
	contact, valid := models.NormalizeMaxContact(contact)
	if !valid {
		return b.askMaxContact(s, "Контакт обязателен. Его можно заменить, но нельзя убрать.")
	}
	user, err := b.appUser(s)
	if err != nil {
		return err
	}
	user, err = crud.UpdateUser(b.DB, user.ID, crud.UpdateUserInput{MaxContact: &contact})
	if err != nil {
		return err
	}
	s.Step, s.PeerUserID = stepMenu, nil
	if err := b.saveSession(s); err != nil {
		return err
	}
	return b.sendOwn(s, user, "Контакт сохранён. Он будет доступен только после взаимного лайка.")
}
