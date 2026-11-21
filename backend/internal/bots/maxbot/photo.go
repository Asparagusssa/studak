package maxbot

import (
	"errors"
	"strings"

	"studak/internal/crud"
	"studak/internal/logx"
	"studak/internal/models"
)

func (b *Bot) beginPhoto(s *models.BotSession, step string) error {
	s.Step = step
	if err := b.saveSession(s); err != nil {
		return err
	}
	return b.askPhoto(s, "")
}

func (b *Bot) askPhoto(s *models.BotSession, intro string) error {
	if s.Step == stepEditPhoto {
		return b.send(s.ExternalID, withIntro(intro, "Пришли одно новое фото для анкеты. Другие участники смогут его увидеть в боте. Фото можно заменить, но нельзя удалить."), [][]Button{
			{CallbackButton("Назад к анкете", "menu:profile")},
		})
	}
	return b.send(s.ExternalID, withIntro(intro, "8/8 · Добавь фото для анкеты. Пришли одно изображение через галерею — его увидят другие участники в боте. Фото обязательно для публикации."), nil)
}

func (b *Bot) updatePhoto(s *models.BotSession, token string) error {
	if strings.TrimSpace(token) == "" {
		return b.askPhoto(s, "Фото обязательно: его можно заменить, но нельзя удалить.")
	}
	user, err := b.appUser(s)
	if err != nil {
		return err
	}
	user, err = crud.UpdateUser(b.DB, user.ID, b.photoUpdate(&token))
	if err != nil {
		return err
	}
	s.Step = stepMenu
	if err := b.saveSession(s); err != nil {
		return err
	}
	return b.sendOwn(s, user, "Фото обновлено ✨")
}

func (b *Bot) sendOwn(s *models.BotSession, user *models.User, intro string) error {
	if user.Paused {
		intro += "\nАнкета на паузе"
	}
	rows := [][]Button{
		{CallbackButton("📷 Фото", "photo:edit"), CallbackButton("Изменить анкету", "menu:reset")},
	}
	if b.channel() == channelMax {
		rows = append(rows, []Button{CallbackButton("Контакт MAX", "contact:edit")})
		if user.MaxProfileURL != "" {
			intro += "\nТвой контакт MAX: " + user.MaxProfileURL
		}
	} else if user.TelegramUsername != "" {
		intro += "\nТвой контакт Telegram: @" + user.TelegramUsername
	} else {
		intro += "\nUsername Telegram не указан: после мэтча будет доступна ссылка на профиль."
	}
	rows = append(rows, []Button{CallbackButton("В меню", "menu:home")})
	return b.sendProfile(s.ExternalID, user, intro, rows)
}

func (b *Bot) sendProfile(recipient int64, user *models.User, intro string, keyboard [][]Button) error {
	text := withIntro(intro, formatProfile(user))
	photo := b.photoPayload(user)
	if len(photo) == 0 {
		return b.send(recipient, text, keyboard)
	}

	if err := b.transport().SendPhotoContext(b.context(), recipient, text, keyboard, photo); err != nil {
		logx.Error("MAX profile photo send failed", err)
		var failure PhotoError
		if errors.As(err, &failure) && failure.PhotoUnavailable() {
			return b.send(recipient, text+"\n\nФото пока недоступно.", keyboard)
		}
		return err
	}
	return nil
}
