// Package telegrambot adapts Telegram webhooks and keyboards to the shared
// dating engine. It does not maintain a second set of dating rules.
package telegrambot

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"gorm.io/gorm"
	"studak/internal/bots/maxbot"
	"studak/internal/logx"
)

type Bot struct {
	Engine *maxbot.Bot
	Client *Client
	Secret string
}

func New(db *gorm.DB, token, secret string) *Bot {
	client := NewClient(token)
	engine := &maxbot.Bot{DB: db, Channel: "telegram", Messenger: client}
	return &Bot{Engine: engine, Client: client, Secret: strings.TrimSpace(secret)}
}

type Actor struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	IsBot     bool   `json:"is_bot"`
}
type Message struct {
	ID   int64  `json:"message_id"`
	From *Actor `json:"from"`
	Chat struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	} `json:"chat"`
	Text         string `json:"text"`
	Caption      string `json:"caption"`
	MediaGroupID string `json:"media_group_id"`
	Photo        []struct {
		FileID string `json:"file_id"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	} `json:"photo"`
	Document  json.RawMessage `json:"document"`
	Video     json.RawMessage `json:"video"`
	Voice     json.RawMessage `json:"voice"`
	Audio     json.RawMessage `json:"audio"`
	Sticker   json.RawMessage `json:"sticker"`
	Animation json.RawMessage `json:"animation"`
	VideoNote json.RawMessage `json:"video_note"`
	Contact   json.RawMessage `json:"contact"`
	Location  json.RawMessage `json:"location"`
}
type Update struct {
	ID       *int64   `json:"update_id"`
	Message  *Message `json:"message"`
	Callback *struct {
		ID      string   `json:"id"`
		From    Actor    `json:"from"`
		Message *Message `json:"message"`
		Data    string   `json:"data"`
	} `json:"callback_query"`
}

func actor(a Actor) maxbot.User {
	return maxbot.User{UserID: a.ID, FirstName: strings.TrimSpace(a.FirstName + " " + a.LastName), IsBot: a.IsBot}
}

func (b *Bot) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	secret := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
	if b.Secret == "" || subtle.ConstantTimeCompare([]byte(secret), []byte(b.Secret)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var update Update
	d := json.NewDecoder(r.Body)
	if err := d.Decode(&update); err != nil {
		http.Error(w, "invalid update", http.StatusBadRequest)
		return
	}
	var trailing any
	if err := d.Decode(&trailing); !errors.Is(err, io.EOF) || update.ID == nil || *update.ID < 0 {
		http.Error(w, "invalid update", http.StatusBadRequest)
		return
	}
	event, ok := normalize(update)
	if !ok {
		w.WriteHeader(http.StatusOK)
		return
	}
	if err := b.Engine.HandleContext(r.Context(), event); err != nil {
		logx.Error("Telegram webhook processing failed", err)
		http.Error(w, "update failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func normalize(u Update) (maxbot.Update, bool) {
	if cb := u.Callback; cb != nil {
		if cb.From.IsBot || cb.From.ID <= 0 || cb.Message == nil || cb.Message.Chat.Type != "private" || cb.Message.Chat.ID != cb.From.ID || cb.Data == "" {
			return maxbot.Update{}, false
		}
		return maxbot.Update{UpdateType: "message_callback", Callback: &maxbot.Callback{CallbackID: cb.ID, User: actor(cb.From), Payload: cb.Data},
			Message: &maxbot.Message{Recipient: maxbot.Recipient{ChatType: "dialog"}}}, true
	}
	m := u.Message
	if m == nil || m.ID <= 0 || m.From == nil || m.From.IsBot || m.From.ID <= 0 || m.Chat.Type != "private" || m.Chat.ID != m.From.ID {
		return maxbot.Update{}, false
	}
	body := maxbot.Body{Mid: fmt.Sprintf("tg:%d:%d", m.Chat.ID, m.ID), Text: m.Text}
	if body.Text == "" {
		body.Text = m.Caption
	}
	if strings.HasPrefix(body.Text, "/start ") {
		body.Text = "/start"
	}
	if len(m.Photo) > 0 {
		best := m.Photo[0]
		for _, photo := range m.Photo {
			if int64(photo.Width)*int64(photo.Height) > int64(best.Width)*int64(best.Height) {
				best = photo
			}
		}
		raw, _ := json.Marshal(map[string]string{"token": best.FileID})
		body.Attachments = append(body.Attachments, maxbot.Attachment{Type: "image", Payload: raw})
		if m.MediaGroupID != "" {
			// An album is several updates. Reject the album rather than publish
			// its first item and treat the rest as unrelated user messages.
			body.Attachments = append(body.Attachments, maxbot.Attachment{Type: "image", Payload: raw})
		}
	}
	for _, attachment := range []json.RawMessage{m.Document, m.Video, m.Voice, m.Audio, m.Sticker, m.Animation, m.VideoNote, m.Contact, m.Location} {
		if len(attachment) > 0 && string(attachment) != "null" {
			body.Attachments = append(body.Attachments, maxbot.Attachment{Type: "unsupported"})
		}
	}
	return maxbot.Update{UpdateType: "message_created", Message: &maxbot.Message{Sender: actor(*m.From),
		Recipient: maxbot.Recipient{ChatType: "dialog"}, Body: body}}, true
}
