package maxbot

import (
	"errors"

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
		return b.send(s.ExternalID, withIntro(intro, "Пришли одно фото для анкеты. Другие участники смогут его увидеть в боте."), [][]Button{
			{CallbackButton("Убрать фото", "photo:remove"), CallbackButton("Назад к анкете", "menu:profile")},
		})
	}
	return b.send(s.ExternalID, withIntro(intro, "8/8 · Добавим фото? Пришли одно изображение — его увидят другие участники в боте. Этот шаг можно пропустить."), [][]Button{
		{CallbackButton("Пропустить", "photo:skip")},
	})
}

func (b *Bot) updatePhoto(s *models.BotSession, token string) error {
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
	message := "Фото обновлено ✨"
	if token == "" {
		message = "Фото убрано из анкеты."
	}
	return b.sendOwn(s, user, message)
}

func (b *Bot) sendOwn(s *models.BotSession, user *models.User, intro string) error {
	if user.Paused {
		intro += "\nАнкета на паузе"
	}
	return b.sendProfile(s.ExternalID, user, intro, [][]Button{
		{CallbackButton("📷 Фото", "photo:edit"), CallbackButton("Изменить анкету", "menu:reset")},
		{CallbackButton("В меню", "menu:home")},
	})
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
