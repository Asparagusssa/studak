package telegrambot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm"
	"studak/internal/crud"
	"studak/internal/models"
)

type tgQA struct {
	t         *testing.T
	db        *gorm.DB
	bot       *Bot
	seq       int
	mu        sync.Mutex
	sent      []tgDelivery
	usernames map[int64]string
}
type tgDelivery struct {
	Method, Text string
	Recipient    int64
	Buttons      [][]button
}

func newTGQA(t *testing.T) *tgQA {
	t.Helper()
	q := &tgQA{t: t, db: testDB(t), usernames: make(map[int64]string)}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ChatID  int64  `json:"chat_id"`
			Text    string `json:"text"`
			Caption string `json:"caption"`
			Markup  struct {
				Buttons [][]button `json:"inline_keyboard"`
			} `json:"reply_markup"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		text := body.Text
		if text == "" {
			text = body.Caption
		}
		q.mu.Lock()
		q.sent = append(q.sent, tgDelivery{Method: r.URL.Path, Recipient: body.ChatID, Text: text, Buttons: body.Markup.Buttons})
		q.mu.Unlock()
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":100}}`)
	}))
	t.Cleanup(api.Close)
	q.bot = New(q.db, "123:fake-token", "qa-secret")
	q.bot.Client.BaseURL, q.bot.Client.HTTP = api.URL, api.Client()
	return q
}
func (q *tgQA) must(err error) {
	q.t.Helper()
	if err != nil {
		q.t.Fatal(err)
	}
}
func (q *tgQA) post(raw []byte) {
	q.t.Helper()
	req := httptest.NewRequest("POST", "/webhook/tg", bytes.NewReader(raw))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "qa-secret")
	rec := httptest.NewRecorder()
	q.bot.ServeHTTP(rec, req)
	if rec.Code != 200 {
		q.t.Fatalf("webhook %d: %s", rec.Code, rec.Body.String())
	}
}
func (q *tgQA) message(id int64, text string, extra map[string]any) []byte {
	q.t.Helper()
	q.seq++
	msg := map[string]any{"message_id": q.seq, "from": map[string]any{"id": id, "username": q.usernames[id]}, "chat": map[string]any{"id": id, "type": "private"}, "text": text}
	for k, v := range extra {
		msg[k] = v
	}
	raw, err := json.Marshal(map[string]any{"update_id": q.seq, "message": msg})
	q.must(err)
	q.post(raw)
	return raw
}
func (q *tgQA) click(id int64, data string) {
	q.t.Helper()
	q.seq++
	raw, err := json.Marshal(map[string]any{"update_id": q.seq, "callback_query": map[string]any{"id": fmt.Sprintf("qa-callback-%d", q.seq), "from": map[string]any{"id": id, "username": q.usernames[id]}, "message": map[string]any{"chat": map[string]any{"id": id, "type": "private"}}, "data": data}})
	q.must(err)
	q.post(raw)
}
func (q *tgQA) user(id int64) *models.User {
	q.t.Helper()
	u, err := crud.GetUserByTelegramID(q.db, id)
	q.must(err)
	return u
}
func (q *tgQA) state(id int64) models.BotSession {
	q.t.Helper()
	var s models.BotSession
	q.must(q.db.Where("channel = ? AND external_id = ?", "telegram", id).First(&s).Error)
	return s
}
func (q *tgQA) last(id int64) tgDelivery {
	q.t.Helper()
	q.mu.Lock()
	defer q.mu.Unlock()
	for i := len(q.sent) - 1; i >= 0; i-- {
		if q.sent[i].Recipient == id {
			return q.sent[i]
		}
	}
	q.t.Fatalf("no delivery to %d", id)
	return tgDelivery{}
}
func (q *tgQA) count(id int64, part string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := 0
	for _, m := range q.sent {
		if m.Recipient == id && strings.Contains(m.Text, part) {
			n++
		}
	}
	return n
}
func (q *tgQA) registerUntilPhoto(id int64, name, gender string) {
	q.t.Helper()
	q.message(id, "/start", nil)
	q.message(id, name, nil)
	q.message(id, "не возраст", nil)
	if q.state(id).Step != "age" {
		q.t.Fatal("invalid age advanced registration")
	}
	q.message(id, "21", nil)
	q.click(id, "gender:"+gender)
	q.click(id, "who:all")
	q.message(id, "МГУ", nil)
	var uni models.University
	q.must(q.db.Where("short_name = ?", "МГУ").First(&uni).Error)
	q.click(id, fmt.Sprintf("uni:%d", uni.ID))
	q.click(id, "mode:all")
	q.click(id, "bio_skip:")
}

func (q *tgQA) register(id int64, name, gender string) {
	q.t.Helper()
	q.registerUntilPhoto(id, name, gender)
	q.message(id, "", map[string]any{"photo": []map[string]any{{"file_id": "registration-photo", "width": 800, "height": 900}}})
}

func TestTelegramMenuCommandFromAnyStep(t *testing.T) {
	q := newTGQA(t)
	q.message(90, "/start", nil)
	q.message(90, "Анна", nil)
	q.message(90, "/menu", nil)
	if s := q.state(90); s.Step != "age" || s.DraftName != "Анна" {
		t.Fatalf("menu command changed an unfinished registration: %+v", s)
	}
	q.register(90, "Анна", "female")
	for _, command := range []string{"/menu", "меню", "☰ Меню"} {
		q.click(90, "photo:edit")
		q.message(90, command, nil)
		if q.state(90).Step != "menu" || len(q.last(90).Buttons) == 0 {
			t.Fatalf("%q did not open the menu from photo editing", command)
		}
		if q.user(90).TelegramPhotoID != "registration-photo" {
			t.Fatal("opening the menu changed the profile photo")
		}
	}
}

func TestTelegramRegistrationRequiresPhoto(t *testing.T) {
	q := newTGQA(t)
	q.message(91, "/start", nil)
	if text := q.last(91).Text; !strings.Contains(text, "Студак") || strings.Contains(text, "Искра") {
		t.Fatalf("incorrect welcome brand: %s", text)
	}
	q.registerUntilPhoto(91, "Анна", "female")
	if len(q.last(91).Buttons) != 0 || !strings.Contains(q.last(91).Text, "Фото обязательно") {
		t.Fatal("photo prompt still permits skipping")
	}
	q.click(91, "photo:skip")
	q.message(91, "пропустить", nil)
	if _, err := crud.GetUserByTelegramID(q.db, 91); err != crud.ErrNotFound || q.state(91).Step != "photo" {
		t.Fatal("skipping published a profile without a photo")
	}
	q.message(91, "", map[string]any{"photo": []map[string]any{{"file_id": "required-photo", "width": 800, "height": 900}}})
	if q.user(91).TelegramPhotoID != "required-photo" || q.state(91).Step != "menu" {
		t.Fatal("registration did not finish after sending a photo")
	}
}

func TestTelegramGenderChoicesAndFeedControls(t *testing.T) {
	q := newTGQA(t)
	q.message(92, "/start", nil)
	q.message(92, "Анна", nil)
	q.message(92, "21", nil)
	for _, row := range q.last(92).Buttons {
		for _, button := range row {
			if button.Data == "gender:other" || button.Text == "Другое" {
				t.Fatal("obsolete gender choice still shown")
			}
		}
	}
	q.click(92, "gender:other")
	if q.state(92).Step != "gender" || q.state(92).DraftGender != "" {
		t.Fatal("stale gender button was accepted")
	}
	q.register(93, "Лера", "female")
	q.register(94, "Саша", "male")
	q.click(93, "menu:feed")
	card := q.last(93)
	if len(card.Buttons) < 2 || len(card.Buttons[0]) != 2 || card.Buttons[0][0].Text != "❤️" || card.Buttons[0][1].Text != "👎" {
		t.Fatalf("feed reactions are not emoji-only: %+v", card.Buttons)
	}
	peer := q.user(94)
	block := card.Buttons[1][0]
	if block.Text != "Заблокировать" || block.Data != fmt.Sprintf("block:%d", peer.ID) || block.Style != "danger" {
		t.Fatalf("direct block action missing: %+v", block)
	}
	q.click(93, block.Data)
	viewer := q.user(93)
	if blocked, err := crud.IsPairBlocked(q.db, viewer.ID, peer.ID); err != nil || blocked || q.state(93).Step != "block" {
		t.Fatalf("block confirmation was bypassed: %v, %v", blocked, err)
	}
	q.click(93, fmt.Sprintf("privacy:block:%d", peer.ID))
	if blocked, err := crud.IsPairBlocked(q.db, viewer.ID, peer.ID); err != nil || !blocked {
		t.Fatalf("confirmed block failed: %v, %v", blocked, err)
	}
}

func TestTelegramContactsRequireAMatchAndTrackUsernameChanges(t *testing.T) {
	q := newTGQA(t)
	q.usernames[95], q.usernames[96] = "masha_test", "sasha_test"
	q.register(95, "Маша", "female")
	q.register(96, "Саша", "male")
	me, peer := q.user(95), q.user(96)
	if me.TelegramUsername != "masha_test" || peer.TelegramUsername != "sasha_test" {
		t.Fatal("authenticated usernames were not saved during registration")
	}
	hasLink := func(delivery tgDelivery, link string) bool {
		for _, row := range delivery.Buttons {
			for _, button := range row {
				if button.URL == link {
					return true
				}
			}
		}
		return false
	}
	q.click(95, "menu:feed")
	if strings.Contains(q.last(95).Text, "sasha_test") || hasLink(q.last(95), "https://t.me/sasha_test") {
		t.Fatal("feed disclosed a contact before a match")
	}
	q.click(95, fmt.Sprintf("write:%d", peer.ID))
	if hasLink(q.last(95), "https://t.me/sasha_test") {
		t.Fatal("legacy write callback bypassed mutual-like requirement")
	}
	q.click(95, fmt.Sprintf("like:%d", peer.ID))
	if strings.Contains(q.last(96).Text, "masha_test") {
		t.Fatal("incoming-like notification disclosed contact before a match")
	}
	q.click(96, fmt.Sprintf("like_back:%d", me.ID))
	if !hasLink(q.last(95), "https://t.me/sasha_test") || !hasLink(q.last(96), "https://t.me/masha_test") {
		t.Fatal("mutual match did not expose direct Telegram contacts to both participants")
	}
	q.click(95, fmt.Sprintf("write:%d", peer.ID))
	q.message(95, "Не пересылай этот текст", nil)
	if q.state(95).Step == "write" || q.count(96, "Не пересылай этот текст") != 0 {
		t.Fatal("legacy button still starts bot-mediated chat")
	}
	q.must(q.db.Model(&models.BotSession{}).Where("channel = ? AND external_id = ?", "telegram", 95).
		Updates(map[string]any{"step": "write", "peer_user_id": peer.ID}).Error)
	q.message(95, "Старый черновик тоже не отправляй", nil)
	if q.state(95).Step == "write" || q.count(96, "Старый черновик тоже не отправляй") != 0 {
		t.Fatal("pre-upgrade draft still forwards text")
	}
	q.usernames[96] = "sasha_new"
	q.message(96, "/start", nil)
	q.click(95, fmt.Sprintf("match:%d", peer.ID))
	if hasLink(q.last(95), "https://t.me/sasha_new") || q.user(96).TelegramUsername != "sasha_new" {
		t.Fatal("updated Telegram username was not refreshed or stale history disclosed it")
	}
	q.usernames[96] = ""
	q.message(96, "/start", nil)
	q.click(95, fmt.Sprintf("match:%d", peer.ID))
	if q.user(96).TelegramUsername != "" || hasLink(q.last(95), "tg://user?id=96") {
		t.Fatal("removed username was not cleared or deleted history disclosed it")
	}
	q.click(95, fmt.Sprintf("block:%d", peer.ID))
	q.click(95, fmt.Sprintf("privacy:block:%d", peer.ID))
	q.click(95, fmt.Sprintf("write:%d", peer.ID))
	if hasLink(q.last(95), "tg://user?id=96") {
		t.Fatal("blocked match still reveals its contact through stale callback")
	}
}

func TestTelegramNativeLifecycle(t *testing.T) {
	q := newTGQA(t)
	q.register(51, "Маша", "female")
	q.register(52, "Саша", "male")
	a, b := q.user(51), q.user(52)
	// University preferences can be edited without restarting registration.
	q.click(51, "menu:filter")
	q.click(51, "mode:selected")
	q.message(51, "МГУ", nil)
	q.click(51, fmt.Sprintf("pref:%d", *a.MyUniversityID))
	q.click(51, "pref_done:")
	a = q.user(51)
	if a.Name != "Маша" || a.SearchMode != "selected" || len(a.PreferredUniversities) != 1 {
		t.Fatal("filter edit corrupted the profile")
	}
	q.click(51, "menu:who")
	q.click(51, "who:male")
	if q.user(51).SearchGender != "male" {
		t.Fatal("gender search was not saved")
	}
	q.click(51, fmt.Sprintf("like:%d", b.ID))
	q.click(52, "menu:incoming")
	q.click(52, fmt.Sprintf("like_back:%d", a.ID))
	var pairCount int64
	q.must(q.db.Model(&models.Like{}).Where("from_user_id IN ?", []uint{a.ID, b.ID}).Count(&pairCount).Error)
	if pairCount != 0 {
		t.Fatal("reciprocal likes remained after both notifications")
	}
	if q.count(51, "Симпатия взаимна") != 1 || q.count(52, "Симпатия взаимна") != 1 {
		t.Fatal("missing mutuality messages")
	}

	q.click(51, fmt.Sprintf("write:%d", b.ID))
	q.message(51, strings.Repeat("я", 1201), nil)
	if q.state(51).Step == "write" {
		t.Fatal("legacy write action still starts message drafting")
	}
	q.message(51, "caption", map[string]any{"document": map[string]any{"file_id": "document"}})
	if q.state(51).Step == "write" {
		t.Fatal("attachment reopened retired message drafting")
	}
	q.message(51, "/cancel", nil)
	if q.state(51).Step == "write" {
		t.Fatal("cancel did not exit draft")
	}
	q.click(51, fmt.Sprintf("write:%d", b.ID))
	raw := q.message(51, "Один привет", nil)
	q.post(raw)
	if q.count(52, "Один привет") != 0 {
		t.Fatal("Telegram still relays text after a match")
	}
	q.click(52, fmt.Sprintf("write:%d", a.ID))
	q.message(52, "Привет в ответ", nil)
	if q.count(51, "Привет в ответ") != 0 {
		t.Fatal("Telegram still relays replies")
	}
	q.click(51, "privacy:pause")
	if _, err := crud.GetNextProfile(q.db, a.ID); err != crud.ErrProfilePaused {
		t.Fatal("pause did not stop new feed")
	}
	q.click(52, fmt.Sprintf("write:%d", a.ID))
	q.message(52, "Сообщение во время паузы", nil)
	if q.count(51, "Сообщение во время паузы") != 0 {
		t.Fatal("bot still relays text during pause")
	}
	q.click(51, "privacy:resume")
	q.click(52, fmt.Sprintf("write:%d", a.ID))
	q.click(51, fmt.Sprintf("more:%d", b.ID))
	q.click(51, fmt.Sprintf("privacy:block:%d", b.ID))
	q.message(52, "После блокировки", nil)
	if q.count(51, "После блокировки") != 0 {
		t.Fatal("Telegram block failed to close existing draft")
	}
	q.click(51, "privacy:blocked:0")
	q.click(51, fmt.Sprintf("privacy:unblock:%d", b.ID))
	if _, err := crud.MatchedUser(q.db, a.ID, b.ID); err != crud.ErrNotFound {
		t.Fatal("unblock restored the old match")
	}
	q.click(51, "privacy:delete")
	old := q.state(51).Step
	q.click(51, "privacy:home")
	q.click(51, "privacy:delete")
	fresh := q.state(51).Step
	q.click(51, "privacy:confirm_delete:"+strings.TrimPrefix(old, "delete:"))
	if _, err := crud.GetUserByTelegramID(q.db, 51); err != nil {
		t.Fatal("stale delete confirmation deleted the account")
	}
	q.click(51, "privacy:confirm_delete:"+strings.TrimPrefix(fresh, "delete:"))
	if _, err := crud.GetUserByTelegramID(q.db, 51); err != crud.ErrNotFound {
		t.Fatal("confirmed deletion failed")
	}
	q.post(raw)
	if _, err := crud.GetUserByTelegramID(q.db, 51); err != crud.ErrNotFound {
		t.Fatal("replayed update recreated deleted profile")
	}
	q.message(51, "/start", nil)
	if q.state(51).Step != "name" {
		t.Fatal("cannot register again after deletion")
	}
}

func TestTelegramAlbumsReplacementAndRequiredPhoto(t *testing.T) {
	q := newTGQA(t)
	q.register(61, "Лера", "female")
	q.click(61, "photo:edit")
	photos := []map[string]any{{"file_id": "photo-one", "width": 800, "height": 900}}
	q.message(61, "", map[string]any{"photo": photos, "media_group_id": "album"})
	if q.user(61).TelegramPhotoID != "registration-photo" || q.state(61).Step != "edit_photo" {
		t.Fatal("album should not save its first photo")
	}
	q.message(61, "", map[string]any{"photo": photos})
	if q.user(61).TelegramPhotoID != "photo-one" {
		t.Fatal("one photo was not saved")
	}
	q.click(61, "photo:edit")
	q.message(61, "", map[string]any{"photo": []map[string]any{{"file_id": "photo-two", "width": 1200, "height": 900}}})
	if q.user(61).TelegramPhotoID != "photo-two" || !strings.HasSuffix(q.last(61).Method, "/sendPhoto") {
		t.Fatal("photo replacement was not rendered")
	}
	q.click(61, "photo:edit")
	q.click(61, "photo:remove")
	if q.user(61).TelegramPhotoID != "photo-two" || q.state(61).Step != "edit_photo" {
		t.Fatal("stale remove callback deleted the required photo")
	}
	for _, row := range q.last(61).Buttons {
		for _, button := range row {
			if button.Data == "photo:remove" {
				t.Fatal("photo editor offers removal")
			}
		}
	}
}

func TestTelegramConcurrentWebhookReplay(t *testing.T) {
	q := newTGQA(t)
	q.message(71, "/start", nil)
	raw := q.message(71, "Лена", nil)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); q.post(raw) }()
	}
	wg.Wait()
	if s := q.state(71); s.Step != "age" || s.DraftName != "Лена" {
		t.Fatalf("replay damaged registration: %+v", s)
	}
	client := q.bot.Client
	q.bot = New(q.db, "123:fake-token", "qa-secret")
	q.bot.Client = client
	q.bot.Engine.Messenger = client
	q.post(raw)
	if q.count(71, "Сколько тебе лет") != 1 {
		t.Fatal("duplicate replay was answered after bot restart")
	}
}

func TestTelegramAPIErrorMatrix(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"blocked", 403, `{"ok":false,"error_code":403}`}, {"too many", 429, `{"ok":false,"error_code":429,"parameters":{"retry_after":3}}`},
		{"server", 500, `{"ok":false}`}, {"logical failure", 200, `{"ok":false,"error_code":400}`}, {"bad JSON", 200, `invalid`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer mock.Close()
			c := NewClient("123:private-token")
			c.BaseURL, c.HTTP = mock.URL, mock.Client()
			err := c.SendContext(t.Context(), 81, "text", nil)
			if err == nil || strings.Contains(err.Error(), "private-token") {
				t.Fatalf("bad error handling: %v", err)
			}
		})
	}
}

func TestTelegramPhotoFallbackAndMenu(t *testing.T) {
	q := newTGQA(t)
	q.register(91, "Тест", "female")
	menu := q.last(91).Buttons
	if len(menu) == 0 || len(menu[len(menu)-1]) != 1 || menu[len(menu)-1][0].Data != "privacy:home" {
		t.Fatal("settings must occupy entire bottom row")
	}
	for _, row := range menu {
		for _, btn := range row {
			if len(btn.Data) > 64 {
				t.Fatalf("callback exceeds Telegram limit: %s", btn.Data)
			}
		}
	}
	calls := 0
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if strings.HasSuffix(r.URL.Path, "/sendPhoto") {
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"ok":false,"error_code":400}`)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer mock.Close()
	q.bot.Client.BaseURL, q.bot.Client.HTTP = mock.URL, mock.Client()
	token := "bad-photo"
	_, err := crud.UpdateUser(q.db, q.user(91).ID, crud.UpdateUserInput{TelegramPhotoID: &token})
	q.must(err)
	q.click(91, "menu:profile")
	// callback acknowledgement, failed photo, successful text fallback.
	if calls != 3 {
		t.Fatalf("photo fallback made %d API calls", calls)
	}
}
