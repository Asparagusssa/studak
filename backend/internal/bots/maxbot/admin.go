package maxbot

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"studak/internal/crud"
	"studak/internal/models"
)

func configuredAdmin(channel string, id int64) bool {
	if id <= 0 {
		return false
	}
	key := "ADMIN_MAX_IDS"
	if channel == "telegram" {
		key = "ADMIN_TELEGRAM_IDS"
	}
	for _, part := range strings.Split(os.Getenv(key), ",") {
		parsed, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err == nil && parsed > 0 && parsed == id {
			return true
		}
	}
	return false
}
func (b *Bot) isAdmin(id int64) bool { return configuredAdmin(b.channel(), id) }
func protectedAdmin(u *models.User) bool {
	return (u.TelegramID != nil && configuredAdmin("telegram", *u.TelegramID)) || (u.MaxID != nil && configuredAdmin("max", *u.MaxID))
}

func (b *Bot) accessBlocked(id int64) (bool, error) {
	user, err := b.lookupUser(id)
	if errors.Is(err, crud.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !user.Banned {
		return false, nil
	}
	return true, b.send(id, "Доступ к Студаку заблокирован администратором.", nil)
}
func (b *Bot) datingBlocked(id int64) (bool, error) {
	started, err := crud.DatingStarted(b.DB)
	if err != nil {
		return true, err
	}
	if started {
		return false, nil
	}
	return true, b.send(id, "Студак готовится к запуску 🎓\nРегистрация уже открыта. Пока собираем анкеты, знакомства недоступны. Свою анкету можно заполнить или изменить уже сейчас.", menuKeyboard())
}

func (b *Bot) adminHome(id int64, intro string) error {
	if !b.isAdmin(id) {
		return b.send(id, "Нет доступа к админке. Твой ID можно узнать командой /id.", nil)
	}
	stats, err := crud.RegisteredUsers(b.DB)
	if err != nil {
		return err
	}
	started, err := crud.DatingStarted(b.DB)
	if err != nil {
		return err
	}
	status := "Сбор анкет. Знакомства ещё не запущены."
	if started {
		status = "Знакомства запущены в Telegram и MAX."
	}
	text := fmt.Sprintf("Админка\n%s\n\nВсего зарегистрировано: %d\nTelegram: %d\nMAX: %d\nДругие: %d\nЗаблокировано: %d", status, stats.Total, stats.Telegram, stats.Max, stats.Web, stats.Banned)
	rows := [][]Button{{CallbackButton("Все пользователи", "admin:users:0")}, {CallbackButton("Сбросить пропуски у всех", "admin:reset:0")}}
	if !started {
		rows = append(rows, []Button{CallbackButton("Запустить знакомства", "admin:launch:0")})
	}
	rows = append(rows, []Button{CallbackButton("Обновить", "admin:home"), CallbackButton("В меню", "menu:home")})
	return b.send(id, withIntro(intro, text), rows)
}

func (b *Bot) adminUsers(id int64, offset int) error {
	const page = 8
	users, err := crud.AdminUsers(b.DB, offset, page+1)
	if err != nil {
		return err
	}
	if len(users) == 0 && offset > 0 {
		return b.adminUsers(id, 0)
	}
	var rows [][]Button
	for _, u := range users[:min(page, len(users))] {
		mark := ""
		if u.Banned {
			mark = "⛔ "
		}
		name := []rune(strings.Join(strings.Fields(u.Name), " "))
		if len(name) > 20 {
			name = append(name[:19], '…')
		}
		rows = append(rows, []Button{CallbackButton(fmt.Sprintf("%s#%d %s · %s", mark, u.ID, string(name), u.Platform()), fmt.Sprintf("admin:user:%d", u.ID))})
	}
	var pages []Button
	if offset > 0 {
		pages = append(pages, CallbackButton("← Назад", fmt.Sprintf("admin:users:%d", max(0, offset-page))))
	}
	if len(users) > page {
		pages = append(pages, CallbackButton("Дальше →", fmt.Sprintf("admin:users:%d", offset+page)))
	}
	if len(pages) > 0 {
		rows = append(rows, pages)
	}
	rows = append(rows, []Button{CallbackButton("В админку", "admin:home")})
	return b.send(id, "Пользователи Telegram и MAX\nВыбери пользователя для управления анкетой.", rows)
}

func (b *Bot) adminUser(id int64, target uint, intro string) error {
	user, err := crud.GetUser(b.DB, target)
	if errors.Is(err, crud.ErrNotFound) {
		return b.adminHome(id, "Пользователь уже удалён.")
	}
	if err != nil {
		return err
	}
	status := "Активен"
	if user.Paused {
		status = "На паузе"
	}
	if user.Banned {
		status = "Заблокирован администратором"
	}
	text := fmt.Sprintf("#%d · %s\n%s\nСтатус: %s", user.ID, user.Platform(), formatProfile(user), status)
	action, label := "ban", "Заблокировать"
	if user.Banned {
		action, label = "unban", "Разблокировать"
	}
	rows := [][]Button{{CallbackButton(label, fmt.Sprintf("admin:%s:%d", action, target))}, {CallbackButton("Сбросить пропуски", fmt.Sprintf("admin:reset:%d", target))}, {CallbackButton("К списку", "admin:users:0"), CallbackButton("В админку", "admin:home")}}
	return b.send(id, withIntro(intro, text), rows)
}

func (b *Bot) onAdmin(s *models.BotSession, raw string) error {
	// Every callback is authorized independently, including confirmations and old buttons.
	if !b.isAdmin(s.ExternalID) {
		return b.send(s.ExternalID, "Нет доступа к админке.", nil)
	}
	action, arg, _ := strings.Cut(raw, ":")
	switch action {
	case "home":
		return b.adminHome(s.ExternalID, "")
	case "users":
		offset, err := strconv.Atoi(arg)
		if err != nil || offset < 0 || offset > 1000000 {
			offset = 0
		}
		return b.adminUsers(s.ExternalID, offset)
	case "user":
		target, ok := userIDArg(arg)
		if !ok {
			return b.adminHome(s.ExternalID, "")
		}
		return b.adminUser(s.ExternalID, target, "")
	case "confirm":
		return b.confirmAdmin(s, arg)
	case "ban", "unban", "reset", "launch":
		n, err := strconv.ParseUint(arg, 10, 32)
		if err != nil {
			return b.adminHome(s.ExternalID, "")
		}
		target := uint(n)
		if (action == "ban" || action == "unban") && target == 0 {
			return b.adminHome(s.ExternalID, "")
		}
		if action == "launch" && target != 0 {
			return b.adminHome(s.ExternalID, "")
		}
		if target != 0 {
			u, err := crud.GetUser(b.DB, target)
			if err != nil {
				return b.adminHome(s.ExternalID, "Пользователь недоступен.")
			}
			if action == "ban" && protectedAdmin(u) {
				return b.adminUser(s.ExternalID, target, "Администратора нельзя заблокировать.")
			}
		}
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return err
		}
		token := hex.EncodeToString(random[:])
		if err := b.DB.Model(s).Updates(map[string]any{"admin_token": token, "admin_action": action, "admin_target": target}).Error; err != nil {
			return err
		}
		text := "Запустить знакомства сразу в Telegram и MAX? Регистрация останется открытой."
		switch action {
		case "ban":
			text = fmt.Sprintf("Заблокировать пользователя #%d? Он не сможет пользоваться ботом, а его анкета исчезнет из выдачи. Анкета сохранится.", target)
		case "unban":
			text = fmt.Sprintf("Снять блокировку пользователя #%d?", target)
		case "reset":
			scope := fmt.Sprintf("у пользователя #%d", target)
			if target == 0 {
				scope = "у всех пользователей"
			}
			text = "Сбросить пропуски " + scope + "? Пропущенные анкеты снова смогут попасть в ленту. Лайки и блокировки сохранятся."
		}
		return b.send(s.ExternalID, text, [][]Button{{CallbackButton("Подтвердить", "admin:confirm:"+token), CallbackButton("Отмена", "admin:cancel")}})
	case "cancel":
		if err := b.DB.Model(s).Updates(map[string]any{"admin_token": "", "admin_action": "", "admin_target": 0}).Error; err != nil {
			return err
		}
		return b.adminHome(s.ExternalID, "Действие отменено.")
	default:
		return b.adminHome(s.ExternalID, "")
	}
}

func (b *Bot) confirmAdmin(s *models.BotSession, token string) error {
	if token == "" {
		return b.adminHome(s.ExternalID, "Подтверждение устарело.")
	}
	var changed int64
	var action string
	err := b.DB.Transaction(func(tx *gorm.DB) error {
		var pending models.BotSession
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND admin_token = ?", s.ID, token).First(&pending).Error; err != nil {
			return crud.ErrNotFound
		}
		action = pending.AdminAction
		if action == "ban" {
			user, err := crud.GetUser(tx, pending.AdminTarget)
			if err != nil {
				return err
			}
			if protectedAdmin(user) {
				return crud.ErrInvalidInput
			}
		}
		switch action {
		case "ban", "unban":
			if err := crud.SetUserBanned(tx, pending.AdminTarget, action == "ban"); err != nil {
				return err
			}
		case "reset":
			var err error
			changed, err = crud.ResetSkips(tx, pending.AdminTarget)
			if err != nil {
				return err
			}
		case "launch":
			if err := crud.StartDating(tx); err != nil {
				return err
			}
		default:
			return crud.ErrNotFound
		}
		return tx.Model(&pending).Updates(map[string]any{"admin_token": "", "admin_action": "", "admin_target": 0}).Error
	})
	if errors.Is(err, crud.ErrNotFound) || errors.Is(err, crud.ErrInvalidInput) {
		return b.adminHome(s.ExternalID, "Подтверждение устарело или действие недоступно.")
	}
	if err != nil {
		return err
	}
	text := "Готово."
	if action == "reset" {
		text = fmt.Sprintf("Удалено пропусков: %d. Лайки и блокировки сохранены.", changed)
	}
	if action == "launch" {
		text = "Знакомства запущены в Telegram и MAX 🎓"
	}
	return b.adminHome(s.ExternalID, text)
}
