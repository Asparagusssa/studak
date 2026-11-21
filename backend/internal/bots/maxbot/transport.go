package maxbot

import (
	"context"
	"errors"

	"studak/internal/crud"
	"studak/internal/models"
)

// Messenger is the only platform-specific part of the shared dating flow.
// The package name is retained for backwards compatibility with MAX.
type Messenger interface {
	SendContext(context.Context, int64, string, [][]Button) error
	SendPhotoContext(context.Context, int64, string, [][]Button, map[string]string) error
	AnswerCallbackContext(context.Context, string, string) error
}

type PhotoError interface{ PhotoUnavailable() bool }

func (b *Bot) channel() string {
	if b.Channel == "telegram" {
		return "telegram"
	}
	return channelMax
}

func (b *Bot) transport() Messenger {
	if b.Messenger != nil {
		return b.Messenger
	}
	return b.Client
}

func (b *Bot) lookupUser(id int64) (*models.User, error) {
	if b.channel() == "telegram" {
		return crud.GetUserByTelegramID(b.DB, id)
	}
	return crud.GetUserByMaxID(b.DB, id)
}

// Delivery always prefers the receiving user's own platform and never routes
// using a raw numeric ID from the other messenger.
func (b *Bot) destination(peer *models.User) (Messenger, int64, string) {
	if peer == nil {
		return nil, 0, ""
	}
	ids := map[string]*int64{"max": peer.MaxID, "telegram": peer.TelegramID}
	current := b.channel()
	if id := ids[current]; id != nil && *id > 0 {
		return b.transport(), *id, current
	}
	for _, platform := range []string{"max", "telegram"} {
		if id := ids[platform]; id != nil && *id > 0 && b.Peers[platform] != nil {
			return b.Peers[platform], *id, platform
		}
	}
	return nil, 0, ""
}

func (b *Bot) canWrite(peer *models.User) bool {
	client, id, _ := b.destination(peer)
	return client != nil && id > 0
}

func (b *Bot) peerEngine(channel string, client Messenger) *Bot {
	copy := *b
	copy.Channel = channel
	copy.Messenger = client
	return &copy
}

func (b *Bot) notify(peer *models.User, text string, buttons [][]Button) error {
	client, id, _ := b.destination(peer)
	if client == nil {
		return errors.New("recipient has no configured messenger")
	}
	return client.SendContext(b.context(), id, text, buttons)
}

func (b *Bot) photoUpdate(token *string) crud.UpdateUserInput {
	if b.channel() == "telegram" {
		return crud.UpdateUserInput{TelegramPhotoID: token}
	}
	return crud.UpdateUserInput{MaxPhotoToken: token}
}

func (b *Bot) photoPayload(user *models.User) map[string]string {
	if b.channel() == "telegram" && user.TelegramPhotoID != "" {
		return map[string]string{"token": user.TelegramPhotoID}
	}
	if b.channel() == "max" && user.MaxPhotoToken != "" {
		return map[string]string{"token": user.MaxPhotoToken}
	}
	if user.PhotoURL != "" {
		return map[string]string{"url": user.PhotoURL}
	}
	return nil
}

// HandleContext binds a webhook's timeout to DB and platform calls.
func (b *Bot) HandleContext(ctx context.Context, u Update) error {
	copy := *b
	copy.ctx = ctx
	copy.DB = b.DB.WithContext(ctx)
	return copy.HandleUpdate(u)
}

var _ Messenger = (*Client)(nil)
