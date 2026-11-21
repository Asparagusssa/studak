package maxbot

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"studak/internal/crud"
	"studak/internal/models"
)

func (b *Bot) showIncoming(s *models.BotSession) error {
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
		if b.canNotify(peer) {
			// Delivery may be unavailable if the recipient stopped the bot.
			// The match can still be reopened from the saved list.
			client, id, channel := b.destination(peer)
			_ = b.peerEngine(channel, client).sendMatch(id, me)
		}
		return b.sendMatch(s.ExternalID, peer)
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
	return append(rows, []Button{CallbackButton("Мои мэтчи", "matches:0"), blockButton(peer.ID)})
}

func (b *Bot) showMatches(s *models.BotSession, offset int) error {
	const pageSize = 8
	me, err := b.appUser(s)
	if errors.Is(err, crud.ErrNotFound) {
		return b.onStart(s.ExternalID, "")
	}
	if err != nil {
		return err
	}
	if err := b.leaveWrite(s); err != nil {
		return err
	}
	users, err := crud.ListMatches(b.DB, me.ID, offset, pageSize+1)
	if err != nil {
		return err
	}
	if len(users) == 0 && offset > 0 {
		return b.showMatches(s, 0)
	}
	text := "Мои мэтчи 💛\nВыбери анкету, чтобы получить контакт и познакомиться напрямую."
	rows := make([][]Button, 0, pageSize+2)
	for _, person := range users[:min(len(users), pageSize)] {
		label := []rune(strings.Join(strings.Fields(fmt.Sprintf("%s, %d", person.Name, person.Age)), " "))
		if len(label) > 32 {
			label = append(label[:31], '…')
		}
		rows = append(rows, []Button{CallbackButton(string(label), fmt.Sprintf("match:%d", person.ID))})
	}
	var pages []Button
	if offset > 0 {
		pages = append(pages, CallbackButton("← Назад", fmt.Sprintf("matches:%d", max(0, offset-pageSize))))
	}
	if len(users) > pageSize {
		pages = append(pages, CallbackButton("Дальше →", fmt.Sprintf("matches:%d", offset+pageSize)))
	}
	if len(pages) > 0 {
		rows = append(rows, pages)
	}
	if len(users) == 0 {
		text = "Здесь появятся взаимные симпатии 💛"
		rows = append(rows, []Button{CallbackButton("Входящие лайки", "menu:incoming")})
	}
	return b.send(s.ExternalID, text, append(rows, []Button{CallbackButton("В меню", "menu:home")}))
}

func (b *Bot) showMatch(s *models.BotSession, raw string) error {
	_, peer, err := b.findMatch(s, raw)
	if errors.Is(err, crud.ErrNotFound) {
		return b.matchGone(s)
	}
	if err != nil {
		return err
	}
	if err := b.leaveWrite(s); err != nil {
		return err
	}
	// MatchedUser intentionally retrieves just the user; reload the university for the card.
	peer, err = crud.GetUser(b.DB, peer.ID)
	if errors.Is(err, crud.ErrNotFound) {
		return b.matchGone(s)
	}
	if err != nil {
		return err
	}
	return b.sendProfile(s.ExternalID, peer, b.matchText(peer), b.matchKeyboard(peer))
}

func (b *Bot) findMatch(s *models.BotSession, raw string) (*models.User, *models.User, error) {
	id, ok := userIDArg(raw)
	if !ok {
		return nil, nil, crud.ErrNotFound
	}
	me, err := b.appUser(s)
	if err != nil {
		return nil, nil, err
	}
	peer, err := crud.MatchedUser(b.DB, me.ID, id)
	return me, peer, err
}

func userIDArg(value string) (uint, bool) {
	id, err := strconv.ParseUint(value, 10, strconv.IntSize)
	return uint(id), err == nil && id > 0
}
