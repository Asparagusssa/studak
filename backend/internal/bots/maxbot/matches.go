package maxbot

import (
	"errors"
	"fmt"
	"strconv"

	"studak/internal/crud"
	"studak/internal/logx"
	"studak/internal/models"
)

func (b *Bot) showIncoming(s *models.BotSession) error {
	if blocked, err := b.datingBlocked(s.ExternalID); blocked || err != nil {
		return err
	}
	user, err := b.appUser(s)
	if errors.Is(err, crud.ErrNotFound) {
		return b.onStart(s.ExternalID, "")
	}
	if err != nil {
		return err
	}
	if err := b.leaveWrite(s); err != nil {
		return err
	}

	from, err := crud.NextIncomingLike(b.DB, user.ID)
	if errors.Is(err, crud.ErrContactRequired) {
		return b.handleContact(s, "edit")
	}
	if errors.Is(err, crud.ErrProfilePaused) {
		return b.pausedMenu(s.ExternalID)
	}
	if errors.Is(err, crud.ErrNoProfiles) {
		return b.send(s.ExternalID, "Новых лайков пока нет. Здесь появятся анкеты тех, кому ты понравишься 💌", [][]Button{
			{CallbackButton("Смотреть анкеты", "menu:feed"), CallbackButton("В меню", "menu:home")},
		})
	}
	if err != nil {
		return err
	}
	return b.sendProfile(s.ExternalID, from, "💌 Ты понравился этому человеку", [][]Button{
		{CallbackButton("❤️", fmt.Sprintf("like_back:%d", from.ID)), CallbackButton("👎", fmt.Sprintf("skip_in:%d", from.ID))},
		{blockButton(from.ID), CallbackButton("В меню", "menu:home")},
	})
}

func (b *Bot) react(s *models.BotSession, rawID string, like, inbox bool) error {
	if blocked, err := b.datingBlocked(s.ExternalID); blocked || err != nil {
		return err
	}
	next := b.showNext
	if inbox {
		next = b.showIncoming
	}
	me, err := b.appUser(s)
	if errors.Is(err, crud.ErrNotFound) {
		return b.onStart(s.ExternalID, "")
	}
	if err != nil {
		return err
	}
	peerID, ok := userIDArg(rawID)
	if !ok || peerID == me.ID {
		return next(s)
	}
	// Old feed buttons must not leave the bot drafting a message.
	if err := b.leaveWrite(s); err != nil {
		return err
	}
	res, err := crud.ReactOnce(b.DB, me.ID, peerID, like, inbox)
	if errors.Is(err, crud.ErrContactRequired) {
		return b.handleContact(s, "edit")
	}
	if errors.Is(err, crud.ErrProfilePaused) {
		return b.pausedMenu(s.ExternalID)
	}
	if errors.Is(err, crud.ErrNotFound) || errors.Is(err, crud.ErrAlreadyExists) {
		return next(s)
	}
	if err != nil {
		return err
	}
	if !res.NewLike {
		return next(s)
	}

	peer, err := crud.GetUser(b.DB, peerID)
	if errors.Is(err, crud.ErrNotFound) {
		return next(s)
	}
	if err != nil {
		return err
	}
	if res.NewMatch {
		if err := b.deliverPendingMatch(me.ID, peer.ID); err != nil {
			logx.Error("mutuality delivery pending", err)
		}
		return nil
	}

	if b.canNotify(peer) {
		_ = b.notify(peer, "У тебя новый лайк 💌\nПосмотри анкету и реши, взаимно ли это.", [][]Button{
			{CallbackButton("Посмотреть лайк", "menu:incoming")},
		})
	}
	return next(s)
}

func (b *Bot) sendMatch(viewer int64, peer *models.User) error {
	return b.send(viewer, withIntro("Симпатия взаимна! 💛", b.matchText(peer)), b.matchKeyboard(peer))
}

func (b *Bot) matchText(peer *models.User) string {
	text := fmt.Sprintf("Твой мэтч — %s, %d.", peer.Name, peer.Age)
	contact, _, _ := b.contactDetails(peer)
	return text + "\n\n" + contact
}

func (b *Bot) matchKeyboard(peer *models.User) [][]Button {
	var rows [][]Button
	if _, label, link := b.contactDetails(peer); link != "" {
		rows = append(rows, []Button{{Type: "link", Text: label, URL: link}})
	}
	return append(rows, []Button{blockButton(peer.ID), CallbackButton("В меню", "menu:home")})
}

// Old history buttons explain where to find contacts without recreating a match.
func (b *Bot) showMatches(s *models.BotSession, _ int) error {
	if blocked, err := b.datingBlocked(s.ExternalID); blocked || err != nil {
		return err
	}
	if err := b.leaveWrite(s); err != nil {
		return err
	}
	return b.send(s.ExternalID, "Мэтчи больше не сохраняются. Контакт остаётся только в сообщении о взаимной симпатии в этой переписке.", menuKeyboard())
}
func (b *Bot) showMatch(s *models.BotSession, _ string) error { return b.showMatches(s, 0) }

func userIDArg(value string) (uint, bool) {
	id, err := strconv.ParseUint(value, 10, 32)
	return uint(id), err == nil && id > 0
}
