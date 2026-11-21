package crud

import (
	"crypto/rand"
	"math/big"

	"gorm.io/gorm"

	"studak/internal/models"
)

// GetNextProfile mixes age-priority candidates with peers within two years.
// Each draw prefers the priority group with probability 2/3; an empty group
// falls back to the other. A random ID pivot avoids ORDER BY RANDOM().
func GetNextProfile(db *gorm.DB, userID uint) (*models.User, error) {
	roll, err := randomPivot(3)
	if err != nil {
		return nil, err
	}
	return getNextProfile(db, userID, roll <= 2)
}

func getNextProfile(db *gorm.DB, userID uint, priorityFirst bool) (*models.User, error) {
	if err := requireDatingStarted(db); err != nil {
		return nil, err
	}
	me, err := profileViewer(db, userID)
	if err != nil {
		return nil, err
	}

	if me.Paused {
		return nil, ErrProfilePaused
	}

	reacted := excludedReactions(db, me.ID)

	q := eligibleProfiles(excludeBlocked(db.Model(&models.User{}), me.ID), me).
		Where("users.id <> ? AND users.paused = ?", me.ID, false).
		Where("users.id NOT IN (?)", reacted).
		Where(`NOT EXISTS (SELECT 1 FROM likes AS pending WHERE pending.from_user_id = users.id AND pending.to_user_id = ? AND pending.is_mutual = ?)`, me.ID, true).
		Where("users.age BETWEEN ? AND ?", max(MinAge, me.Age-2), min(MaxAge, me.Age+2))

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
	// Keep the age buckets and pivot queries independent.
	q = q.Session(&gorm.Session{})
	queries := []*gorm.DB{q}
	var gender string
	var youngest, oldest int
	switch me.Gender {
	case "male":
		gender, youngest, oldest = "female", me.Age-2, me.Age-1
	case "female":
		gender, youngest, oldest = "male", me.Age+1, me.Age+2
	}
	if gender != "" {
		priority := q.Where("users.gender = ? AND users.age BETWEEN ? AND ?", gender, youngest, oldest)
		mixed := q.Where("COALESCE(users.gender, '') <> ? OR users.age < ? OR users.age > ?", gender, youngest, oldest)
		queries = []*gorm.DB{priority, mixed}
		if !priorityFirst {
			queries[0], queries[1] = queries[1], queries[0]
		}
	}
	for _, bucket := range queries {
		bucket = bucket.Session(&gorm.Session{})
		for _, query := range []*gorm.DB{bucket.Where("users.id >= ?", pivot), bucket.Where("users.id < ?", pivot)} {
			if candidate, err := find(query); err == nil {
				return candidate, nil
			} else if err != gorm.ErrRecordNotFound {
				return nil, err
			}
		}
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
