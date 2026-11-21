package maxbot

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"studak/internal/crud"
	"studak/internal/models"
	"testing"
)

type adminTestMessenger struct {
	text    string
	buttons [][]Button
}

func (m *adminTestMessenger) SendContext(_ context.Context, _ int64, text string, buttons [][]Button) error {
	m.text = text
	m.buttons = buttons
	return nil
}
func (m *adminTestMessenger) SendPhotoContext(ctx context.Context, id int64, text string, buttons [][]Button, _ map[string]string) error {
	return m.SendContext(ctx, id, text, buttons)
}
func (m *adminTestMessenger) AnswerCallbackContext(context.Context, string, string) error { return nil }

func TestAdminAuthorizationConfirmationAndDraftPreservation(t *testing.T) {
	t.Setenv("ADMIN_TELEGRAM_IDS", "700")
	t.Setenv("ADMIN_MAX_IDS", "900")
	db, err := testDB(filepath.Join(t.TempDir(), "admin.db"))
	if err != nil {
		t.Fatal(err)
	}
	messenger := &adminTestMessenger{}
	bot := &Bot{DB: db, Channel: "telegram", Messenger: messenger}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	admin, err := bot.session(700)
	must(err)
	admin.Step = stepAge
	admin.DraftName = "Черновик"
	must(bot.saveSession(admin))
	id := int64(800)
	target, err := crud.CreateUser(db, crud.CreateUserInput{Name: "Студент", Age: 20, TelegramID: &id})
	must(err)
	must(bot.onMessage(701, Body{Text: "/admin"}))
	if !strings.Contains(messenger.text, "Нет доступа") {
		t.Fatal("unconfigured administrator allowed")
	}
	must(bot.onCallback(900, fmt.Sprintf("admin:ban:%d", target.ID)))
	if !strings.Contains(messenger.text, "Нет доступа") {
		t.Fatal("MAX admin ID gained Telegram privileges")
	}
	must(bot.onMessage(700, Body{Text: "/id"}))
	if !strings.Contains(messenger.text, "700") {
		t.Fatal("ID command did not show authenticated sender")
	}
	must(bot.onMessage(700, Body{Text: "/admin"}))
	must(bot.onCallback(700, fmt.Sprintf("admin:ban:%d", target.ID)))
	target, err = crud.GetUser(db, target.ID)
	must(err)
	if target.Banned {
		t.Fatal("ban applied before confirmation")
	}
	pending, err := bot.session(700)
	must(err)
	nonce := pending.AdminToken
	if len(nonce) != 32 {
		t.Fatal("missing one-time confirmation")
	}
	must(bot.onCallback(701, "admin:confirm:"+nonce))
	target, err = crud.GetUser(db, target.ID)
	must(err)
	if target.Banned {
		t.Fatal("non-admin used an admin confirmation")
	}
	must(bot.onCallback(700, "admin:confirm:forged"))
	must(bot.onCallback(700, "admin:confirm:"+nonce))
	target, err = crud.GetUser(db, target.ID)
	must(err)
	if !target.Banned {
		t.Fatal("confirmed ban was not applied")
	}
	must(crud.SetUserBanned(db, target.ID, false))
	must(bot.onCallback(700, "admin:confirm:"+nonce))
	target, err = crud.GetUser(db, target.ID)
	must(err)
	if target.Banned {
		t.Fatal("consumed confirmation was replayed")
	}
	current, err := bot.session(700)
	must(err)
	if current.Step != stepAge || current.DraftName != "Черновик" {
		t.Fatal("admin actions overwrote registration")
	}
	must(db.Model(&models.ServiceState{}).Where("id = ?", 1).Update("started", false).Error)
	must(bot.onCallback(700, "admin:launch:0"))
	started, err := crud.DatingStarted(db)
	must(err)
	if started {
		t.Fatal("launched before confirmation")
	}
	pending, err = bot.session(700)
	must(err)
	must(bot.onCallback(700, "admin:confirm:"+pending.AdminToken))
	started, err = crud.DatingStarted(db)
	must(err)
	if !started {
		t.Fatal("confirmed launch did not persist")
	}
	adminID := int64(700)
	owner, err := crud.CreateUser(db, crud.CreateUserInput{Name: "Администратор", Age: 21, TelegramID: &adminID})
	must(err)
	must(bot.onCallback(700, fmt.Sprintf("admin:ban:%d", owner.ID)))
	if !strings.Contains(messenger.text, "нельзя заблокировать") {
		t.Fatal("administrator is not protected from a ban")
	}
	// MAX uses its own administrator list even when IDs overlap.
	bot.Channel = "max"
	must(bot.onMessage(900, Body{Text: "/admin"}))
	if !strings.Contains(messenger.text, "Админка") {
		t.Fatal("MAX administrator denied")
	}
}

func TestWaitingRoomRegistrationBanAndUniversityButtonCounts(t *testing.T) {
	t.Setenv("ADMIN_TELEGRAM_IDS", "")
	t.Setenv("ADMIN_MAX_IDS", "")
	db, err := testDB(filepath.Join(t.TempDir(), "waiting.db"))
	if err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(db.Model(&models.ServiceState{}).Where("id = ?", 1).Update("started", false).Error)
	messenger := &adminTestMessenger{}
	bot := &Bot{DB: db, Channel: "telegram", Messenger: messenger}
	must(bot.onMessage(50, Body{Text: "/start"}))
	must(bot.onMessage(50, Body{Text: "Анна"}))
	state, err := bot.session(50)
	must(err)
	if state.Step != stepAge {
		t.Fatal("registration closed before launch")
	}
	must(bot.onCallback(50, "menu:feed"))
	if !strings.Contains(messenger.text, "готовится к запуску") {
		t.Fatal("feed opened before launch")
	}
	state, err = bot.session(50)
	must(err)
	if state.Step != stepAge {
		t.Fatal("waiting room discarded registration")
	}
	uni := models.University{Name: "Тестовый университет", ShortName: "ТУ"}
	must(db.Create(&uni).Error)
	native := int64(60)
	u, err := crud.CreateUser(db, crud.CreateUserInput{Name: "Борис", Age: 21, TelegramID: &native, MyUniversityID: &uni.ID})
	must(err)
	state.Step = stepUniversity
	must(bot.saveSession(state))
	must(bot.offerUniversities(state, "ТУ"))
	found := false
	for _, row := range messenger.buttons {
		for _, button := range row {
			if button.Payload == fmt.Sprintf("uni:%d", uni.ID) && strings.Contains(button.Text, "1 чел.") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("university choice lacks registration count")
	}
	must(crud.SetUserBanned(db, u.ID, true))
	for _, action := range []string{"menu:profile", "privacy:delete", "privacy:resume", "menu:feed"} {
		must(bot.onCallback(native, action))
		if !strings.Contains(messenger.text, "заблокирован администратором") {
			t.Fatalf("banned actor used %s", action)
		}
	}
	must(bot.onMessage(native, Body{Text: "/id"}))
	if !strings.Contains(messenger.text, "60") {
		t.Fatal("banned actor cannot determine own ID")
	}
}
