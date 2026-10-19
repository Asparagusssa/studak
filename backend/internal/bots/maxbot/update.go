package maxbot

import (
	"bytes"
	"encoding/json"
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
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
	IsBot     bool   `json:"is_bot"`
}

func (u User) MaxID() int64 {
	if u.UserID != 0 {
		return u.UserID
	}
	return u.ID
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
	Mid  string `json:"mid"`
	Text string `json:"text"`
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
