package maxbot

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"studak/internal/crud"
	"studak/internal/models"
)

const (
	deleteStepPrefix = "delete:"
	stepBlock        = "block"
)

func blockButton(id uint) Button {
	return CallbackButton("Заблокировать", fmt.Sprintf("block:%d", id))
}

func (b *Bot) handlePrivacy(s *models.BotSession, payload string) error {
	action, arg, _ := strings.Cut(payload, ":")
	user, err := b.appUser(s)
	if errors.Is(err, crud.ErrNotFound) {
		return b.onStart(s.ExternalID, "")
	}
	if err != nil {
		return err
	}
	switch action {
	case "home":
		if err := b.leaveWrite(s); err != nil {
			return err
		}
		return b.sendPrivacy(s, user, "")
	case "pause", "resume":
		if err := b.leaveWrite(s); err != nil {
			return err
		}
		paused := action == "pause"
		if err := crud.SetProfilePaused(b.DB, user.ID, paused); err != nil {
			return err
		}
		user.Paused = paused
		intro := "Анкета снова в поиске ✨"
		if paused {
			intro = "Анкета на паузе 🌙"
		}
		return b.sendPrivacy(s, user, intro)
	case "delete":
		var nonce [8]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return err
		}
		s.Step = deleteStepPrefix + hex.EncodeToString(nonce[:])
		s.PeerUserID = nil
		if err := b.saveSession(s); err != nil {
			return err
		}
		return b.sendDeleteConfirmation(s)
	case "confirm_delete":
		if arg == "" || s.Step != deleteStepPrefix+arg {
			return b.resume(s, staleButtonText)
		}
		err := crud.DeleteMessengerProfile(b.DB, user.ID, s.ExternalID, b.channel(), s.Step)
		if errors.Is(err, crud.ErrNotFound) {
			return b.send(s.ExternalID, "Подтверждение устарело. Если анкета ещё есть, открой её настройки заново.", nil)
		}
		if err != nil {
			return err
		}
		return b.send(s.ExternalID, "Анкета удалена. Если захочешь вернуться, нажми «Создать анкету».", [][]Button{
			{CallbackButton("Создать анкету", "menu:home")},
		})
	case "block":
		peerID, ok := userIDArg(arg)
		if !ok || s.Step != stepBlock || s.PeerUserID == nil || *s.PeerUserID != peerID {
			return b.resume(s, staleButtonText)
		}
		if err := b.leaveWrite(s); err != nil {
			return err
		}
		err := crud.BlockUser(b.DB, user.ID, peerID)
		if errors.Is(err, crud.ErrNotFound) {
			return b.send(s.ExternalID, "Эта анкета уже недоступна.", menuKeyboard())
		}
		if err != nil {
			return err
		}
		return b.send(s.ExternalID, "Пользователь заблокирован. Вы больше не увидите анкеты друг друга в Студаке. Уже полученный контакт останется у собеседника.", [][]Button{
			{CallbackButton("Заблокированные", "privacy:blocked:0"), CallbackButton("В меню", "menu:home")},
		})
	case "blocked":
		offset, err := strconv.Atoi(arg)
		if err != nil || offset < 0 || offset > 10000 {
			offset = 0
		}
		return b.showBlocked(s, user.ID, offset, "")
	case "unblock":
		peerID, ok := userIDArg(arg)
		if !ok {
			return b.showBlocked(s, user.ID, 0, "")
		}
		if err := crud.UnblockUser(b.DB, user.ID, peerID); err != nil {
			return err
		}
		return b.showBlocked(s, user.ID, 0, "Блокировка снята. Прежний мэтч не восстановится.")
	default:
		return b.resume(s, staleButtonText)
	}
}

func (b *Bot) sendPrivacy(s *models.BotSession, user *models.User, intro string) error {
	action := "Поставить на паузу"
	target := "privacy:pause"
	status := "Анкета видна в поиске. На паузе новые знакомства остановятся, а существующие мэтчи и контакты останутся доступны."
	if user.Paused {
		action, target = "Вернуть в поиск", "privacy:resume"
		status = "Анкета скрыта из поиска. Существующие мэтчи и контакты доступны."
	}
	title := "Настройки"
	if intro != "" {
		title = intro
	}
	return b.send(s.ExternalID, withIntro(title, status), [][]Button{
		{CallbackButton(action, target)},
		{CallbackButton("Заблокированные", "privacy:blocked:0")},
		{CallbackButton("Удалить анкету", "privacy:delete"), CallbackButton("В меню", "menu:home")},
	})
}

func (b *Bot) pausedMenu(maxID int64) error {
	return b.send(maxID, "Анкета на паузе 🌙\nВерни её в поиск, чтобы снова смотреть новые анкеты и отвечать на лайки.", [][]Button{
		{CallbackButton("Вернуть в поиск", "privacy:resume")},
		{CallbackButton("Мои мэтчи", "matches:0"), CallbackButton("Настройки анкеты", "privacy:home")},
	})
}

func (b *Bot) sendDeleteConfirmation(s *models.BotSession) error {
	challenge := strings.TrimPrefix(s.Step, deleteStepPrefix)
	return b.send(s.ExternalID, "Удалить анкету?\n\nФото, лайки и мэтчи будут удалены из «Студака». Это нельзя отменить. Уже полученные другими пользователями контакты не удаляются.\n\nЕсли нужен перерыв, можно поставить анкету на паузу.", [][]Button{
		{CallbackButton("Оставить анкету", "privacy:home"), CallbackButton("Удалить навсегда", "privacy:confirm_delete:"+challenge)},
	})
}

func (b *Bot) askBlock(s *models.BotSession, raw string) error {
	user, err := b.appUser(s)
	if errors.Is(err, crud.ErrNotFound) {
		return b.onStart(s.ExternalID, "")
	}
	if err != nil {
		return err
	}
	id, ok := userIDArg(raw)
	if !ok || id == user.ID {
		return b.send(s.ExternalID, "Эта анкета недоступна.", menuKeyboard())
	}
	blocked, err := crud.IsPairBlocked(b.DB, user.ID, id)
	if err != nil {
		return err
	}
	if blocked {
		return b.send(s.ExternalID, "Эта анкета сейчас недоступна.", menuKeyboard())
	}
	peer, err := crud.GetUser(b.DB, id)
	if errors.Is(err, crud.ErrNotFound) {
		return b.send(s.ExternalID, "Эта анкета уже недоступна.", menuKeyboard())
	}
	if err != nil {
		return err
	}
	s.Step, s.PeerUserID = stepBlock, &peer.ID
	if err := b.saveSession(s); err != nil {
		return err
	}
	return b.send(s.ExternalID, fmt.Sprintf("%s, %d\n\nПри блокировке вы перестанете видеть анкеты друг друга в Студаке. Если был мэтч, он исчезнет. Уже полученный контакт останется у собеседника. Снять блокировку можно в настройках.", peer.Name, peer.Age), [][]Button{
		{CallbackButton("Заблокировать", fmt.Sprintf("privacy:block:%d", peer.ID)), CallbackButton("Отмена", "menu:home")},
	})
}

func (b *Bot) showBlocked(s *models.BotSession, owner uint, offset int, intro string) error {
	const pageSize = 8
	if err := b.leaveWrite(s); err != nil {
		return err
	}
	users, err := crud.ListBlockedUsers(b.DB, owner, offset, pageSize+1)
	if err != nil {
		return err
	}
	if len(users) == 0 && offset > 0 {
		return b.showBlocked(s, owner, 0, intro)
	}
	text := "Заблокированные\nНажми «Снять» рядом с именем, чтобы разблокировать человека."
	rows := make([][]Button, 0, pageSize+2)
	for _, user := range users[:min(len(users), pageSize)] {
		name := []rune(strings.Join(strings.Fields(user.Name), " "))
		if len(name) > 24 {
			name = append(name[:23], '…')
		}
		rows = append(rows, []Button{CallbackButton("Снять · "+string(name), fmt.Sprintf("privacy:unblock:%d", user.ID))})
	}
	var pages []Button
	if offset > 0 {
		pages = append(pages, CallbackButton("← Назад", fmt.Sprintf("privacy:blocked:%d", max(0, offset-pageSize))))
	}
	if len(users) > pageSize {
		pages = append(pages, CallbackButton("Дальше →", fmt.Sprintf("privacy:blocked:%d", offset+pageSize)))
	}
	if len(pages) > 0 {
		rows = append(rows, pages)
	}
	if len(users) == 0 {
		text = "Заблокированных пользователей нет."
	}
	return b.send(s.ExternalID, withIntro(intro, text), append(rows, []Button{CallbackButton("К настройкам", "privacy:home")}))
}
