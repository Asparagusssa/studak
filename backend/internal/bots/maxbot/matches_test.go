package maxbot

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"studak/internal/crud"
	"studak/internal/models"
)

func TestIncomingLikesAndDirectContacts(t *testing.T) {
	db, err := testDB(filepath.Join(t.TempDir(), "match.db"))
	if err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	create := func(name string, maxID int64) *models.User {
		t.Helper()
		user, err := crud.CreateUser(db, crud.CreateUserInput{Name: name, Age: 21, MaxID: &maxID, MaxContact: fmt.Sprintf("https://max.ru/u/user%d", maxID)})
		must(err)
		return user
	}
	anna := create("Анна", 101)
	boris := create("Борис", 202)
	other := create("Дарья", 303)

	type sentMessage struct {
		recipient string
		text      string
		buttons   []string
	}
	var mu sync.Mutex
	var sent []sentMessage
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text        string `json:"text"`
			Attachments []struct {
				Payload struct {
					Buttons [][]Button `json:"buttons"`
				} `json:"payload"`
			} `json:"attachments"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode MAX request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.URL.Path == "/messages" {
			msg := sentMessage{recipient: r.URL.Query().Get("user_id"), text: body.Text}
			for _, attachment := range body.Attachments {
				for _, row := range attachment.Payload.Buttons {
					for _, btn := range row {
						msg.buttons = append(msg.buttons, btn.Payload, btn.URL)
					}
				}
			}
			mu.Lock()
			sent = append(sent, msg)
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	t.Cleanup(api.Close)
	bot := New(db, "fake-token", "fake-secret")
	bot.Client.BaseURL = api.URL
	bot.Client.HTTP = api.Client()

	countText := func(recipient, part string) int {
		mu.Lock()
		defer mu.Unlock()
		n := 0
		for _, msg := range sent {
			if msg.recipient == recipient && strings.Contains(msg.text, part) {
				n++
			}
		}
		return n
	}
	hasButton := func(recipient, payload string) bool {
		mu.Lock()
		defer mu.Unlock()
		for _, msg := range sent {
			for _, btn := range msg.buttons {
				if msg.recipient == recipient && btn == payload {
					return true
				}
			}
		}
		return false
	}
	click := func(uid int64, verb string, id uint) {
		t.Helper()
		must(bot.onCallback(uid, fmt.Sprintf("%s:%d", verb, id)))
	}
	message := func(uid int64, mid, text string) {
		t.Helper()
		event := textUpdate(uid, text)
		event.Message.Body.Mid = mid
		must(bot.HandleUpdate(event))
	}

	click(202, "write", anna.ID)
	message(202, "before-match", "Этого никто не должен получить")
	if countText("101", "Этого никто не должен получить") != 0 {
		t.Fatal("sent a message before a match")
	}

	click(101, "like", boris.ID)
	click(101, "like", boris.ID)
	if countText("202", "У тебя новый лайк") != 1 || !hasButton("202", "menu:incoming") {
		t.Fatal("the recipient should receive one incoming-like notification")
	}
	if _, err := crud.ReactOnce(db, other.ID, boris.ID, false, false); err != nil {
		t.Fatal(err)
	}
	must(bot.onCallback(202, "menu:incoming"))
	if !hasButton("202", fmt.Sprintf("like_back:%d", anna.ID)) || hasButton("202", fmt.Sprintf("like_back:%d", other.ID)) {
		t.Fatal("inbox should show positive likes only")
	}
	click(202, "like_back", anna.ID)
	click(202, "like_back", anna.ID)
	click(101, "skip", boris.ID)
	if got := []int{countText("101", "Симпатия взаимна"), countText("202", "Симпатия взаимна")}; !reflect.DeepEqual(got, []int{1, 1}) {
		t.Fatalf("expected one match notice per recipient, got %v", got)
	}
	must(bot.onCallback(202, "matches:0"))
	if !hasButton("202", fmt.Sprintf("match:%d", anna.ID)) || !hasButton("202", "https://max.ru/u/user101") {
		t.Fatal("match and direct-contact actions are missing")
	}

	click(202, "write", anna.ID)
	message(202, "matched-hi", "Привет! Ты в каком корпусе?")
	click(202, "write", anna.ID)
	message(202, "matched-hi", "Привет! Ты в каком корпусе?")
	if countText("101", "Ты в каком корпусе?") != 0 || !hasButton("202", "https://max.ru/u/user101") {
		t.Fatal("legacy write button relayed text instead of showing the direct contact")
	}

	click(101, "write", boris.ID)
	message(101, "matched-reply", "В главном кампусе")
	if countText("202", "В главном кампусе") != 0 {
		t.Fatal("bot still forwards match replies")
	}

	click(202, "write", anna.ID)
	must(bot.onCallback(202, "cancel_write:"))
	message(202, "cancelled", "Отменённое сообщение не уйдёт")
	if countText("101", "Отменённое сообщение не уйдёт") != 0 {
		t.Fatal("sent a cancelled message")
	}
	click(202, "write", anna.ID)
	_, err = crud.SetLike(db, anna.ID, boris.ID, false)
	must(err)
	message(202, "after-unmatch", "Сообщение после отмены мэтча")
	if countText("101", "Сообщение после отмены мэтча") != 0 {
		t.Fatal("sent a message after the match stopped being mutual")
	}

	click(303, "like", anna.ID)
	must(bot.onCallback(101, "menu:incoming"))
	click(101, "skip_in", other.ID)
	incoming, err := crud.NextIncomingLike(db, anna.ID)
	if err != crud.ErrNoProfiles || incoming != nil {
		t.Fatalf("skipped like reappeared: %+v / %v", incoming, err)
	}
	if _, err := crud.MatchedUser(db, other.ID, anna.ID); err != crud.ErrNotFound {
		t.Fatalf("skipping a pending like created a match: %v", err)
	}
}
