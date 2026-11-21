package maxbot

import "studak/internal/models"

// beginWrite keeps old message buttons working by showing the matched contact.
func (b *Bot) beginWrite(s *models.BotSession, raw string) error {
	return b.showMatch(s, raw)
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
	return b.send(s.ExternalID, "После взаимного лайка открой «Мои мэтчи», чтобы получить контакт и написать напрямую.", menuKeyboard())
}

func (b *Bot) matchGone(s *models.BotSession) error {
	if err := b.leaveWrite(s); err != nil {
		return err
	}
	return b.send(s.ExternalID, "Этот мэтч сейчас недоступен. Контакт можно получить только при взаимной симпатии.", [][]Button{
		{CallbackButton("Мои мэтчи", "matches:0"), CallbackButton("В меню", "menu:home")},
	})
}
