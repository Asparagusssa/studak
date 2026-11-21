package maxbot

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"studak/internal/crud"
)

func TestMaxRegistrationRequiresContactBeforePhotoAndPublication(t *testing.T) {
	for _, contact := range []string{"+79991234567", "https://t.me/anna_test", "https://max.ru/u/annaOpaque"} {
		t.Run(contact, func(t *testing.T) {
			db, err := testDB(filepath.Join(t.TempDir(), "register.db"))
			if err != nil {
				t.Fatal(err)
			}
			var last string
			var mu sync.Mutex
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				mu.Lock()
				last = string(raw)
				mu.Unlock()
				_, _ = io.WriteString(w, `{"success":true}`)
			}))
			defer api.Close()
			lastBody := func() string { mu.Lock(); defer mu.Unlock(); return last }
			bot := New(db, "fake", "secret")
			bot.Client.BaseURL, bot.Client.HTTP = api.URL, api.Client()
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			s, err := bot.session(601)
			must(err)
			s.Step, s.DraftName, s.DraftAge, s.DraftGender, s.DraftSearchMode = stepBio, "Анна", 21, "female", "all"
			must(bot.saveSession(s))
			must(bot.onMessage(601, Body{Text: "Люблю прогулки"}))
			s, err = bot.session(601)
			must(err)
			if s.Step != stepRegistrationContact || !strings.Contains(lastBody(), "8/9") {
				t.Fatal("registration did not ask for contact")
			}
			must(bot.onMessage(601, Body{Text: "пропустить"}))
			token := "photo-before-contact"
			must(bot.finishOnboarding(s, &token))
			if _, err := crud.GetUserByMaxID(db, 601); err != crud.ErrNotFound {
				t.Fatal("published a profile without a contact")
			}
			must(bot.onMessage(601, Body{Text: contact}))
			s, err = bot.session(601)
			must(err)
			if s.Step != stepPhoto || s.DraftContact != contact || !strings.Contains(lastBody(), "9/9") {
				t.Fatalf("contact did not advance to photo: %+v", s)
			}
			if _, err := crud.GetUserByMaxID(db, 601); err != crud.ErrNotFound {
				t.Fatal("contact submission published a profile without a photo")
			}
			must(bot.HandleBody(photoUpdate(601, "anna-photo")))
			user, err := crud.GetUserByMaxID(db, 601)
			must(err)
			if user.MaxContact != contact || user.MaxPhotoToken != "anna-photo" || user.Platform() != "max" {
				t.Fatalf("bad published profile: %+v", user)
			}
		})
	}
}
