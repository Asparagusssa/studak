package crud

import (
	"strings"

	"gorm.io/gorm"

	"studak/internal/models"
)

// GetNextProfile returns a random candidate for the user's feed, honoring their
// SearchMode and skipping themselves and anyone they already liked/disliked.
func GetNextProfile(db *gorm.DB, userID uint) (*models.User, error) {
	var me models.User
	if err := db.First(&me, userID).Error; err != nil {
		return nil, wrapNotFound(err)
	}

	reacted := db.Model(&models.Like{}).Select("to_user_id").Where("from_user_id = ?", me.ID)

	q := db.Model(&models.User{}).
		Where("users.id <> ?", me.ID).
		Where("users.id NOT IN (?)", reacted)

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

	var candidate models.User
	err := q.Preload("MyUniversity").Order("RANDOM()").Limit(1).Take(&candidate).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNoProfiles
		}
		return nil, err
	}
	return &candidate, nil
}

const universitySearchLimit = 10

// SearchUniversities does a case-insensitive substring match on full and short
// names (Cyrillic included). Prefix matches are ranked first.
func SearchUniversities(db *gorm.DB, query string) ([]models.University, error) {
	query = strings.ToLower(strings.TrimSpace(query))
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
