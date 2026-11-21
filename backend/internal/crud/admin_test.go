package crud

import (
	"errors"
	"studak/internal/models"
	"testing"
	"time"
)

func TestLaunchAndAdministrativeBan(t *testing.T) {
	now := time.Now()
	db := feedDB(t, &now)
	me := feedUser(t, db, "А", "male", "all", 21)
	peer := feedUser(t, db, "Б", "female", "all", 20)
	if err := db.Model(&models.ServiceState{}).Where("id = ?", 1).Update("started", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := GetNextProfile(db, me.ID); !errors.Is(err, ErrNotLaunched) {
		t.Fatalf("closed feed: %v", err)
	}
	if _, err := NextIncomingLike(db, me.ID); !errors.Is(err, ErrNotLaunched) {
		t.Fatalf("closed inbox: %v", err)
	}
	if _, err := SetLike(db, me.ID, peer.ID, true); !errors.Is(err, ErrNotLaunched) {
		t.Fatalf("closed reaction: %v", err)
	}
	name := "Новое имя"
	if _, err := UpdateUser(db, me.ID, UpdateUserInput{Name: &name}); err != nil {
		t.Fatal("editing before launch", err)
	}
	if err := StartDating(db); err != nil {
		t.Fatal(err)
	}
	if next, err := GetNextProfile(db, me.ID); err != nil || next.ID != peer.ID {
		t.Fatalf("launched feed: %v", err)
	}
	if err := SetUserBanned(db, peer.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := GetNextProfile(db, me.ID); !errors.Is(err, ErrNoProfiles) {
		t.Fatalf("banned profile visible: %v", err)
	}
	if _, err := GetNextProfile(db, peer.ID); !errors.Is(err, ErrBanned) {
		t.Fatalf("banned user browsing: %v", err)
	}
	if _, err := SetLike(db, me.ID, peer.ID, true); !errors.Is(err, ErrBanned) {
		t.Fatalf("banned user received reaction: %v", err)
	}
	if _, err := UpdateUser(db, peer.ID, UpdateUserInput{Name: &name}); !errors.Is(err, ErrBanned) {
		t.Fatalf("banned user editing: %v", err)
	}
	if err := SetUserBanned(db, peer.ID, false); err != nil {
		t.Fatal(err)
	}
	if next, err := GetNextProfile(db, me.ID); err != nil || next.ID != peer.ID {
		t.Fatalf("unbanned profile absent: %v", err)
	}
}

func TestResetOnlyOutgoingSkips(t *testing.T) {
	now := time.Now()
	db := feedDB(t, &now)
	a := feedUser(t, db, "А", "male", "all", 21)
	b := feedUser(t, db, "Б", "female", "all", 20)
	c := feedUser(t, db, "В", "female", "all", 21)
	for _, reaction := range []struct {
		from, to uint
		like     bool
	}{{a.ID, b.ID, true}, {b.ID, a.ID, true}, {a.ID, c.ID, false}, {c.ID, a.ID, false}} {
		if _, err := SetLike(db, reaction.from, reaction.to, reaction.like); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&models.UserBlock{BlockerID: b.ID, BlockedID: c.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if n, err := ResetSkips(db, a.ID); err != nil || n != 1 {
		t.Fatalf("user reset: %d / %v", n, err)
	}
	var skipped, liked, blocks int64
	db.Model(&models.Like{}).Where("is_like = ?", false).Count(&skipped)
	db.Model(&models.Like{}).Where("is_like = ?", true).Count(&liked)
	db.Model(&models.UserBlock{}).Count(&blocks)
	if skipped != 1 || liked != 2 || blocks != 1 {
		t.Fatalf("reset damaged retained data: skips=%d likes=%d blocks=%d", skipped, liked, blocks)
	}
	if n, err := ResetSkips(db, 0); err != nil || n != 1 {
		t.Fatalf("global reset: %d / %v", n, err)
	}
}

func TestUniversityCountsStayInsideMessenger(t *testing.T) {
	now := time.Now()
	db := feedDB(t, &now)
	uni := models.University{Name: "Тестовый университет", ShortName: "ТУ"}
	if err := db.Create(&uni).Error; err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct {
		id             int64
		channel        string
		paused, banned bool
	}{{1, "telegram", false, false}, {2, "telegram", true, false}, {3, "telegram", false, true}, {4, "max", false, false}} {
		u := models.User{Name: "Студент", Age: 20, WebUUID: entry.channel + string(rune('a'+entry.id)), MyUniversityID: &uni.ID, Paused: entry.paused, Banned: entry.banned, SearchMode: "all"}
		if entry.channel == "telegram" {
			u.TelegramID = &entry.id
		} else {
			u.MaxID = &entry.id
		}
		if err := db.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
	}
	for channel, want := range map[string]int64{"telegram": 2, "max": 1} {
		got, err := UniversityRegistrations(db, channel, []uint{uni.ID, 999})
		if err != nil || got[uni.ID] != want || got[999] != 0 {
			t.Fatalf("%s counts: %v / %v", channel, got, err)
		}
	}
}

func TestMutualityIsDeletedAfterRetryingOnlyUnsentRecipient(t *testing.T) {
	now := time.Now()
	db := feedDB(t, &now)
	a := feedUser(t, db, "А", "male", "all", 21)
	b := feedUser(t, db, "Б", "female", "all", 20)
	if _, err := SetLike(db, a.ID, b.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := SetLike(db, b.ID, a.ID, true); err != nil {
		t.Fatal(err)
	}
	sent := map[uint]int{}
	fail := true
	send := func(recipient, peer *models.User) error {
		if recipient.ID == b.ID && fail {
			return errors.New("temporary messenger failure")
		}
		if recipient.Name == "" || peer.Name == "" {
			t.Fatal("notification missing profile data")
		}
		sent[recipient.ID]++
		return nil
	}
	if err := DeliverPendingMatch(db, a.ID, b.ID, send); err == nil {
		t.Fatal("ignored transport failure")
	}
	var rows []models.Like
	if err := db.Order("from_user_id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || !rows[0].MatchNotified || rows[1].MatchNotified {
		t.Fatalf("lost delivery progress: %+v", rows)
	}
	if _, err := GetNextProfile(db, a.ID); !errors.Is(err, ErrNoProfiles) {
		t.Fatalf("pending delivery pair reappeared: %v", err)
	}
	fail = false
	if err := DeliverPendingMatch(db, b.ID, a.ID, send); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&models.Like{}).Count(&count)
	if count != 0 || sent[a.ID] != 1 || sent[b.ID] != 1 {
		t.Fatalf("persisted history or duplicate delivery: count=%d sent=%v", count, sent)
	}
	if err := DeliverPendingMatch(db, a.ID, b.ID, send); err != nil {
		t.Fatal(err)
	}
	if sent[a.ID] != 1 || sent[b.ID] != 1 {
		t.Fatal("redelivered deleted match")
	}
	if next, err := GetNextProfile(db, a.ID); err != nil || next.ID != b.ID {
		t.Fatalf("pair excluded after forgetting match: %v", err)
	}
}
