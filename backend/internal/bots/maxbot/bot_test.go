package maxbot

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
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
	must(bot.HandleUpdate(textUpdate(1001, "/start")))
	must(bot.HandleUpdate(textUpdate(1001, "20")))
	must(bot.HandleUpdate(Update{
		UpdateType: "message_callback",
		Callback:   &Callback{CallbackID: "cb1", Payload: "gender:female", User: User{UserID: 1001}},
	}))
	must(bot.HandleUpdate(Update{
		UpdateType: "message_callback",
		Callback:   &Callback{CallbackID: "cb-who", Payload: "who:male", User: User{UserID: 1001}},
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
	must(bot.HandleBody([]byte(`{"update_type":"message_created","message":{
		"sender":{"user_id":1001},"recipient":{"chat_type":"dialog"},"body":{"attachments":[
			{"type":"image","payload":{"token":"first-image"}},
			{"type":"image","payload":{"token":"second-image"}}
		]}}}`)))
	if _, err := crud.GetUserByMaxID(db, 1001); err != crud.ErrNotFound {
		t.Fatalf("sending two photos must keep the draft open; got %v", err)
	}
	must(bot.HandleBody(photoUpdate(1001, "anna-photo")))
	must(bot.onCallback(1001, "menu:profile"))

	anna, err := crud.GetUserByMaxID(db, 1001)
	if err != nil {
		t.Fatal(err)
	}
	if anna.Name != "Анна" || anna.Age != 20 || anna.Gender != "female" || anna.MyUniversityID == nil ||
		anna.Bio != "ищу одногруппников" || anna.MaxPhotoToken != "anna-photo" || anna.PhotoURL != "" ||
		anna.SearchGender != models.SearchGenderMale {
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
		Callback:   &Callback{CallbackID: "filter2", Payload: "mode:selected", User: User{UserID: 1001}},
	}))
	must(bot.HandleUpdate(textUpdate(1001, "МГУ")))
	must(bot.HandleUpdate(Update{
		UpdateType: "message_callback",
		Callback:   &Callback{CallbackID: "filter3", Payload: fmt.Sprintf("pref:%d", msu.ID), User: User{UserID: 1001}},
	}))
	must(bot.HandleUpdate(Update{
		UpdateType: "message_callback",
		Callback:   &Callback{CallbackID: "filter4", Payload: "pref_done:", User: User{UserID: 1001}},
	}))
	anna, err = crud.GetUserByMaxID(db, 1001)
	if err != nil {
		t.Fatal(err)
	}
	if anna.Name != changedName || anna.SearchMode != models.SearchModeSelected ||
		len(anna.PreferredUniversities) != 1 || anna.PreferredUniversities[0].ID != msu.ID {
		t.Fatalf("filter was not saved or overwrote the profile: %+v", anna)
	}

	other, err := crud.CreateUser(db, crud.CreateUserInput{
		Name: "Борис", Age: 21, Gender: "male", Bio: "МФТИ", MaxPhotoToken: "boris-photo",
		MyUniversityID: &msu.ID, SearchMode: models.SearchModeAll,
	})
	if err != nil {
		t.Fatal(err)
	}

	woman, err := crud.CreateUser(db, crud.CreateUserInput{
		Name: "Варя", Age: 22, Gender: "female",
		MyUniversityID: &msu.ID, SearchMode: models.SearchModeAll,
	})
	must(err)

	profile, err := crud.GetNextProfile(db, anna.ID)
	must(err)
	if profile.ID != other.ID {
		t.Fatalf("with male preference, got wrong profile: %d", profile.ID)
	}
	must(bot.onCallback(1001, "menu:who"))
	must(bot.onCallback(1001, "who:female"))
	profile, err = crud.GetNextProfile(db, anna.ID)
	must(err)
	if profile.ID != woman.ID {
		t.Fatalf("with female preference, got wrong profile: %d", profile.ID)
	}
	must(bot.onCallback(1001, "menu:who"))
	must(bot.onCallback(1001, "who:male"))

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

	must(bot.onCallback(1001, "photo:edit"))
	must(bot.HandleBody(photoUpdate(1001, "new-photo")))
	anna, err = crud.GetUserByMaxID(db, 1001)
	if err != nil || anna.MaxPhotoToken != "new-photo" || anna.Name != changedName {
		t.Fatalf("photo replacement did not preserve the rest of the profile: %+v, %v", anna, err)
	}
	must(bot.onCallback(1001, "photo:edit"))
	must(bot.onCallback(1001, "photo:remove"))
	anna, err = crud.GetUserByMaxID(db, 1001)
	if err != nil || anna.MaxPhotoToken != "" || anna.PhotoURL != "" {
		t.Fatalf("photo was not removed: %+v, %v", anna, err)
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
	for _, token := range []string{"anna-photo", "boris-photo", "new-photo"} {
		if !strings.Contains(blob, `"payload":{"token":"`+token+`"},"type":"image"},{"payload":{"buttons":`) {
			t.Fatalf("photo %s must be sent with the card's buttons", token)
		}
	}
	last := sent[len(sent)-1]
	if strings.Contains(last, `"type":"image"`) {
		t.Fatalf("photo still present in the card after removal: %s", last)
	}
}

func TestPhotoStepCanBeSkipped(t *testing.T) {
	db, err := testDB(filepath.Join(t.TempDir(), "skip.db"))
	if err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	t.Cleanup(api.Close)
	bot := New(db, "test-token", "secret")
	bot.Client.BaseURL = api.URL
	bot.Client.HTTP = api.Client()

	session, err := bot.session(1002)
	if err != nil {
		t.Fatal(err)
	}
	session.Step = stepPhoto
	session.DraftName, session.DraftAge = "Лена", 21
	if err := db.Save(session).Error; err != nil {
		t.Fatal(err)
	}
	if err := bot.onCallback(1002, "photo:skip"); err != nil {
		t.Fatal(err)
	}
	user, err := crud.GetUserByMaxID(db, 1002)
	if err != nil || user.MaxPhotoToken != "" || user.Name != "Лена" || user.SearchGender != models.SearchGenderAll {
		t.Fatalf("profile was not created without a photo: %+v, %v", user, err)
	}
}

func photoUpdate(userID int64, token string) []byte {
	return []byte(fmt.Sprintf(`{"update_type":"message_created","message":{
		"sender":{"user_id":%d},"recipient":{"chat_type":"dialog"},"body":{"text":null,"attachments":[
			{"type":"image","payload":{"token":%q,"url":"https://example.org/photo?expires=1"}}
		]}}}`, userID, token))
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
	var dialector gorm.Dialector = sqlite.Open(path + "?_pragma=foreign_keys(1)")
	if os.Getenv("QA_POSTGRES") == "1" {
		u := &url.URL{Scheme: "postgres", Host: "postgres:5432", Path: "/" + os.Getenv("QA_DATABASE"),
			User: url.UserPassword(os.Getenv("POSTGRES_USER"), os.Getenv("POSTGRES_PASSWORD"))}
		u.RawQuery = "sslmode=disable"
		admin, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256([]byte(path))
		schema := fmt.Sprintf("test_%x", sum[:8])
		err = admin.Exec("CREATE SCHEMA " + schema).Error
		sql, closeErr := admin.DB()
		if closeErr == nil {
			_ = sql.Close()
		}
		if err != nil {
			return nil, err
		}
		params := u.Query()
		params.Set("search_path", schema)
		u.RawQuery = params.Encode()
		dialector = postgres.Open(u.String())
	}
	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&models.University{}, &models.User{}, &models.Like{}, &models.BotSession{}, &models.MaxRelay{}, &models.UserBlock{}, &models.MaxWebhookEvent{}); err != nil {
		return nil, err
	}
	return db, nil
}
