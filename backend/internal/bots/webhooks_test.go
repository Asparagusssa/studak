package bots

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestRegisterWebhooksTelegramAndMax(t *testing.T) {
	var mu sync.Mutex
	var tgBody map[string]any
	var maxDeleted []string
	var maxPosted map[string]any

	tg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/setWebhook") {
			t.Errorf("unexpected telegram request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&tgBody)
		_, _ = w.Write([]byte(`{"ok":true,"result":true,"description":"Webhook was set"}`))
	}))
	defer tg.Close()

	maxMessenger := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/subscriptions":
			if got := r.Header.Get("Authorization"); got != "max-token" {
				t.Errorf("max auth: %q", got)
			}
			_, _ = w.Write([]byte(`{"subscriptions":[{"url":"https://abc.ngrok-free.app/webhook/max"},{"url":"https://production.example/webhook/max"}]}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/subscriptions":
			mu.Lock()
			maxDeleted = append(maxDeleted, r.URL.Query().Get("url"))
			mu.Unlock()
			_, _ = w.Write([]byte(`{"success":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/subscriptions":
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &maxPosted)
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			t.Errorf("unexpected max request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer maxMessenger.Close()

	oldTg, oldMax := telegramAPI, maxAPI
	telegramAPI, maxAPI = tg.URL, maxMessenger.URL
	t.Cleanup(func() { telegramAPI, maxAPI = oldTg, oldMax })

	t.Setenv("TG_BOT_TOKEN", "123456:test-token")
	t.Setenv("TG_WEBHOOK_SECRET", "telegram-secret-123")
	t.Setenv("MAX_BOT_TOKEN", "max-token")

	RegisterWebhooks("https://abc.ngrok-free.app/")

	if tgBody["url"] != "https://abc.ngrok-free.app/webhook/tg" {
		t.Fatalf("telegram url: %#v", tgBody["url"])
	}
	if tgBody["secret_token"] != "telegram-secret-123" {
		t.Fatalf("telegram secret: %#v", tgBody["secret_token"])
	}
	if len(maxDeleted) != 1 || maxDeleted[0] != "https://abc.ngrok-free.app/webhook/max" {
		t.Fatalf("max deleted: %#v", maxDeleted)
	}
	if maxPosted["url"] != "https://abc.ngrok-free.app/webhook/max" {
		t.Fatalf("max posted: %#v", maxPosted)
	}
}

func TestValidTelegramToken(t *testing.T) {
	if !ValidTelegramToken("123456:AbCd_ef-123") {
		t.Fatal("valid token rejected")
	}
	for _, token := range []string{"", "tg-token", "abc:secret", "123:bad/segment", "123:bad?query", "123:bad secret"} {
		if ValidTelegramToken(token) {
			t.Errorf("invalid token accepted: %q", token)
		}
	}
}

func TestRegisterWebhooksSkipsEmpty(t *testing.T) {
	_ = os.Unsetenv("TG_BOT_TOKEN")
	_ = os.Unsetenv("MAX_BOT_TOKEN")
	RegisterWebhooks("")
	RegisterWebhooks("http://insecure.example")
}

func TestNormalizeWebhookBaseURL(t *testing.T) {
	good, err := normalizeWebhookBaseURL(" https://example.test/hooks/ ")
	if err != nil || good != "https://example.test/hooks" {
		t.Fatalf("valid URL: got %q, err=%v", good, err)
	}
	for _, raw := range []string{
		"http://example.test",
		"https://user:pass@example.test",
		"https://example.test?token=secret",
		"https://example.test/#fragment",
		"not a url",
	} {
		if _, err := normalizeWebhookBaseURL(raw); err == nil {
			t.Errorf("expected %q to be rejected", raw)
		}
	}
}
