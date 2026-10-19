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
)

var (
	telegramAPI = "https://api.telegram.org"
	maxAPI      = "https://platform-api2.max.ru"
)

var httpClient = &http.Client{Timeout: 20 * time.Second}

// RegisterWebhooks points Telegram and Max bots at this process via the
// public HTTPS base URL (typically an ngrok tunnel written by start.sh).
// Missing tokens or an empty base URL are skipped with a log line; a failed
// registration does not stop the HTTP server.
func RegisterWebhooks(baseURL string) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		log.Printf("webhooks: WEBHOOK_BASE_URL is empty, skip registration")
		return
	}
	if !strings.HasPrefix(baseURL, "https://") {
		log.Printf("webhooks: WEBHOOK_BASE_URL must be https, got %q", baseURL)
		return
	}

	if token := strings.TrimSpace(os.Getenv("TG_BOT_TOKEN")); token != "" {
		if err := registerTelegram(token, strings.TrimSpace(os.Getenv("TG_WEBHOOK_SECRET")), baseURL+"/webhook/tg"); err != nil {
			log.Printf("webhooks: telegram: %v", err)
		}
	} else {
		log.Printf("webhooks: TG_BOT_TOKEN is empty, skip Telegram")
	}

	if token := strings.TrimSpace(os.Getenv("MAX_BOT_TOKEN")); token != "" {
		if err := registerMax(token, baseURL+"/webhook/max"); err != nil {
			log.Printf("webhooks: max: %v", err)
		}
	} else {
		log.Printf("webhooks: MAX_BOT_TOKEN is empty, skip Max")
	}
}

func registerTelegram(token, secret, hookURL string) error {
	payload := map[string]any{
		"url": hookURL,
	}
	if secret != "" {
		payload["secret_token"] = secret
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, telegramAPI+"/bot"+token+"/setWebhook", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
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

	resp, err := httpClient.Do(req)
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
	if resp.StatusCode >= 300 {
		msg := parsed.Message
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
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

	resp, err := httpClient.Do(req)
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
		delResp, err := httpClient.Do(del)
		if err != nil {
			return err
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(delResp.Body, 1<<20))
		delResp.Body.Close()
		if delResp.StatusCode >= 300 {
			return fmt.Errorf("unsubscribe %s HTTP %d", sub.URL, delResp.StatusCode)
		}
		log.Printf("webhooks: max unsubscribed %s", sub.URL)
	}
	return nil
}
