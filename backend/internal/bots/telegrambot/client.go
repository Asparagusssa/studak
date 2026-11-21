package telegrambot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"studak/internal/bots/maxbot"
)

type Client struct {
	Token   string
	BaseURL string
	HTTP    *http.Client
}

func NewClient(token string) *Client {
	return &Client{
		Token: strings.TrimSpace(token), BaseURL: "https://api.telegram.org",
		HTTP: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
}

type button struct {
	Text string `json:"text"`
	Data string `json:"callback_data,omitempty"`
	URL  string `json:"url,omitempty"`
}

func keyboard(rows [][]maxbot.Button) map[string]any {
	result := make([][]button, 0, len(rows))
	for _, row := range rows {
		buttons := make([]button, 0, len(row))
		for _, b := range row {
			buttons = append(buttons, button{Text: b.Text, Data: b.Payload, URL: b.URL})
		}
		result = append(result, buttons)
	}
	return map[string]any{"inline_keyboard": result}
}

func (c *Client) SendContext(ctx context.Context, id int64, text string, rows [][]maxbot.Button) error {
	body := map[string]any{"chat_id": id, "text": text}
	if len(rows) > 0 {
		body["reply_markup"] = keyboard(rows)
	}
	return c.call(ctx, "sendMessage", body, false)
}

func (c *Client) SendPhotoContext(ctx context.Context, id int64, text string, rows [][]maxbot.Button, photo map[string]string) error {
	source := photo["token"]
	if source == "" {
		source = photo["url"]
	}
	if source == "" {
		return c.SendContext(ctx, id, text, rows)
	}
	body := map[string]any{"chat_id": id, "photo": source}
	if len(rows) > 0 {
		body["reply_markup"] = keyboard(rows)
	}
	// Telegram captions are limited to 1024 characters, message text to 4096.
	// Keep a long description intact as a second message with the same actions.
	long := utf8.RuneCountInString(text) > 1024
	if !long {
		body["caption"] = text
	} else {
		delete(body, "reply_markup")
	}
	if err := c.call(ctx, "sendPhoto", body, true); err != nil {
		return err
	}
	if long {
		return c.SendContext(ctx, id, text, rows)
	}
	return nil
}

func (c *Client) AnswerCallbackContext(ctx context.Context, id, text string) error {
	if id == "" {
		return nil
	}
	return c.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": id, "text": text}, false)
}

type apiError struct {
	Method string
	Status int
	Photo  bool
}

func (e *apiError) Error() string {
	return fmt.Sprintf("Telegram API %s: HTTP/API %d", e.Method, e.Status)
}
func (e *apiError) PhotoUnavailable() bool { return e.Photo && e.Status == http.StatusBadRequest }

func (c *Client) call(ctx context.Context, method string, body any, photo bool) error {
	if c == nil || c.Token == "" {
		return errors.New("Telegram token is empty")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/bot"+c.Token+"/"+method, bytes.NewReader(raw))
	if err != nil {
		return errors.New("Telegram request configuration error")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		// net/http errors contain the full request URL, including the token.
		return errors.New("Telegram connection failed")
	}
	defer resp.Body.Close()
	var result struct {
		OK   bool `json:"ok"`
		Code int  `json:"error_code"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return errors.New("Telegram invalid JSON response")
	}
	if resp.StatusCode >= 300 || !result.OK {
		code := resp.StatusCode
		if result.Code != 0 {
			code = result.Code
		}
		return &apiError{Method: method, Status: code, Photo: photo}
	}
	return nil
}

var _ maxbot.Messenger = (*Client)(nil)
