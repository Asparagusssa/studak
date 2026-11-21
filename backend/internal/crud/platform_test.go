package crud

import (
	"errors"
	"testing"
	"time"

	"studak/internal/models"
)

func TestMessengerPoolsExcludeCrossPlatformAndLegacyMatches(t *testing.T) {
	now := time.Now().UTC()
	db := feedDB(t, &now)
	tgID, tgPeerID, maxID, maxPeerID := int64(11), int64(12), int64(11), int64(12)
	create := func(in CreateUserInput) *models.User {
		t.Helper()
		u, err := CreateUser(db, in)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	tg := create(CreateUserInput{Name: "ТГ", Age: 21, TelegramID: &tgID})
	tgPeer := create(CreateUserInput{Name: "ТГ собеседник", Age: 21, TelegramID: &tgPeerID})
	max := create(CreateUserInput{Name: "MAX", Age: 21, MaxID: &maxID, MaxContact: "+79991234567"})
	maxPeer := create(CreateUserInput{Name: "MAX собеседник", Age: 21, MaxID: &maxPeerID, MaxContact: "https://t.me/max_peer"})
	for viewer, want := range map[uint]uint{tg.ID: tgPeer.ID, max.ID: maxPeer.ID} {
		got, err := GetNextProfile(db, viewer)
		if err != nil || got.ID != want {
			t.Fatalf("mixed messenger pools: %+v, %v", got, err)
		}
	}
	if _, err := SetLike(db, tg.ID, max.ID, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-platform like accepted: %v", err)
	}
	if err := BlockUser(db, tg.ID, max.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale cross-platform block accepted: %v", err)
	}
	if err := db.Create(&models.UserBlock{BlockerID: tg.ID, BlockedID: max.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if got, err := ListBlockedUsers(db, tg.ID, 0, 10); err != nil || len(got) != 0 {
		t.Fatalf("legacy cross-platform profile visible in block list: %+v, %v", got, err)
	}
	// Simulate reactions created before profiles were separated.
	for _, pair := range [][2]uint{{tg.ID, max.ID}, {max.ID, tg.ID}} {
		if err := db.Create(&models.Like{FromUserID: pair[0], ToUserID: pair[1], IsLike: true, IsMutual: true}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, viewer := range []uint{tg.ID, max.ID} {
		if got, err := ListMatches(db, viewer, 0, 10); err != nil || len(got) != 0 {
			t.Fatalf("legacy cross-platform match visible: %+v, %v", got, err)
		}
		if _, err := NextIncomingLike(db, viewer); !errors.Is(err, ErrNoProfiles) {
			t.Fatalf("legacy cross-platform like visible: %v", err)
		}
	}
	if _, err := MatchedUser(db, tg.ID, max.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("legacy contact accessible: %v", err)
	}
	if _, err := ReactOnce(db, max.ID, tg.ID, true, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale cross-platform callback accepted: %v", err)
	}
}

func TestMaxProfilesWithoutContactsStayOutOfDiscovery(t *testing.T) {
	now := time.Now().UTC()
	db := feedDB(t, &now)
	id, oldID := int64(21), int64(22)
	viewer, err := CreateUser(db, CreateUserInput{Name: "MAX", Age: 21, MaxID: &id, MaxContact: "+79991234567"})
	if err != nil {
		t.Fatal(err)
	}
	old, err := CreateUser(db, CreateUserInput{Name: "Старая анкета", Age: 21, MaxID: &oldID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := GetNextProfile(db, viewer.ID); !errors.Is(err, ErrNoProfiles) {
		t.Fatalf("incomplete MAX profile visible: %v", err)
	}
	if _, err := GetNextProfile(db, old.ID); !errors.Is(err, ErrContactRequired) {
		t.Fatalf("incomplete viewer can browse: %v", err)
	}
	if _, err := SetLike(db, viewer.ID, old.ID, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("incomplete peer can be liked: %v", err)
	}
	contact := "https://max.ru/u/oldOpaque"
	if _, err := UpdateUser(db, old.ID, UpdateUserInput{MaxContact: &contact}); err != nil {
		t.Fatal(err)
	}
	if got, err := GetNextProfile(db, viewer.ID); err != nil || got.ID != old.ID {
		t.Fatalf("completed MAX profile missing: %+v, %v", got, err)
	}
}
