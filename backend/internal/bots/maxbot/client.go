package maxbot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultAPI = "https://platform-api2.max.ru"

type Button struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Payload string `json:"payload,omitempty"`
	URL     string `json:"url,omitempty"`
}

func CallbackButton(text, payload string) Button {
	return Button{Type: "callback", Text: text, Payload: payload}
}

type Client struct {
	Token   string
	BaseURL string
	HTTP    *http.Client
}

func NewClient(token string) *Client {
	return &Client{
		Token:   strings.TrimSpace(token),
		BaseURL: defaultAPI,
		HTTP:    NewHTTPClient(20 * time.Second),
	}
}

func (c *Client) Send(userID int64, text string, keyboard [][]Button) error {
	return c.SendContext(context.Background(), userID, text, keyboard)
}

func (c *Client) SendContext(ctx context.Context, userID int64, text string, keyboard [][]Button) error {
	return c.sendMessage(ctx, userID, text, keyboard, nil)
}

func (c *Client) SendPhotoContext(ctx context.Context, userID int64, text string, keyboard [][]Button, photo map[string]string) error {
	return c.sendMessage(ctx, userID, text, keyboard, photo)
}

func (c *Client) sendMessage(ctx context.Context, userID int64, text string, keyboard [][]Button, photo map[string]string) error {
	if c == nil || c.Token == "" {
		return fmt.Errorf("max token is empty")
	}
	if userID <= 0 {
		return fmt.Errorf("invalid MAX user_id")
	}
	body := map[string]any{"text": text}
	attachments := make([]any, 0, 2)
	if len(photo) > 0 {
		attachments = append(attachments, map[string]any{"type": "image", "payload": photo})
	}
	if len(keyboard) > 0 {
		attachments = append(attachments, map[string]any{
			"type":    "inline_keyboard",
			"payload": map[string]any{"buttons": keyboard},
		})
	}
	if len(attachments) > 0 {
		body["attachments"] = attachments
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	endpoint := c.base() + "/messages?user_id=" + strconv.FormatInt(userID, 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.Token)
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

func (c *Client) AnswerCallback(callbackID, notification string) error {
	return c.AnswerCallbackContext(context.Background(), callbackID, notification)
}

func (c *Client) AnswerCallbackContext(ctx context.Context, callbackID, notification string) error {
	if c == nil || c.Token == "" || strings.TrimSpace(callbackID) == "" {
		return nil
	}
	payload := map[string]any{}
	if notification != "" {
		payload["notification"] = notification
	}
	raw, _ := json.Marshal(payload)
	endpoint := c.base() + "/answers?callback_id=" + url.QueryEscape(callbackID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.Token)
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

type apiError struct {
	Method, Path, Code string
	Status             int
}

func (e *apiError) PhotoUnavailable() bool {
	return strings.HasPrefix(e.Code, "attachment.") || e.Status == http.StatusUnsupportedMediaType
}

func (e *apiError) Error() string {
	return fmt.Sprintf("max API %s %s: HTTP %d %s", e.Method, e.Path, e.Status, e.Code)
}

func (c *Client) do(req *http.Request) error {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	reader := io.LimitReader(resp.Body, 1<<20)
	if resp.StatusCode >= 300 {
		var failure struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(reader).Decode(&failure)
		return &apiError{Method: req.Method, Path: req.URL.Path, Status: resp.StatusCode, Code: failure.Code}
	}
	var result struct {
		Success *bool  `json:"success"`
		Code    string `json:"code"`
	}
	if err := json.NewDecoder(reader).Decode(&result); err != nil {
		return fmt.Errorf("max API %s: invalid JSON response", req.URL.Path)
	}
	if result.Success != nil && !*result.Success {
		return &apiError{Method: req.Method, Path: req.URL.Path, Status: resp.StatusCode, Code: result.Code}
	}
	return nil
}

func (c *Client) base() string {
	if c != nil && c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return defaultAPI
}
