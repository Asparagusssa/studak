package maxbot

import (
	"errors"
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

func (b *Bot) contactDetails(peer *models.User) (text, label, link string) {
	hasMax := peer.MaxID != nil && *peer.MaxID > 0 && peer.MaxProfileURL != "" && models.IsValidMaxProfileURL(peer.MaxProfileURL)
	if b.channel() == channelMax && hasMax {
		return "Профиль MAX: " + peer.MaxProfileURL, "Открыть MAX", peer.MaxProfileURL
	}
	if peer.TelegramID != nil && *peer.TelegramID > 0 {
		if peer.TelegramUsername != "" && models.IsValidTelegramUsername(peer.TelegramUsername) {
			return "Telegram: @" + peer.TelegramUsername, "Открыть Telegram", "https://t.me/" + peer.TelegramUsername
		}
		if b.channel() == "telegram" {
			return "Username в Telegram не указан. Можно открыть профиль по кнопке ниже.", "Открыть Telegram", "tg://user?id=" + strconv.FormatInt(*peer.TelegramID, 10)
		}
	}
	if hasMax {
		return "Профиль MAX: " + peer.MaxProfileURL, "Открыть MAX", peer.MaxProfileURL
	}
	if peer.MaxID != nil {
		return "Пользователь пока не добавил личную ссылку MAX.", "", ""
	}
	return "Контакт пока недоступен: пользователь не указал username или ссылку на профиль.", "", ""
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
			return b.updateMaxContact(s, "")
		}
	}
	return b.resume(s, staleButtonText)
}

func (b *Bot) askMaxContact(s *models.BotSession, intro string) error {
	return b.send(s.ExternalID, withIntro(intro, "Пришли личную ссылку на свой профиль MAX вида https://max.ru/u/…\n\nСкопируй её в профиле MAX через «Поделиться». Студак покажет ссылку только после взаимного лайка. При замене ссылки ранее полученные ссылки у других пользователей сохранятся."), [][]Button{
		{CallbackButton("Убрать ссылку", "contact:remove"), CallbackButton("Назад к анкете", "menu:profile")},
	})
}

func (b *Bot) saveMaxContact(s *models.BotSession, body Body) error {
	link := strings.TrimSpace(body.Text)
	if b.channel() != channelMax || len(body.Attachments) != 0 || link == "" || !models.IsValidMaxProfileURL(link) {
		return b.askMaxContact(s, "Нужна личная HTTPS-ссылка MAX, а не ссылка на канал или группу.")
	}
	return b.updateMaxContact(s, link)
}

func (b *Bot) updateMaxContact(s *models.BotSession, link string) error {
	user, err := b.appUser(s)
	if err != nil {
		return err
	}
	user, err = crud.UpdateUser(b.DB, user.ID, crud.UpdateUserInput{MaxProfileURL: &link})
	if err != nil {
		return err
	}
	s.Step, s.PeerUserID = stepMenu, nil
	if err := b.saveSession(s); err != nil {
		return err
	}
	intro := "Контакт MAX сохранён. Ссылка будет доступна только после взаимного лайка."
	if link == "" {
		intro = "Контакт MAX убран из Студака."
	}
	return b.sendOwn(s, user, intro)
}
