package bots

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"studak/internal/bots/maxbot"
	"studak/internal/logx"
)

var (
	telegramAPI = "https://api.telegram.org"
	maxAPI      = "https://platform-api2.max.ru"
)

var telegramHTTPClient = &http.Client{
	Timeout: 20 * time.Second,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

var maxHTTPClient = maxbot.NewHTTPClient(20 * time.Second)

// RegisterWebhooks points Telegram and Max bots at this process via the
// public HTTPS base URL (typically an ngrok tunnel written by start.sh).
// Missing tokens or an empty base URL are skipped with a log line; a failed
// registration does not stop the HTTP server.
func RegisterWebhooks(baseURL string) {
	var err error
	baseURL, err = normalizeWebhookBaseURL(baseURL)
	if err != nil {
		logx.Error("webhook configuration invalid", err)
		return
	}
	if baseURL == "" {
		log.Printf("webhooks: WEBHOOK_BASE_URL is empty, skip registration")
		return
	}

	if token := strings.TrimSpace(os.Getenv("TG_BOT_TOKEN")); token != "" {
		if err := registerTelegram(token, strings.TrimSpace(os.Getenv("TG_WEBHOOK_SECRET")), baseURL+"/webhook/tg"); err != nil {
			logx.Error("Telegram webhook registration failed", err)
		}
	} else {
		log.Printf("webhooks: TG_BOT_TOKEN is empty, skip Telegram")
	}

	if token := strings.TrimSpace(os.Getenv("MAX_BOT_TOKEN")); token != "" {
		if err := registerMax(token, baseURL+"/webhook/max"); err != nil {
			logx.Error("MAX webhook registration failed", err)
		}
	} else {
		log.Printf("webhooks: MAX_BOT_TOKEN is empty, skip Max")
	}
}

func normalizeWebhookBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", fmt.Errorf("WEBHOOK_BASE_URL must be an absolute HTTPS URL")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("WEBHOOK_BASE_URL must not contain credentials, query parameters or a fragment")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}

func registerTelegram(token, secret, hookURL string) error {
	if !ValidTelegramToken(token) {
		return fmt.Errorf("invalid Telegram bot token format")
	}
	payload := map[string]any{
		"url": hookURL,
	}
	if secret != "" {
		payload["secret_token"] = secret
	}
	body, _ := json.Marshal(payload)
	// #nosec G704 -- telegramAPI is a fixed production origin and the only
	// variable URL segment is constrained by ValidTelegramToken above.
	req, err := http.NewRequest(http.MethodPost, telegramAPI+"/bot"+token+"/setWebhook", strings.NewReader(string(body)))
	if err != nil {
		return redactTelegramToken(err, token)
	}
	req.Header.Set("Content-Type", "application/json")

	// #nosec G704 -- the request origin is fixed and validated above.
	resp, err := telegramHTTPClient.Do(req)
	if err != nil {
		return redactTelegramToken(err, token)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var parsed struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	_ = json.Unmarshal(raw, &parsed)
	if resp.StatusCode >= 300 || !parsed.OK {
		desc := parsed.Description
		if desc == "" {
			desc = strings.TrimSpace(string(raw))
		}
		return fmt.Errorf("setWebhook HTTP %d: %s", resp.StatusCode, desc)
	}
	log.Printf("webhooks: telegram -> %s", hookURL)
	return nil
}

func redactTelegramToken(err error, token string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), token, "[REDACTED]"))
}

// ValidTelegramToken accepts Telegram's documented numeric bot ID followed by
// a colon and a URL-safe secret. It rejects URL/header delimiter injection.
func ValidTelegramToken(token string) bool {
	id, secret, ok := strings.Cut(strings.TrimSpace(token), ":")
	if !ok || id == "" || secret == "" {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	for _, r := range secret {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func registerMax(token, hookURL string) error {
	if err := clearMaxSubscription(token, hookURL); err != nil {
		return err
	}

	payload := map[string]any{
		"url":          hookURL,
		"update_types": []string{"message_created", "bot_started", "message_callback"},
	}
	if secret := strings.TrimSpace(os.Getenv("MAX_WEBHOOK_SECRET")); secret != "" {
		payload["secret"] = secret
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, maxAPI+"/subscriptions", strings.NewReader(string(body)))
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
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var parsed struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &parsed)
	if resp.StatusCode >= 300 || !parsed.Success {
		msg := parsed.Message
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		if msg == "" {
			msg = "MAX rejected the subscription"
		}
		return fmt.Errorf("subscriptions HTTP %d: %s", resp.StatusCode, msg)
	}
	log.Printf("webhooks: max -> %s", hookURL)
	return nil
}

// clearMaxSubscription replaces only this exact endpoint. It deliberately does
// not remove subscriptions belonging to another environment using the same bot.
func clearMaxSubscription(token, hookURL string) error {
	req, err := http.NewRequest(http.MethodGet, maxAPI+"/subscriptions", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", token)

	resp, err := maxHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("list subscriptions HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var parsed struct {
		Subscriptions []struct {
			URL string `json:"url"`
		} `json:"subscriptions"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return fmt.Errorf("list subscriptions: %w", err)
	}

	for _, sub := range parsed.Subscriptions {
		if strings.TrimSpace(sub.URL) != hookURL {
			continue
		}
		delURL := maxAPI + "/subscriptions?url=" + url.QueryEscape(sub.URL)
		del, err := http.NewRequest(http.MethodDelete, delURL, nil)
		if err != nil {
			return err
		}
		del.Header.Set("Authorization", token)
		delResp, err := maxHTTPClient.Do(del)
		if err != nil {
			return err
		}
		if _, err := io.Copy(io.Discard, io.LimitReader(delResp.Body, 1<<20)); err != nil {
			_ = delResp.Body.Close()
			return fmt.Errorf("read unsubscribe response: %w", err)
		}
		if err := delResp.Body.Close(); err != nil {
			return fmt.Errorf("close unsubscribe response: %w", err)
		}
		if delResp.StatusCode >= 300 {
			return fmt.Errorf("unsubscribe %s HTTP %d", sub.URL, delResp.StatusCode)
		}
		log.Printf("webhooks: max unsubscribed %s", sub.URL)
	}
	return nil
}
