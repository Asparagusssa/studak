package maxbot

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"studak/internal/crud"
	"studak/internal/models"
)

func TestProfilePrivacyInMax(t *testing.T) {
	db, err := testDB(filepath.Join(t.TempDir(), "privacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	create := func(name, gender, search string, id int64) *models.User {
		t.Helper()
		user, err := crud.CreateUser(db, crud.CreateUserInput{MaxContact: "+79991234567", Name: name, Age: 22, Gender: gender, SearchGender: search, MaxID: &id})
		must(err)
		return user
	}
	a := create("Алекс", "male", "all", 101)
	b := create("Мила", "female", "male", 202)
	c := create("Варя", "female", "all", 303)
	like := func(from, to uint) {
		t.Helper()
		_, err := crud.SetLike(db, from, to, true)
		must(err)
	}
	like(a.ID, b.ID)
	like(a.ID, c.ID)
	like(c.ID, a.ID)

	var mu sync.Mutex
	var sentToA []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if r.URL.Path == "/messages" && r.URL.Query().Get("user_id") == "101" {
			mu.Lock()
			sentToA = append(sentToA, string(raw))
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	t.Cleanup(api.Close)
	bot := New(db, "fake-token", "fake-secret")
	bot.Client.BaseURL, bot.Client.HTTP = api.URL, api.Client()
	click := func(id int64, payload string) {
		t.Helper()
		must(bot.onCallback(id, payload))
	}
	message := func(id int64, mid, text string) {
		t.Helper()
		update := textUpdate(id, text)
		update.Message.Body.Mid = mid
		must(bot.HandleUpdate(update))
	}
	sent := func(part string) bool {
		mu.Lock()
		defer mu.Unlock()
		return strings.Contains(strings.Join(sentToA, "\n"), part)
	}

	must(bot.onStart(101, ""))
	if !sent("privacy:home") {
		t.Fatal("main menu has no settings button")
	}
	click(101, "privacy:home")
	click(101, "privacy:pause")
	if !sent("privacy:resume") {
		t.Fatal("no resume action from paused settings")
	}
	if _, err := crud.GetNextProfile(db, b.ID); !errors.Is(err, crud.ErrNoProfiles) {
		t.Fatalf("paused profile visible in feed: %v", err)
	}
	if _, err := crud.NextIncomingLike(db, b.ID); !errors.Is(err, crud.ErrNoProfiles) {
		t.Fatalf("paused profile visible in incoming likes: %v", err)
	}
	if _, err := crud.GetNextProfile(db, a.ID); !errors.Is(err, crud.ErrProfilePaused) {
		t.Fatalf("paused viewer can browse: %v", err)
	}
	if _, err := crud.SetLike(db, b.ID, a.ID, true); !errors.Is(err, crud.ErrNotFound) {
		t.Fatalf("stale card can like paused profile: %v", err)
	}
	click(303, fmt.Sprintf("write:%d", a.ID))
	message(303, "under-pause", "Старый мэтч всё ещё открыт")
	if sent("Старый мэтч всё ещё открыт") {
		t.Fatal("bot still relays messages during a pause")
	}
	click(101, "privacy:resume")
	target, err := crud.GetNextProfile(db, b.ID)
	if err != nil || target.ID != a.ID {
		t.Fatalf("profile did not return to feed: %+v %v", target, err)
	}

	like(b.ID, a.ID)
	click(202, fmt.Sprintf("write:%d", a.ID))
	click(101, fmt.Sprintf("more:%d", b.ID))
	click(101, fmt.Sprintf("privacy:block:%d", b.ID))
	message(202, "after-block", "Этот текст должен быть скрыт")
	if sent("Этот текст должен быть скрыт") {
		t.Fatal("blocked sender reached an already open conversation")
	}
	if _, err := crud.MatchedUser(db, a.ID, b.ID); !errors.Is(err, crud.ErrNotFound) {
		t.Fatalf("blocked match still visible: %v", err)
	}
	if _, err := crud.GetNextProfile(db, b.ID); !errors.Is(err, crud.ErrNoProfiles) {
		t.Fatalf("blocker visible to blocked user: %v", err)
	}
	if match, err := crud.GetNextProfile(db, a.ID); err == nil && match.ID == b.ID {
		t.Fatal("blocked user visible to blocker")
	}
	if _, err := crud.ReactOnce(db, b.ID, a.ID, true, false); !errors.Is(err, crud.ErrNotFound) {
		t.Fatalf("blocked user can react from a stale card: %v", err)
	}
	click(101, "privacy:blocked:0")
	if !sent(fmt.Sprintf("privacy:unblock:%d", b.ID)) {
		t.Fatal("blocker cannot unblock via settings")
	}
	click(101, fmt.Sprintf("privacy:unblock:%d", b.ID))
	if blocked, err := crud.IsPairBlocked(db, a.ID, b.ID); err != nil || blocked {
		t.Fatalf("did not remove own block: blocked=%v, err=%v", blocked, err)
	}
	if _, err := crud.MatchedUser(db, b.ID, a.ID); !errors.Is(err, crud.ErrNotFound) {
		t.Fatalf("unblocking restored an old match: %v", err)
	}
	click(101, fmt.Sprintf("more:%d", b.ID))
	click(101, fmt.Sprintf("privacy:block:%d", b.ID))

	readConfirmation := func() string {
		t.Helper()
		s, err := bot.session(101)
		must(err)
		if !strings.HasPrefix(s.Step, deleteStepPrefix) {
			t.Fatalf("missing deletion confirmation: %q", s.Step)
		}
		return "privacy:confirm_delete:" + strings.TrimPrefix(s.Step, deleteStepPrefix)
	}
	click(101, "privacy:delete")
	old := readConfirmation()
	click(101, "privacy:home")
	click(101, "privacy:delete")
	fresh := readConfirmation()
	click(101, old)
	if _, err := crud.GetUser(db, a.ID); err != nil {
		t.Fatalf("stale confirmation deleted a profile: %v", err)
	}
	click(101, fresh)
	if _, err := crud.GetUser(db, a.ID); !errors.Is(err, crud.ErrNotFound) {
		t.Fatalf("confirmed profile still exists: %v", err)
	}
	for _, item := range []struct {
		model any
		query string
		args  []any
	}{
		{&models.BotSession{}, "channel = ? AND external_id = ?", []any{"max", 101}},
		{&models.Like{}, "from_user_id = ? OR to_user_id = ?", []any{a.ID, a.ID}},
		{&models.UserBlock{}, "blocker_id = ? OR blocked_id = ?", []any{a.ID, a.ID}},
	} {
		var count int64
		must(db.Model(item.model).Where(item.query, item.args...).Count(&count).Error)
		if count != 0 {
			t.Fatalf("deletion left %d rows in %T", count, item.model)
		}
	}
	if _, err := crud.GetUser(db, c.ID); err != nil {
		t.Fatalf("deletion affected someone else's profile: %v", err)
	}
}
