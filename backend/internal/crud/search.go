package crud

import (
	"crypto/rand"
	"math/big"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"studak/internal/models"
)

// GetNextProfile returns a candidate starting from a random ID pivot. Unlike
// ORDER BY RANDOM(), this lets PostgreSQL use the primary-key index as the user
// table grows.
func GetNextProfile(db *gorm.DB, userID uint) (*models.User, error) {
	var me models.User
	if err := db.First(&me, userID).Error; err != nil {
		return nil, wrapNotFound(err)
	}

	if me.Paused {
		return nil, ErrProfilePaused
	}

	reacted := db.Model(&models.Like{}).Select("to_user_id").Where("from_user_id = ?", me.ID)

	q := excludeBlocked(db.Model(&models.User{}), me.ID).
		Where("users.id <> ? AND users.paused = ?", me.ID, false).
		Where("users.id NOT IN (?)", reacted)

	if me.SearchGender == models.SearchGenderFemale || me.SearchGender == models.SearchGenderMale {
		q = q.Where("users.gender = ?", me.SearchGender)
	}

	switch me.SearchMode {
	case models.SearchModeMyOnly:
		if me.MyUniversityID == nil {
			return nil, ErrNoProfiles
		}
		q = q.Where("users.my_university_id = ?", *me.MyUniversityID)
	case models.SearchModeSelected:
		preferred := db.Table("user_preferred_universities").
			Select("university_id").
			Where("user_id = ?", me.ID)
		q = q.Where("users.my_university_id IN (?)", preferred)
	case models.SearchModeAll:
	default:
		return nil, invalid("unknown search_mode %q", me.SearchMode)
	}

	var maxID uint
	if err := db.Model(&models.User{}).Select("COALESCE(MAX(id), 0)").Scan(&maxID).Error; err != nil {
		return nil, err
	}
	if maxID == 0 {
		return nil, ErrNoProfiles
	}
	pivot, err := randomPivot(maxID)
	if err != nil {
		return nil, err
	}

	find := func(query *gorm.DB) (*models.User, error) {
		var candidate models.User
		err := query.Preload("MyUniversity").Order("users.id").Take(&candidate).Error
		if err != nil {
			return nil, err
		}
		return &candidate, nil
	}
	// Keep the pivot queries independent so the wraparound does not also
	// inherit the first query's lower bound.
	q = q.Session(&gorm.Session{})
	if candidate, err := find(q.Where("users.id >= ?", pivot)); err == nil {
		return candidate, nil
	} else if err != gorm.ErrRecordNotFound {
		return nil, err
	}
	if candidate, err := find(q.Where("users.id < ?", pivot)); err == nil {
		return candidate, nil
	} else if err != gorm.ErrRecordNotFound {
		return nil, err
	}
	return nil, ErrNoProfiles
}

func randomPivot(maxID uint) (uint, error) {
	n, err := rand.Int(rand.Reader, new(big.Int).SetUint64(uint64(maxID)))
	if err != nil {
		return 0, err
	}
	return uint(n.Uint64()) + 1, nil
}

const (
	universitySearchLimit    = 10
	MaxUniversitySearchRunes = 100
)

// SearchUniversities does a case-insensitive substring match on full and short
// names (Cyrillic included). Prefix matches are ranked first.
func SearchUniversities(db *gorm.DB, query string) ([]models.University, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	if utf8.RuneCountInString(query) > MaxUniversitySearchRunes {
		return nil, invalid("university search query is too long")
	}
	unis := []models.University{}

	q := db.Model(&models.University{})
	if query != "" {
		escaped := escapeLike(query)
		q = q.Where("search_key LIKE ? ESCAPE '\\'", "%"+escaped+"%").
			Order(gorm.Expr(`CASE
				WHEN search_key LIKE ? ESCAPE '\' THEN 0
				WHEN search_key LIKE ? ESCAPE '\' OR search_key LIKE ? ESCAPE '\' THEN 1
				ELSE 2 END`,
				escaped+"%", "% "+escaped+"%", "%«"+escaped+"%"))
	}
	err := q.Order("name").Limit(universitySearchLimit).Find(&unis).Error
	return unis, err
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
