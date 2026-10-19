package maxbot

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"studak/internal/crud"
	"studak/internal/database"
	"studak/internal/models"
)

func TestParseUpdates(t *testing.T) {
	single := []byte(`{"update_type":"message_created","message":{"sender":{"user_id":9},"body":{"text":"привет"}}}`)
	got := ParseUpdates(single)
	maxID, err := got[0].Message.Sender.MaxID()
	if err != nil || len(got) != 1 || got[0].Message.Body.Text != "привет" || maxID != 9 {
		t.Fatalf("single: %+v", got)
	}

	arr := ParseUpdates([]byte(`[{"update_type":"bot_started","user":{"user_id":1,"name":"A"}}]`))
	if len(arr) != 1 || arr[0].UpdateType != "bot_started" {
		t.Fatalf("array: %+v", arr)
	}

	wrap := ParseUpdates([]byte(`{"updates":[{"update_type":"message_callback","callback":{"payload":"like:3","user":{"user_id":2}}}]}`))
	if len(wrap) != 1 || wrap[0].Callback.Payload != "like:3" {
		t.Fatalf("wrap: %+v", wrap)
	}
}

func TestRejectsMissingMaxUserID(t *testing.T) {
	db, err := testDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	bot := New(db, "test-token", "secret")
	err = bot.HandleUpdate(Update{UpdateType: "bot_started", User: &User{Name: "Без ID"}})
	if err == nil || !strings.Contains(err.Error(), "user_id") {
		t.Fatalf("expected missing user_id error, got %v", err)
	}
	var count int64
	if err := db.Model(&models.BotSession{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("invalid update created %d sessions", count)
	}
}

func TestMaxDatingFlow(t *testing.T) {
	db, err := testDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SeedUniversities(db); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var sent []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		sent = append(sent, r.URL.Path+"?"+r.URL.RawQuery+" "+string(raw))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	t.Cleanup(api.Close)

	bot := New(db, "test-token", "sekret")
	bot.Client.BaseURL = api.URL
	bot.Client.HTTP = api.Client()

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	must(bot.HandleUpdate(Update{UpdateType: "bot_started", User: &User{UserID: 1001, Name: "Анна"}}))
	must(bot.HandleUpdate(textUpdate(1001, "Анна")))
	must(bot.HandleUpdate(textUpdate(1001, "20")))
	must(bot.HandleUpdate(Update{
		UpdateType: "message_callback",
		Callback:   &Callback{CallbackID: "cb1", Payload: "gender:female", User: User{UserID: 1001}},
	}))
	must(bot.HandleUpdate(textUpdate(1001, "МГУ")))

	var msu models.University
	if err := db.Where("short_name = ?", "МГУ").First(&msu).Error; err != nil {
		t.Fatal(err)
	}
	must(bot.HandleUpdate(Update{
		UpdateType: "message_callback",
		Callback:   &Callback{CallbackID: "cb2", Payload: fmt.Sprintf("uni:%d", msu.ID), User: User{UserID: 1001}},
	}))
	must(bot.HandleUpdate(Update{
		UpdateType: "message_callback",
		Callback:   &Callback{CallbackID: "cb3", Payload: "mode:all", User: User{UserID: 1001}},
	}))
	must(bot.HandleUpdate(textUpdate(1001, "ищу одногруппников")))

	anna, err := crud.GetUserByMaxID(db, 1001)
	if err != nil {
		t.Fatal(err)
	}
	if anna.Name != "Анна" || anna.Age != 20 || anna.Gender != "female" || anna.MyUniversityID == nil {
		t.Fatalf("saved profile: %+v", anna)
	}

	changedName := "Анна из API"
	if _, err := crud.UpdateUser(db, anna.ID, crud.UpdateUserInput{Name: &changedName}); err != nil {
		t.Fatal(err)
	}
	must(bot.HandleUpdate(Update{
		UpdateType: "message_callback",
		Callback:   &Callback{CallbackID: "filter1", Payload: "menu:filter", User: User{UserID: 1001}},
	}))
	must(bot.HandleUpdate(Update{
		UpdateType: "message_callback",
		Callback:   &Callback{CallbackID: "filter2", Payload: "mode:my_only", User: User{UserID: 1001}},
	}))
	anna, err = crud.GetUserByMaxID(db, 1001)
	if err != nil {
		t.Fatal(err)
	}
	if anna.Name != changedName || anna.SearchMode != models.SearchModeMyOnly {
		t.Fatalf("filter update overwrote profile: %+v", anna)
	}

	other, err := crud.CreateUser(db, crud.CreateUserInput{
		Name: "Борис", Age: 21, Gender: "male", Bio: "МФТИ",
		MyUniversityID: &msu.ID, SearchMode: models.SearchModeAll,
	})
	if err != nil {
		t.Fatal(err)
	}

	must(bot.HandleUpdate(Update{
		UpdateType: "message_callback",
		Callback:   &Callback{CallbackID: "cb4", Payload: "menu:feed", User: User{UserID: 1001}},
	}))
	must(bot.HandleUpdate(Update{
		UpdateType: "message_callback",
		Callback:   &Callback{CallbackID: "cb5", Payload: fmt.Sprintf("like:%d", other.ID), User: User{UserID: 1001}},
	}))

	like, err := crud.SetLike(db, other.ID, anna.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !like.IsMatch {
		t.Fatal("expected match after reverse like")
	}

	mu.Lock()
	blob := strings.Join(sent, "\n")
	mu.Unlock()
	if !strings.Contains(blob, `"text":"Как тебя зовут?`) && !strings.Contains(blob, "Как тебя зовут") {
		t.Fatalf("expected name prompt in %s", blob)
	}
	if !strings.Contains(blob, "/messages?user_id=1001") {
		t.Fatalf("expected send to user_id=1001, got %s", blob)
	}
	if !strings.Contains(blob, "/answers?callback_id=cb1") {
		t.Fatalf("expected callback answer, got %s", blob)
	}
}

func TestWebhookSecretAndHTTP200(t *testing.T) {
	db, err := testDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	t.Cleanup(api.Close)

	bot := New(db, "tok", "sekret")
	bot.Client.BaseURL = api.URL
	bot.Client.HTTP = api.Client()

	req := httptest.NewRequest(http.MethodPost, "/webhook/max", bytes.NewReader([]byte(`{"update_type":"bot_started","user":{"user_id":7,"name":"X"}}`)))
	rec := httptest.NewRecorder()
	bot.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing secret: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/webhook/max", bytes.NewReader([]byte(`{"update_type":"bot_started","user":{"user_id":7,"name":"X"}}`)))
	req.Header.Set("X-Max-Bot-Api-Secret", "sekret")
	rec = httptest.NewRecorder()
	bot.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid secret: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/webhook/max", bytes.NewReader([]byte(`{not json`)))
	req.Header.Set("X-Max-Bot-Api-Secret", "sekret")
	rec = httptest.NewRecorder()
	bot.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid json should be rejected, got %d", rec.Code)
	}
}

func TestIgnoresGroupChats(t *testing.T) {
	db, err := testDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	var hits int
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	t.Cleanup(api.Close)
	bot := New(db, "tok", "")
	bot.Client.BaseURL = api.URL
	bot.Client.HTTP = api.Client()

	err = bot.HandleUpdate(Update{
		UpdateType: "message_created",
		Message: &Message{
			Sender:    User{UserID: 5, Name: "G"},
			Recipient: Recipient{ChatType: "chat", ChatID: 99},
			Body:      Body{Text: "привет"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if hits != 0 {
		t.Fatalf("group message should be ignored, sends=%d", hits)
	}
}

func textUpdate(maxID int64, text string) Update {
	return Update{
		UpdateType: "message_created",
		Message: &Message{
			Sender:    User{UserID: maxID},
			Recipient: Recipient{ChatType: "dialog", UserID: maxID},
			Body:      Body{Text: text},
		},
	}
}

func testDB(path string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path+"?_pragma=foreign_keys(1)"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&models.University{}, &models.User{}, &models.Like{}, &models.BotSession{}); err != nil {
		return nil, err
	}
	return db, nil
}
