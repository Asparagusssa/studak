package telegrambot

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
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

	"studak/internal/bots/maxbot"
	"studak/internal/crud"
	"studak/internal/database"
	"studak/internal/models"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tg.db")
	var dialector gorm.Dialector = sqlite.Open(path + "?_pragma=foreign_keys(1)")
	if os.Getenv("QA_POSTGRES") == "1" {
		u := &url.URL{Scheme: "postgres", Host: "postgres:5432", Path: "/" + os.Getenv("QA_DATABASE"), User: url.UserPassword(os.Getenv("POSTGRES_USER"), os.Getenv("POSTGRES_PASSWORD"))}
		u.RawQuery = "sslmode=disable"
		admin, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(path))
		schema := fmt.Sprintf("telegram_%x", sum[:8])
		if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
			t.Fatal(err)
		}
		sql, _ := admin.DB()
		_ = sql.Close()
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		dialector = postgres.Open(u.String())
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	err = db.AutoMigrate(&models.University{}, &models.User{}, &models.Like{}, &models.BotSession{}, &models.MaxRelay{}, &models.MaxWebhookEvent{}, &models.UserBlock{})
	if err != nil {
		t.Fatal(err)
	}
	sql, _ := db.DB()
	t.Cleanup(func() { _ = sql.Close() })
	if err := database.SeedUniversities(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestTelegramSameFlowAndCrossPlatformMatches(t *testing.T) {
	db := testDB(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	var tgSent, maxSent []string
	tgAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		tgSent = append(tgSent, r.URL.Path+" "+string(raw))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":1}}`)
	}))
	t.Cleanup(tgAPI.Close)
	maxAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		maxSent = append(maxSent, r.URL.Path+" "+string(raw))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true}`)
	}))
	t.Cleanup(maxAPI.Close)

	tg := New(db, "123:fake-secret", "hook-secret")
	tg.Client.BaseURL, tg.Client.HTTP = tgAPI.URL, tgAPI.Client()
	max := maxbot.New(db, "max-fake", "max-secret")
	max.Client.BaseURL, max.Client.HTTP = maxAPI.URL, maxAPI.Client()
	peers := map[string]maxbot.Messenger{"telegram": tg.Client, "max": max.Client}
	tg.Engine.Peers, max.Peers = peers, peers

	// The same numeric messenger ID is intentionally used on both platforms.
	maxID := int64(101)
	maxUser, err := crud.CreateUser(db, crud.CreateUserInput{MaxID: &maxID, Name: "Борис", Age: 22, Gender: "male"})
	must(err)
	seq := 0
	event := func(value map[string]any, secret string) int {
		t.Helper()
		seq++
		value["update_id"] = seq
		raw, _ := json.Marshal(value)
		req := httptest.NewRequest("POST", "/webhook/tg", bytes.NewReader(raw))
		req.Header.Set("X-Telegram-Bot-Api-Secret-Token", secret)
		rec := httptest.NewRecorder()
		tg.ServeHTTP(rec, req)
		return rec.Code
	}
	post := func(value map[string]any) {
		t.Helper()
		if status := event(value, "hook-secret"); status != 200 {
			t.Fatalf("Telegram webhook returned %d", status)
		}
	}
	message := func(text string) {
		t.Helper()
		post(map[string]any{"message": map[string]any{"message_id": seq + 1, "from": map[string]any{"id": 101, "first_name": "Аня"}, "chat": map[string]any{"id": 101, "type": "private"}, "text": text}})
	}
	callback := func(data string) {
		t.Helper()
		post(map[string]any{"callback_query": map[string]any{"id": fmt.Sprintf("cb-%d", seq+1), "from": map[string]any{"id": 101}, "message": map[string]any{"chat": map[string]any{"id": 101, "type": "private"}}, "data": data}})
	}
	if code := event(map[string]any{}, "wrong"); code != 401 {
		t.Fatalf("invalid secret accepted: %d", code)
	}
	message("/start")
	message("Аня")
	message("20")
	callback("gender:female")
	callback("who:male")
	message("МГУ")
	var university models.University
	must(db.Where("short_name = ?", "МГУ").First(&university).Error)
	callback(fmt.Sprintf("uni:%d", university.ID))
	callback("mode:all")
	message("Люблю кино и прогулки")
	post(map[string]any{"message": map[string]any{
		"message_id": seq + 1, "from": map[string]any{"id": 101}, "chat": map[string]any{"id": 101, "type": "private"},
		"photo": []map[string]any{{"file_id": "small-file", "width": 80, "height": 100}, {"file_id": "big-file", "width": 800, "height": 1000}},
	}})
	user, err := crud.GetUserByTelegramID(db, 101)
	must(err)
	if user.ID == maxUser.ID || user.MaxID != nil || user.TelegramPhotoID != "big-file" || user.MaxPhotoToken != "" || user.SearchGender != "male" {
		t.Fatalf("Telegram profile not isolated from MAX identity: %+v", user)
	}
	callback("menu:profile")
	mu.Lock()
	joined := strings.Join(tgSent, "\n")
	mu.Unlock()
	if !strings.Contains(joined, "/sendPhoto") || !strings.Contains(joined, `"photo":"big-file"`) || !strings.Contains(joined, "privacy:home") {
		t.Fatalf("Telegram photo/menu missing: %s", joined)
	}
	// Plain responses and callback payloads come from the same engine.
	if !strings.Contains(joined, "Сколько тебе лет") || !strings.Contains(joined, `"callback_data":"who:male"`) {
		t.Fatal("Telegram onboarding does not use the shared controls")
	}

	must(max.HandleUpdate(maxbot.Update{UpdateType: "bot_started", User: &maxbot.User{UserID: 101}}))
	must(max.HandleUpdate(maxbot.Update{UpdateType: "message_callback", Callback: &maxbot.Callback{CallbackID: "max-like", User: maxbot.User{UserID: 101}, Payload: fmt.Sprintf("like:%d", user.ID)}}))
	callback("menu:incoming")
	callback(fmt.Sprintf("like_back:%d", maxUser.ID))
	if _, err := crud.MatchedUser(db, user.ID, maxUser.ID); err != nil {
		t.Fatal(err)
	}
	callback(fmt.Sprintf("write:%d", maxUser.ID))
	message("Привет из Telegram")
	mu.Lock()
	mx := strings.Join(maxSent, "\n")
	mu.Unlock()
	if !strings.Contains(mx, "Привет из Telegram") {
		t.Fatalf("cross-platform message not delivered to MAX: %s", mx)
	}
	must(max.HandleUpdate(maxbot.Update{UpdateType: "message_callback", Callback: &maxbot.Callback{CallbackID: "max-write", User: maxbot.User{UserID: 101}, Payload: fmt.Sprintf("write:%d", user.ID)}}))
	must(max.HandleUpdate(maxbot.Update{UpdateType: "message_created", Message: &maxbot.Message{Sender: maxbot.User{UserID: 101}, Recipient: maxbot.Recipient{ChatType: "dialog"}, Body: maxbot.Body{Mid: "reply-from-max", Text: "Привет из MAX"}}}))
	mu.Lock()
	tgMessages := strings.Join(tgSent, "\n")
	mu.Unlock()
	if !strings.Contains(tgMessages, "Привет из MAX") {
		t.Fatal("reply from MAX not routed to Telegram")
	}

	callback("privacy:pause")
	user, err = crud.GetUserByTelegramID(db, 101)
	must(err)
	if !user.Paused {
		t.Fatal("Telegram pause not stored in shared database")
	}
	callback("privacy:resume")
	callback(fmt.Sprintf("more:%d", maxUser.ID))
	callback(fmt.Sprintf("privacy:block:%d", maxUser.ID))
	if _, err := crud.MatchedUser(db, maxUser.ID, user.ID); err != crud.ErrNotFound {
		t.Fatalf("cross-platform block failed: %v", err)
	}
	callback("privacy:delete")
	var session models.BotSession
	must(db.Where("channel = ? AND external_id = ?", "telegram", 101).First(&session).Error)
	if !strings.HasPrefix(session.Step, "delete:") {
		t.Fatalf("missing confirmation %s", session.Step)
	}
	callback("privacy:confirm_delete:" + strings.TrimPrefix(session.Step, "delete:"))
	if _, err := crud.GetUserByTelegramID(db, 101); err != crud.ErrNotFound {
		t.Fatalf("Telegram profile was not deleted: %v", err)
	}
	if _, err := crud.GetUserByMaxID(db, 101); err != nil {
		t.Fatalf("deleting Telegram profile affected same-ID MAX user: %v", err)
	}
}

func TestTelegramWebhookBoundaries(t *testing.T) {
	db := testDB(t)
	b := New(db, "123:secret", "secret")
	for _, tc := range []struct {
		name, raw string
		code      int
	}{
		{"bad-json", "bad", 400}, {"no-id", `{"message":{}}`, 400}, {"trailing", `{"update_id":1}{}`, 400},
		{"unsupported", `{"update_id":1,"edited_message":{}}`, 200},
		{"group", `{"update_id":2,"message":{"message_id":2,"from":{"id":15},"chat":{"type":"group","id":-20},"text":"/start"}}`, 200},
		{"group-callback", `{"update_id":3,"callback_query":{"id":"c","from":{"id":15},"message":{"chat":{"type":"group","id":-20}},"data":"privacy:pause"}}`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/webhook/tg", strings.NewReader(tc.raw))
			req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "secret")
			rec := httptest.NewRecorder()
			b.ServeHTTP(rec, req)
			if rec.Code != tc.code {
				t.Fatalf("got %d want %d", rec.Code, tc.code)
			}
		})
	}
	var n int64
	if err := db.Model(&models.BotSession{}).Count(&n).Error; err != nil || n != 0 {
		t.Fatalf("invalid/group events created sessions: %d %v", n, err)
	}
}

func TestTelegramLongPhotoCaptionAndRedactedErrors(t *testing.T) {
	var methods []string
	var bodies []string
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		methods = append(methods, r.URL.Path)
		bodies = append(bodies, string(raw))
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer mock.Close()
	c := NewClient("123:very-private-token")
	c.BaseURL, c.HTTP = mock.URL, mock.Client()
	long := strings.Repeat("а", 1100)
	err := c.SendPhotoContext(t.Context(), 101, long, [][]maxbot.Button{{maxbot.CallbackButton("В меню", "menu:home")}}, map[string]string{"token": "photo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(methods) != 2 || !strings.HasSuffix(methods[0], "/sendPhoto") || !strings.HasSuffix(methods[1], "/sendMessage") || !strings.Contains(bodies[1], long) || !strings.Contains(bodies[1], "inline_keyboard") {
		t.Fatalf("caption was truncated or buttons lost: %+v", methods)
	}
	mock.Close()
	err = c.SendContext(t.Context(), 101, "fail", nil)
	if err == nil || strings.Contains(err.Error(), "very-private-token") {
		t.Fatalf("connection error leaked token: %v", err)
	}
}
