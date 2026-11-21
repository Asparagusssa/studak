package maxbot

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"studak/internal/crud"
)

func TestMaxContactInputAndDisclosure(t *testing.T) {
	db, err := testDB(filepath.Join(t.TempDir(), "contacts.db"))
	if err != nil {
		t.Fatal(err)
	}
	first, second := int64(501), int64(502)
	me, err := crud.CreateUser(db, crud.CreateUserInput{Name: "Анна", Age: 21, MaxID: &first})
	if err != nil {
		t.Fatal(err)
	}
	peer, err := crud.CreateUser(db, crud.CreateUserInput{Name: "Борис", Age: 21, MaxID: &second})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	sent := map[string]string{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if r.URL.Path == "/messages" {
			mu.Lock()
			sent[r.URL.Query().Get("user_id")] = string(raw)
			mu.Unlock()
		}
		_, _ = io.WriteString(w, `{"success":true}`)
	}))
	defer api.Close()
	bot := New(db, "fake-token", "fake-secret")
	bot.Client.BaseURL, bot.Client.HTTP = api.URL, api.Client()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	last := func(id string) string { mu.Lock(); defer mu.Unlock(); return sent[id] }
	must(bot.onCallback(first, "contact:edit"))
	for _, link := range []string{"https://max.ru.evil.example/u/abc", "https://max.ru/join/group", "https://max.ru/u/abc?x=y", "http://max.ru/u/abc"} {
		must(bot.onMessage(first, Body{Text: link}))
		user, err := crud.GetUserByMaxID(db, first)
		must(err)
		if user.MaxProfileURL != "" {
			t.Fatalf("invalid link saved: %q", link)
		}
	}
	link := "https://max.ru/u/aliceOpaqueToken"
	must(bot.onMessage(first, Body{Text: link}))
	user, err := crud.GetUserByMaxID(db, first)
	must(err)
	if user.MaxProfileURL != link {
		t.Fatal("MAX contact was not saved")
	}
	public, err := json.Marshal(user)
	must(err)
	if strings.Contains(string(public), link) || strings.Contains(string(public), "max_profile_url") {
		t.Fatal("public user JSON leaks private MAX contact")
	}
	must(bot.onCallback(second, fmt.Sprintf("write:%d", me.ID)))
	if strings.Contains(last("502"), link) {
		t.Fatal("unmatched actor received MAX contact")
	}
	must(bot.onCallback(first, fmt.Sprintf("like:%d", peer.ID)))
	if strings.Contains(last("502"), link) {
		t.Fatal("incoming notification reveals contact")
	}
	must(bot.onCallback(second, fmt.Sprintf("like_back:%d", me.ID)))
	if !strings.Contains(last("502"), link) || !strings.Contains(last("502"), `"type":"link"`) {
		t.Fatal("matched actor did not receive direct MAX profile link")
	}
	must(bot.onCallback(first, "contact:edit"))
	must(bot.onCallback(first, "contact:remove"))
	must(bot.onCallback(second, fmt.Sprintf("match:%d", me.ID)))
	if strings.Contains(last("502"), link) || !strings.Contains(last("502"), "пока не добавил") {
		t.Fatal("removed MAX contact is still supplied to future requests")
	}
}
