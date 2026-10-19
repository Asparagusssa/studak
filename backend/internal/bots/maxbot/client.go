package maxbot

import (
	"bytes"
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
		HTTP:    &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *Client) Send(userID int64, text string, keyboard [][]Button) error {
	if c == nil || c.Token == "" {
		return fmt.Errorf("max token is empty")
	}
	body := map[string]any{"text": text}
	if len(keyboard) > 0 {
		body["attachments"] = []any{
			map[string]any{
				"type": "inline_keyboard",
				"payload": map[string]any{
					"buttons": keyboard,
				},
			},
		}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	endpoint := c.base() + "/messages?user_id=" + strconv.FormatInt(userID, 10)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.Token)
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

func (c *Client) AnswerCallback(callbackID, notification string) error {
	if c == nil || c.Token == "" || strings.TrimSpace(callbackID) == "" {
		return nil
	}
	payload := map[string]any{}
	if notification != "" {
		payload["notification"] = notification
	}
	raw, _ := json.Marshal(payload)
	endpoint := c.base() + "/answers?callback_id=" + url.QueryEscape(callbackID)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.Token)
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

func (c *Client) do(req *http.Request) error {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("max API %s %s: HTTP %d %s", req.Method, req.URL.Path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func (c *Client) base() string {
	if c != nil && c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return defaultAPI
}
