package maxbot

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

type Update struct {
	UpdateType string    `json:"update_type"`
	Timestamp  int64     `json:"timestamp"`
	ChatID     int64     `json:"chat_id"`
	User       *User     `json:"user"`
	Message    *Message  `json:"message"`
	Callback   *Callback `json:"callback"`
}

type User struct {
	UserID    int64  `json:"user_id"`
	Name      string `json:"name"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
	IsBot     bool   `json:"is_bot"`
}

func (u User) MaxID() (int64, error) {
	if u.UserID <= 0 {
		return 0, errors.New("MAX user_id is missing")
	}
	return u.UserID, nil
}

func (u User) DisplayName() string {
	if n := strings.TrimSpace(u.Name); n != "" {
		return n
	}
	return strings.TrimSpace(u.FirstName)
}

type Message struct {
	Sender    User      `json:"sender"`
	Recipient Recipient `json:"recipient"`
	Body      Body      `json:"body"`
}

type Recipient struct {
	ChatID   int64  `json:"chat_id"`
	ChatType string `json:"chat_type"`
	UserID   int64  `json:"user_id"`
}

type Body struct {
	Mid         string       `json:"mid"`
	Text        string       `json:"text"`
	Attachments []Attachment `json:"attachments"`
}

type Attachment struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func (b Body) imageToken() (token string, count int) {
	for _, a := range b.Attachments {
		if a.Type != "image" {
			continue
		}
		count++
		var image struct {
			Token string `json:"token"`
		}
		if json.Unmarshal(a.Payload, &image) == nil {
			token = strings.TrimSpace(image.Token)
		}
	}
	return token, count
}

type Callback struct {
	CallbackID string `json:"callback_id"`
	Payload    string `json:"payload"`
	User       User   `json:"user"`
}

func ParseUpdates(body []byte) []Update {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil
	}
	if body[0] == '[' {
		var many []Update
		if err := json.Unmarshal(body, &many); err != nil {
			return nil
		}
		return many
	}

	var envelope struct {
		UpdateType string   `json:"update_type"`
		Updates    []Update `json:"updates"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil
	}
	if envelope.UpdateType != "" {
		var one Update
		if err := json.Unmarshal(body, &one); err != nil {
			return nil
		}
		return []Update{one}
	}
	return envelope.Updates
}
