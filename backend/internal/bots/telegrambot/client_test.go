package telegrambot

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"studak/internal/bots/maxbot"
)

func TestTelegramSendsColoredButtons(t *testing.T) {
	var sent []map[string]any
	var mu sync.Mutex
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		mu.Lock()
		sent = append(sent, body)
		mu.Unlock()
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer api.Close()
	client := NewClient("123:fake-token")
	client.BaseURL, client.HTTP = api.URL, api.Client()
	rows := [][]maxbot.Button{{
		maxbot.CallbackButton("Моя анкета", "menu:profile"),
		maxbot.CallbackButton("Нравится", "like:42"),
		maxbot.CallbackButton("Удалить навсегда", "privacy:confirm_delete:nonce"),
		{Text: "Открыть", URL: "https://example.org"},
	}}
	if err := client.SendContext(t.Context(), 11, "Студак", rows); err != nil {
		t.Fatal(err)
	}
	if err := client.SendPhotoContext(t.Context(), 11, "Анкета", rows, map[string]string{"token": "photo"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 2 {
		t.Fatalf("expected text and photo requests, got %d", len(sent))
	}
	for _, body := range sent {
		buttons := body["reply_markup"].(map[string]any)["inline_keyboard"].([]any)[0].([]any)
		for i, expected := range []string{"primary", "success", "danger", "primary"} {
			if buttons[i].(map[string]any)["style"] != expected {
				t.Fatalf("incorrect button style in Telegram request: %+v", buttons[i])
			}
		}
		if buttons[0].(map[string]any)["callback_data"] != "menu:profile" || buttons[3].(map[string]any)["url"] != "https://example.org" {
			t.Fatal("color styling changed button actions")
		}
	}
}
