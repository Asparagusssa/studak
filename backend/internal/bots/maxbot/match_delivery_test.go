package maxbot

import (
	"context"
	"errors"
	"path/filepath"
	"studak/internal/crud"
	"studak/internal/models"
	"testing"
)

type matchDeliveryMessenger struct {
	fail      int64
	delivered map[int64]int
}

func (m *matchDeliveryMessenger) SendContext(_ context.Context, id int64, _ string, _ [][]Button) error {
	if id == m.fail {
		return errors.New("messenger unavailable")
	}
	m.delivered[id]++
	return nil
}
func (m *matchDeliveryMessenger) SendPhotoContext(ctx context.Context, id int64, text string, buttons [][]Button, _ map[string]string) error {
	return m.SendContext(ctx, id, text, buttons)
}
func (m *matchDeliveryMessenger) AnswerCallbackContext(context.Context, string, string) error {
	return nil
}

func TestPendingMutualityQueryAndDeliveryProgress(t *testing.T) {
	db, err := testDB(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	first, second := int64(601), int64(602)
	a, err := crud.CreateUser(db, crud.CreateUserInput{Name: "А", Age: 21, TelegramID: &first})
	must(err)
	b, err := crud.CreateUser(db, crud.CreateUserInput{Name: "Б", Age: 20, TelegramID: &second})
	must(err)
	_, err = crud.ReactOnce(db, a.ID, b.ID, true, false)
	must(err)
	_, err = crud.ReactOnce(db, b.ID, a.ID, true, false)
	must(err)
	messenger := &matchDeliveryMessenger{fail: second, delivered: make(map[int64]int)}
	bot := &Bot{DB: db, Channel: "telegram", Messenger: messenger}
	if err := bot.deliverPendingMatch(a.ID, b.ID); err == nil {
		t.Fatal("ignored delivery failure")
	}
	pending, err := crud.PendingMatches(db, "telegram")
	must(err)
	if len(pending) != 1 || pending[0].FromUserID != b.ID {
		t.Fatalf("wrong pending recipients: %+v", pending)
	}
	cross, err := crud.PendingMatches(db, "max")
	must(err)
	if len(cross) != 0 {
		t.Fatal("pending notifications crossed platforms")
	}
	messenger.fail = 0
	for _, pair := range pending {
		must(bot.deliverPendingMatch(pair.FromUserID, pair.ToUserID))
	}
	pending, err = crud.PendingMatches(db, "telegram")
	must(err)
	var rows int64
	must(db.Model(&models.Like{}).Count(&rows).Error)
	if len(pending) != 0 || rows != 0 || messenger.delivered[first] != 1 || messenger.delivered[second] != 1 {
		t.Fatalf("wrong final state: rows=%d sent=%v", rows, messenger.delivered)
	}
}
