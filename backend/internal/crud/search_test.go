package crud

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"studak/internal/models"
)

func feedDB(t *testing.T, now *time.Time) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "feed.db")+"?_pragma=foreign_keys(1)"), &gorm.Config{
		Logger:  logger.Default.LogMode(logger.Silent),
		NowFunc: func() time.Time { return *now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.ServiceState{}, &models.University{}, &models.User{}, &models.Like{}, &models.UserBlock{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.ServiceState{ID: 1, Started: true}).Error; err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	return db
}

func feedUser(t *testing.T, db *gorm.DB, name, gender, search string, age int) *models.User {
	t.Helper()
	user, err := CreateUser(db, CreateUserInput{Name: name, Age: age, Gender: gender, SearchGender: search})
	if err != nil {
		t.Fatal(err)
	}
	return user
}

func TestSkippedProfileReturnsAfterTwoHours(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	db := feedDB(t, &now)
	viewer := feedUser(t, db, "Борис", "male", "female", 21)
	peer := feedUser(t, db, "Анна", "female", "male", 20)
	if _, err := SetLike(db, peer.ID, viewer.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := ReactOnce(db, viewer.ID, peer.ID, false, false); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2*time.Hour - time.Second)
	if _, err := GetNextProfile(db, viewer.ID); !errors.Is(err, ErrNoProfiles) {
		t.Fatalf("skip returned before cooldown: %v", err)
	}
	if _, err := NextIncomingLike(db, viewer.ID); !errors.Is(err, ErrNoProfiles) {
		t.Fatalf("skipped incoming sender returned before cooldown: %v", err)
	}
	if _, err := ReactOnce(db, viewer.ID, peer.ID, true, false); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("stale reaction bypassed cooldown: %v", err)
	}
	now = now.Add(time.Second)
	if got, err := GetNextProfile(db, viewer.ID); err != nil || got.ID != peer.ID {
		t.Fatalf("skip not eligible at exactly two hours: %+v, %v", got, err)
	}
	if got, err := NextIncomingLike(db, viewer.ID); err != nil || got.ID != peer.ID {
		t.Fatalf("incoming sender not eligible after cooldown: %+v, %v", got, err)
	}
	if _, err := ReactOnce(db, viewer.ID, peer.ID, false, false); err != nil {
		t.Fatalf("cannot skip a returned profile: %v", err)
	}
	if _, err := GetNextProfile(db, viewer.ID); !errors.Is(err, ErrNoProfiles) {
		t.Fatalf("repeated skip did not restart cooldown: %v", err)
	}
	now = now.Add(2 * time.Hour)
	result, err := ReactOnce(db, viewer.ID, peer.ID, true, true)
	if err != nil || !result.NewLike || !result.NewMatch {
		t.Fatalf("cannot like returned profile: %+v, %v", result, err)
	}
	now = now.Add(365 * 24 * time.Hour)
	if _, err := GetNextProfile(db, viewer.ID); !errors.Is(err, ErrNoProfiles) {
		t.Fatalf("liked profile returned to feed: %v", err)
	}
}

func TestAgePriorityAndMixing(t *testing.T) {
	for _, test := range []struct {
		name, gender, peerGender string
		preferredAge, mixedAge   int
	}{
		{"men prefer younger women", "male", "female", 20, 22},
		{"women prefer older men", "female", "male", 24, 21},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now().UTC()
			db := feedDB(t, &now)
			viewer := feedUser(t, db, "Я", test.gender, test.peerGender, 22)
			preferred := feedUser(t, db, "Приоритет", test.peerGender, "all", test.preferredAge)
			mixed := feedUser(t, db, "Примесь", test.peerGender, "all", test.mixedAge)
			feedUser(t, db, "Слишком далеко", test.peerGender, "all", 25)
			for priority, want := range map[bool]uint{true: preferred.ID, false: mixed.ID} {
				got, err := getNextProfile(db, viewer.ID, priority)
				if err != nil || got.ID != want {
					t.Fatalf("priority=%v: got %+v, %v; want %d", priority, got, err, want)
				}
			}
			if _, err := SetLike(db, viewer.ID, preferred.ID, true); err != nil {
				t.Fatal(err)
			}
			if got, err := getNextProfile(db, viewer.ID, true); err != nil || got.ID != mixed.ID {
				t.Fatalf("empty priority group did not fall back: %+v, %v", got, err)
			}
			if _, err := SetLike(db, viewer.ID, mixed.ID, true); err != nil {
				t.Fatal(err)
			}
			if _, err := GetNextProfile(db, viewer.ID); !errors.Is(err, ErrNoProfiles) {
				t.Fatalf("candidate outside two-year range leaked: %v", err)
			}
		})
	}
}

func TestAgePriorityPreservesGenderAndUniversityFilters(t *testing.T) {
	now := time.Now().UTC()
	db := feedDB(t, &now)
	first := models.University{Name: "Первый колледж"}
	second := models.University{Name: "Второй колледж"}
	if err := db.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	viewer, err := CreateUser(db, CreateUserInput{Name: "Я", Age: 22, Gender: "male", SearchGender: "male", SearchMode: "my_only", MyUniversityID: &first.ID})
	if err != nil {
		t.Fatal(err)
	}
	matching, err := CreateUser(db, CreateUserInput{Name: "Подходит", Age: 22, Gender: "male", MyUniversityID: &first.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = CreateUser(db, CreateUserInput{Name: "Другой пол", Age: 21, Gender: "female", MyUniversityID: &first.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = CreateUser(db, CreateUserInput{Name: "Другой колледж", Age: 21, Gender: "male", MyUniversityID: &second.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := getNextProfile(db, viewer.ID, true); err != nil || got.ID != matching.ID {
		t.Fatalf("priority overrode explicit filters: %+v, %v", got, err)
	}
}

func TestOtherGenderRejected(t *testing.T) {
	now := time.Now().UTC()
	db := feedDB(t, &now)
	if _, err := CreateUser(db, CreateUserInput{Name: "Тест", Age: 21, Gender: "other"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("obsolete gender still accepted: %v", err)
	}
}
