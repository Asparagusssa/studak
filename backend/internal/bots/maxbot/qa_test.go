package maxbot

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"studak/internal/crud"
	"studak/internal/models"
)

func TestWebhookReplayAndParallelOnboarding(t *testing.T) {
	db, err := testDB(filepath.Join(t.TempDir(), "replay.db"))
	if err != nil {
		t.Fatal(err)
	}
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"success":true}`)
	}))
	t.Cleanup(mock.Close)
	b := New(db, "fake", "test-secret")
	b.Client.BaseURL, b.Client.HTTP = mock.URL, mock.Client()
	if err := b.HandleUpdate(Update{UpdateType: "bot_started", User: &User{UserID: 400}}); err != nil {
		t.Fatal(err)
	}

	event := textUpdate(400, "Лена")
	event.Message.Body.Mid = "same-message"
	const copies = 8
	var wg sync.WaitGroup
	errs := make(chan error, copies)
	for i := 0; i < copies; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- b.HandleUpdate(event) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	session, err := b.session(400)
	if err != nil || session.Step != stepAge || session.DraftName != "Лена" {
		t.Fatalf("replay/parallel event changed the draft: %+v, %v", session, err)
	}
	age := textUpdate(400, "20")
	age.Message.Body.Mid = "age-message"
	if err := b.HandleUpdate(age); err != nil {
		t.Fatal(err)
	}
	if err := b.HandleUpdate(event); err != nil {
		t.Fatal(err)
	}
	session, _ = b.session(400)
	if session.Step != stepGender || session.DraftAge != 20 {
		t.Fatalf("old event advanced registration: %+v", session)
	}

	// This old name must also be ignored if a fresh Bot object handles it.
	another := New(db, "fake", "test-secret")
	another.Client = b.Client
	if err := another.HandleUpdate(event); err != nil {
		t.Fatal(err)
	}
	session, _ = b.session(400)
	if session.Step != stepGender {
		t.Fatal("deduplication did not survive a process restart")
	}
}

func TestMAXAPIErrorsAndImageFallback(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"logical failure", 200, `{"success":false,"code":"denied"}`},
		{"invalid json", 200, `not-json`},
		{"server error", 503, `{"code":"temporary.failure"}`},
		{"rate limit", 429, `{"code":"rate.limit"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer mock.Close()
			c := NewClient("private-token")
			c.BaseURL, c.HTTP = mock.URL, mock.Client()
			if err := c.Send(5, "text", nil); err == nil {
				t.Fatal("client reported a failed response as successful")
			}
		})
	}
}

func TestGroupCallbackMustNotChangeProfile(t *testing.T) {
	db, err := testDB(filepath.Join(t.TempDir(), "callback-group.db"))
	if err != nil {
		t.Fatal(err)
	}
	id := int64(81)
	_, err = crud.CreateUser(db, crud.CreateUserInput{Name: "A", Age: 21, MaxID: &id})
	if err != nil {
		t.Fatal(err)
	}
	var sends int
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sends++; _, _ = io.WriteString(w, `{"success":true}`) }))
	defer mock.Close()
	b := New(db, "fake", "secret")
	b.Client.BaseURL, b.Client.HTTP = mock.URL, mock.Client()
	e := Update{UpdateType: "message_callback", Callback: &Callback{CallbackID: "group-callback", Payload: "privacy:pause", User: User{UserID: id}},
		Message: &Message{Recipient: Recipient{ChatType: "chat", ChatID: 45}}}
	if err := b.HandleUpdate(e); err != nil {
		t.Fatal(err)
	}
	user, err := crud.GetUserByMaxID(db, id)
	if err != nil || user.Paused || sends != 0 {
		t.Fatalf("group callback was accepted: paused=%v sends=%d err=%v", user.Paused, sends, err)
	}
}

func TestHTTPBoundaries(t *testing.T) {
	db, err := testDB(filepath.Join(t.TempDir(), "http-boundaries.db"))
	if err != nil {
		t.Fatal(err)
	}
	b := New(db, "fake", "secret")
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"empty", "", 400}, {"invalid", "ÿ", 400}, {"huge", strings.Repeat("x", (1<<20)+1), 400},
		{"batch", "[" + strings.Repeat(`{"update_type":"unknown"},`, 10) + `{"update_type":"unknown"}]`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/webhook/max", bytes.NewBufferString(tc.body))
			req.Header.Set("X-Max-Bot-Api-Secret", "secret")
			rec := httptest.NewRecorder()
			b.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("got %d want %d", rec.Code, tc.status)
			}
		})
	}
}

func TestPhotoFallbackPreservesButtons(t *testing.T) {
	db, err := testDB(filepath.Join(t.TempDir(), "image-fallback.db"))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		calls++
		if calls == 1 {
			if !bytes.Contains(raw, []byte(`"token":"fake-image"`)) {
				t.Error("missing image token")
			}
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"code":"attachment.not.ready"}`)
			return
		}
		if bytes.Contains(raw, []byte(`"type":"image"`)) || !bytes.Contains(raw, []byte("menu:home")) {
			t.Errorf("fallback lost keyboard or retained failed attachment: %s", raw)
		}
		_, _ = io.WriteString(w, `{"success":true}`)
	}))
	defer mock.Close()
	b := New(db, "fake", "secret")
	b.Client.BaseURL, b.Client.HTTP = mock.URL, mock.Client()
	err = b.sendProfile(123, &models.User{Name: "A", Age: 20, MaxPhotoToken: "fake-image"}, "", [][]Button{{CallbackButton("Menu", "menu:home")}})
	if err != nil || calls != 2 {
		t.Fatalf("fallback failed: calls=%d err=%v", calls, err)
	}
}

func TestSingleWideSettingsButton(t *testing.T) {
	rows := menuKeyboard()
	if len(rows) == 0 || len(rows[len(rows)-1]) != 1 || rows[len(rows)-1][0].Payload != "privacy:home" {
		t.Fatalf("settings must be the only button in the bottom row: %v", rows)
	}
}

func TestConcurrentReciprocalLikesAndBlockOnPostgres(t *testing.T) {
	if os.Getenv("QA_POSTGRES") != "1" {
		t.Skip("requires PostgreSQL row locks")
	}
	db, err := testDB(filepath.Join(t.TempDir(), "concurrent-pairs.db"))
	if err != nil {
		t.Fatal(err)
	}
	me, err := crud.CreateUser(db, crud.CreateUserInput{Name: "A", Age: 21})
	if err != nil {
		t.Fatal(err)
	}
	peer, err := crud.CreateUser(db, crud.CreateUserInput{Name: "B", Age: 21})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan *crud.LikeResult, 2)
	errs := make(chan error, 2)
	for _, pair := range [][2]uint{{me.ID, peer.ID}, {peer.ID, me.ID}} {
		pair := pair
		go func() {
			<-start
			result, err := crud.ReactOnce(db, pair[0], pair[1], true, false)
			results <- result
			errs <- err
		}()
	}
	close(start)
	newMatches := 0
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		if (<-results).NewMatch {
			newMatches++
		}
	}
	if newMatches != 1 {
		t.Fatalf("expected one transition to a match, got %d", newMatches)
	}
	if _, err := crud.MatchedUser(db, me.ID, peer.ID); err != nil {
		t.Fatal(err)
	}

	// Blocking and a like on the same pair may arrive together.
	start = make(chan struct{})
	go func() { <-start; errs <- crud.BlockUser(db, me.ID, peer.ID) }()
	go func() { <-start; _, err := crud.SetLike(db, peer.ID, me.ID, true); errs <- err }()
	close(start)
	for range 2 {
		if err := <-errs; err != nil && !errors.Is(err, crud.ErrNotFound) {
			t.Fatal(err)
		}
	}
	if _, err := crud.MatchedUser(db, me.ID, peer.ID); !errors.Is(err, crud.ErrNotFound) {
		t.Fatalf("block did not win: %v", err)
	}
	var count int64
	if err := db.Model(&models.Like{}).Where("from_user_id IN ?", []uint{me.ID, peer.ID}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("reactions survived the block: %d / %v", count, err)
	}
}
