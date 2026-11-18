package maxbot

import (
	"errors"

	"studak/internal/crud"
	"studak/internal/models"
)

func (b *Bot) askWho(s *models.BotSession) error {
	text := "4/8 · Кого показать в ленте? Этот выбор можно поменять позже."
	rows := [][]Button{
		{CallbackButton("Девушек", "who:"+models.SearchGenderFemale), CallbackButton("Парней", "who:"+models.SearchGenderMale)},
		{CallbackButton("Всех", "who:"+models.SearchGenderAll)},
	}
	if s.Step == stepWhoFilter {
		text = "Кого показать в ленте?"
		rows = append(rows, []Button{CallbackButton("В меню", "menu:home")})
	}
	return b.send(s.ExternalID, text, rows)
}

func (b *Bot) beginWhoFilter(s *models.BotSession) error {
	if _, err := b.appUser(s); err != nil {
		if errors.Is(err, crud.ErrNotFound) {
			return b.onStart(s.ExternalID, "")
		}
		return err
	}
	s.Step = stepWhoFilter
	if err := b.saveSession(s); err != nil {
		return err
	}
	return b.askWho(s)
}

func (b *Bot) saveWhoFilter(s *models.BotSession, gender string) error {
	user, err := b.appUser(s)
	if err != nil {
		return err
	}
	if _, err := crud.UpdateUser(b.DB, user.ID, crud.UpdateUserInput{SearchGender: &gender}); err != nil {
		return err
	}
	s.Step = stepMenu
	if err := b.saveSession(s); err != nil {
		return err
	}
	text := "Показываю всех, кто подходит по твоим настройкам вузов."
	switch gender {
	case models.SearchGenderFemale:
		text = "Теперь показываю анкеты девушек."
	case models.SearchGenderMale:
		text = "Теперь показываю анкеты парней."
	}
	return b.send(s.ExternalID, text, menuKeyboard())
}
