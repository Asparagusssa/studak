package maxbot

import "studak/internal/models"

// beginWrite handles old buttons without restoring deleted match history.
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
	return b.send(s.ExternalID, "Контакт приходит сообщением о взаимной симпатии. Список мэтчей больше не хранится.", menuKeyboard())
}

func (b *Bot) matchGone(s *models.BotSession) error {
	if err := b.leaveWrite(s); err != nil {
		return err
	}
	return b.send(s.ExternalID, "Этот мэтч сейчас недоступен. Контакт можно получить только при взаимной симпатии.", [][]Button{
		{CallbackButton("В меню", "menu:home")},
	})
}
