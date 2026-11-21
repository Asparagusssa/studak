package bots

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"studak/internal/logx"
)

// ConfigureMenus exposes the main menu next to the message input using each
// messenger's native command interface. It also works without a public webhook.
func ConfigureMenus() {
	if token := strings.TrimSpace(os.Getenv("TG_BOT_TOKEN")); token != "" {
		if err := configureTelegramMenu(token); err != nil {
			logx.Error("Telegram menu configuration failed", err)
		} else {
			log.Printf("menus: Telegram /menu configured")
		}
	}
	if token := strings.TrimSpace(os.Getenv("MAX_BOT_TOKEN")); token != "" {
		if err := configureMaxMenu(token); err != nil {
			logx.Error("MAX menu configuration failed", err)
		} else {
			log.Printf("menus: MAX /menu configured")
		}
	}
}

func configureTelegramMenu(token string) error {
	if !ValidTelegramToken(token) {
		return fmt.Errorf("invalid Telegram bot token format")
	}
	commands := []map[string]string{{"command": "menu", "description": "Главное меню"}}
	// Configure both the default and Russian list so an older language-specific
	// list cannot hide /menu for Russian-speaking users.
	for _, language := range []string{"", "ru"} {
		if err := telegramMenuRequest(token, "setMyCommands", map[string]any{
			"commands": commands, "language_code": language,
		}); err != nil {
			return err
		}
	}
	return telegramMenuRequest(token, "setChatMenuButton", map[string]any{
		"menu_button": map[string]string{"type": "commands"},
	})
}

func telegramMenuRequest(token, method string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	// #nosec G704 -- telegramAPI is a fixed origin; token is validated above
	// and method is one of the hardcoded menu configuration endpoints.
	req, err := http.NewRequest(http.MethodPost, telegramAPI+"/bot"+token+"/"+method, strings.NewReader(string(body)))
	if err != nil {
		return redactTelegramToken(err, token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := telegramHTTPClient.Do(req)
	if err != nil {
		return redactTelegramToken(err, token)
	}
	defer resp.Body.Close()
	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return fmt.Errorf("%s HTTP %d: invalid response", method, resp.StatusCode)
	}
	if resp.StatusCode >= 300 || !result.OK {
		return redactTelegramToken(fmt.Errorf("%s HTTP %d: %s", method, resp.StatusCode, result.Description), token)
	}
	return nil
}

func configureMaxMenu(token string) error {
	body, err := json.Marshal(map[string]any{
		"commands": []map[string]string{{"name": "menu", "description": "Главное меню"}},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPatch, maxAPI+"/me/commands", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := maxHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result struct {
		Commands []struct {
			Name string `json:"name"`
		} `json:"commands"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return fmt.Errorf("MAX commands HTTP %d: invalid response", resp.StatusCode)
	}
	if resp.StatusCode < 300 {
		for _, command := range result.Commands {
			if command.Name == "menu" {
				return nil
			}
		}
	}
	return fmt.Errorf("MAX commands HTTP %d: menu command was not confirmed", resp.StatusCode)
}
