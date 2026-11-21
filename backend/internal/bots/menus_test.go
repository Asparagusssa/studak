package bots

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestConfigureMenus(t *testing.T) {
	var telegramMethods []string
	var languages []string
	var maxCalls int
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if r.URL.Path == "/me/commands" {
			maxCalls++
			if r.Method != http.MethodPatch || r.Header.Get("Authorization") != "max-test" {
				t.Errorf("incorrect MAX request: %s", r.Method)
			}
			want := map[string]any{"commands": []any{map[string]any{"name": "menu", "description": "Главное меню"}}}
			if !reflect.DeepEqual(body, want) {
				t.Errorf("MAX menu payload: %#v", body)
			}
			_, _ = w.Write([]byte(`{"commands":[{"name":"menu","description":"Главное меню"}]}`))
			return
		}
		telegramMethods = append(telegramMethods, r.URL.Path)
		if r.Method != http.MethodPost {
			t.Errorf("incorrect Telegram method: %s", r.Method)
		}
		switch r.URL.Path {
		case "/bot123:test/setMyCommands":
			languages = append(languages, body["language_code"].(string))
			want := []any{map[string]any{"command": "menu", "description": "Главное меню"}}
			if !reflect.DeepEqual(body["commands"], want) {
				t.Errorf("Telegram commands: %#v", body)
			}
		case "/bot123:test/setChatMenuButton":
			if !reflect.DeepEqual(body, map[string]any{"menu_button": map[string]any{"type": "commands"}}) {
				t.Errorf("Telegram menu button is not the default command button: %#v", body)
			}
		default:
			t.Errorf("unexpected Telegram request %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer api.Close()
	oldTG, oldMax := telegramAPI, maxAPI
	telegramAPI, maxAPI = api.URL, api.URL
	t.Cleanup(func() { telegramAPI, maxAPI = oldTG, oldMax })
	t.Setenv("TG_BOT_TOKEN", "123:test")
	t.Setenv("MAX_BOT_TOKEN", "max-test")
	// No webhook or ngrok URL is needed to configure the native menus.
	t.Setenv("WEBHOOK_BASE_URL", "")
	ConfigureMenus()
	if len(telegramMethods) != 3 || !reflect.DeepEqual(languages, []string{"", "ru"}) || maxCalls != 1 {
		t.Fatalf("menu configuration missing: Telegram=%v languages=%v MAX=%d", telegramMethods, languages, maxCalls)
	}
}

func TestMenuConfigurationRejectsUnconfirmedResponses(t *testing.T) {
	for _, response := range []string{`{"ok":false,"description":"rejected"}`, `{}`, `not-json`} {
		t.Run(response, func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(response))
			}))
			defer api.Close()
			oldTG, oldMax := telegramAPI, maxAPI
			telegramAPI, maxAPI = api.URL, api.URL
			t.Cleanup(func() { telegramAPI, maxAPI = oldTG, oldMax })
			if err := configureTelegramMenu("123:test"); err == nil {
				t.Fatal("Telegram rejection was ignored")
			}
			if err := configureMaxMenu("max-test"); err == nil {
				t.Fatal("MAX rejection was ignored")
			}
		})
	}
}
