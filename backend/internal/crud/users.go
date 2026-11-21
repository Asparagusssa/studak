package crud

import (
	"errors"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"studak/internal/models"
)

const (
	MinAge                   = 16
	MaxAge                   = 100
	MaxPreferredUniversities = 10
)

type CreateUserInput struct {
	TelegramID             *int64 `json:"telegram_id"`
	MaxID                  *int64 `json:"max_id"`
	WebUUID                string `json:"web_uuid"`
	Name                   string `json:"name"`
	Age                    int    `json:"age"`
	Gender                 string `json:"gender"`
	Bio                    string `json:"bio"`
	PhotoURL               string `json:"photo_url"`
	MaxPhotoToken          string `json:"-"`
	TelegramPhotoID        string `json:"-"`
	TelegramUsername       string `json:"-"`
	MaxProfileURL          string `json:"-"`
	MyUniversityID         *uint  `json:"my_university_id"`
	SearchMode             string `json:"search_mode"`
	SearchGender           string `json:"-"`
	PreferredUniversityIDs []uint `json:"preferred_university_ids"`
}

// UpdateUserInput uses pointers so that only provided fields are changed.
// PreferredUniversityIDs replaces the whole set when non-nil.
type UpdateUserInput struct {
	Name                   *string `json:"name"`
	Age                    *int    `json:"age"`
	Gender                 *string `json:"gender"`
	Bio                    *string `json:"bio"`
	PhotoURL               *string `json:"photo_url"`
	MaxPhotoToken          *string `json:"-"`
	TelegramPhotoID        *string `json:"-"`
	TelegramUsername       *string `json:"-"`
	MaxProfileURL          *string `json:"-"`
	MyUniversityID         *uint   `json:"my_university_id"`
	SearchMode             *string `json:"search_mode"`
	SearchGender           *string `json:"-"`
	PreferredUniversityIDs *[]uint `json:"preferred_university_ids"`
}

func CreateUser(db *gorm.DB, in CreateUserInput) (*models.User, error) {
	user := models.User{
		TelegramID:       in.TelegramID,
		MaxID:            in.MaxID,
		WebUUID:          strings.TrimSpace(in.WebUUID),
		Name:             strings.TrimSpace(in.Name),
		Age:              in.Age,
		Gender:           strings.TrimSpace(in.Gender),
		Bio:              strings.TrimSpace(in.Bio),
		PhotoURL:         strings.TrimSpace(in.PhotoURL),
		MaxPhotoToken:    strings.TrimSpace(in.MaxPhotoToken),
		TelegramPhotoID:  strings.TrimSpace(in.TelegramPhotoID),
		TelegramUsername: strings.TrimSpace(in.TelegramUsername),
		MaxProfileURL:    strings.TrimSpace(in.MaxProfileURL),
		MyUniversityID:   in.MyUniversityID,
		SearchMode:       strings.TrimSpace(in.SearchMode),
		SearchGender:     strings.TrimSpace(in.SearchGender),
	}
	if user.MaxPhotoToken != "" || user.TelegramPhotoID != "" {
		user.PhotoURL = ""
	}
	if user.WebUUID == "" {
		user.WebUUID = uuid.NewString()
	} else {
		parsed, err := uuid.Parse(user.WebUUID)
		if err != nil {
			return nil, invalid("web_uuid must be a valid UUID")
		}
		user.WebUUID = parsed.String()
	}
	if user.SearchMode == "" {
		user.SearchMode = models.SearchModeAll
	}
	if user.SearchGender == "" {
		user.SearchGender = models.SearchGenderAll
	}
	if err := validateUser(&user); err != nil {
		return nil, err
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := ensureUniversityExists(tx, user.MyUniversityID); err != nil {
			return err
		}
		preferred, err := loadUniversities(tx, in.PreferredUniversityIDs)
		if err != nil {
			return err
		}
		if user.SearchMode == models.SearchModeSelected && len(preferred) == 0 {
			return invalid("search_mode selected requires preferred_university_ids")
		}
		user.PreferredUniversities = preferred

		if err := tx.Omit("PreferredUniversities.*").Create(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) || isUniqueViolation(err) {
				return ErrAlreadyExists
			}
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return GetUser(db, user.ID)
}

func UpdateUser(db *gorm.DB, id uint, in UpdateUserInput) (*models.User, error) {
	err := db.Transaction(func(tx *gorm.DB) error {
		var user models.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, id).Error; err != nil {
			return wrapNotFound(err)
		}

		if in.Name != nil {
			user.Name = strings.TrimSpace(*in.Name)
		}
		if in.Age != nil {
			user.Age = *in.Age
		}
		if in.Gender != nil {
			user.Gender = strings.TrimSpace(*in.Gender)
		}
		if in.Bio != nil {
			user.Bio = strings.TrimSpace(*in.Bio)
		}
		if in.TelegramUsername != nil {
			user.TelegramUsername = strings.TrimSpace(*in.TelegramUsername)
		}
		if in.MaxProfileURL != nil {
			user.MaxProfileURL = strings.TrimSpace(*in.MaxProfileURL)
		}
		if in.PhotoURL != nil {
			user.PhotoURL = strings.TrimSpace(*in.PhotoURL)
			user.MaxPhotoToken = ""
			user.TelegramPhotoID = ""
		}
		if in.MaxPhotoToken != nil {
			user.MaxPhotoToken = strings.TrimSpace(*in.MaxPhotoToken)
			user.PhotoURL = ""
			user.TelegramPhotoID = ""
		}
		if in.TelegramPhotoID != nil {
			user.TelegramPhotoID = strings.TrimSpace(*in.TelegramPhotoID)
			user.PhotoURL = ""
			user.MaxPhotoToken = ""
		}
		if in.MyUniversityID != nil {
			if err := ensureUniversityExists(tx, in.MyUniversityID); err != nil {
				return err
			}
			user.MyUniversityID = in.MyUniversityID
		}
		if in.SearchMode != nil {
			user.SearchMode = strings.TrimSpace(*in.SearchMode)
		}
		if in.SearchGender != nil {
			user.SearchGender = strings.TrimSpace(*in.SearchGender)
		}
		if err := validateUser(&user); err != nil {
			return err
		}

		if err := tx.Omit("MyUniversity", "PreferredUniversities").Save(&user).Error; err != nil {
			return err
		}

		if in.PreferredUniversityIDs != nil {
			preferred, err := loadUniversities(tx, *in.PreferredUniversityIDs)
			if err != nil {
				return err
			}
			if err := tx.Model(&user).Association("PreferredUniversities").Replace(preferred); err != nil {
				return err
			}
		}
		if user.SearchMode == models.SearchModeSelected {
			var count int64
			if err := tx.Table("user_preferred_universities").Where("user_id = ?", user.ID).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				return invalid("search_mode selected requires preferred_university_ids")
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return GetUser(db, id)
}

func GetUser(db *gorm.DB, id uint) (*models.User, error) {
	var user models.User
	err := db.Preload("MyUniversity").Preload("PreferredUniversities").First(&user, id).Error
	if err != nil {
		return nil, wrapNotFound(err)
	}
	if user.PreferredUniversities == nil {
		user.PreferredUniversities = []models.University{}
	}
	return &user, nil
}

func GetUserByMaxID(db *gorm.DB, maxID int64) (*models.User, error) {
	var user models.User
	err := db.Preload("MyUniversity").Preload("PreferredUniversities").
		Where("max_id = ?", maxID).First(&user).Error
	if err != nil {
		return nil, wrapNotFound(err)
	}
	if user.PreferredUniversities == nil {
		user.PreferredUniversities = []models.University{}
	}
	return &user, nil
}

func validateUser(u *models.User) error {
	if !models.IsValidTelegramUsername(u.TelegramUsername) {
		return invalid("invalid Telegram username")
	}
	if !models.IsValidMaxProfileURL(u.MaxProfileURL) {
		return invalid("MAX profile link must be an HTTPS max.ru/u/ link")
	}
	if u.TelegramID != nil && *u.TelegramID <= 0 {
		return invalid("telegram_id must be a positive integer")
	}
	if u.MaxID != nil && *u.MaxID <= 0 {
		return invalid("max_id must be a positive integer")
	}
	if u.Name == "" {
		return invalid("name is required")
	}
	if len([]rune(u.Name)) > 100 {
		return invalid("name is too long")
	}
	if u.Age < MinAge || u.Age > MaxAge {
		return invalid("age must be between %d and %d", MinAge, MaxAge)
	}
	if len([]rune(u.Bio)) > 1000 {
		return invalid("bio is too long")
	}
	if len([]rune(u.Gender)) > 16 {
		return invalid("gender is too long")
	}
	if u.Gender != "" && u.Gender != "male" && u.Gender != "female" {
		return invalid("gender must be male or female")
	}
	if len([]rune(u.PhotoURL)) > 512 {
		return invalid("photo_url is too long")
	}
	if u.PhotoURL != "" {
		parsed, err := url.ParseRequestURI(u.PhotoURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return invalid("photo_url must be an absolute HTTP or HTTPS URL")
		}
	}
	if u.MyUniversityID != nil && *u.MyUniversityID == 0 {
		return invalid("my_university_id must be a positive integer")
	}
	if !models.IsValidSearchGender(u.SearchGender) {
		return invalid("search gender must be all, female or male")
	}
	if !models.IsValidSearchMode(u.SearchMode) {
		return invalid("search_mode must be one of: my_only, selected, all")
	}
	if u.SearchMode == models.SearchModeMyOnly && u.MyUniversityID == nil {
		return invalid("search_mode my_only requires my_university_id")
	}
	return nil
}

func ensureUniversityExists(tx *gorm.DB, id *uint) error {
	if id == nil {
		return nil
	}
	var count int64
	if err := tx.Model(&models.University{}).Where("id = ?", *id).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return invalid("university %d does not exist", *id)
	}
	return nil
}

func loadUniversities(tx *gorm.DB, ids []uint) ([]models.University, error) {
	unique := make([]uint, 0, len(ids))
	seen := make(map[uint]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			return nil, invalid("preferred_university_ids must contain positive integers")
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			unique = append(unique, id)
			if len(unique) > MaxPreferredUniversities {
				return nil, invalid("preferred_university_ids may contain at most %d universities", MaxPreferredUniversities)
			}
		}
	}
	if len(unique) == 0 {
		return []models.University{}, nil
	}

	var unis []models.University
	if err := tx.Where("id IN ?", unique).Find(&unis).Error; err != nil {
		return nil, err
	}
	if len(unis) != len(unique) {
		return nil, invalid("preferred_university_ids contains unknown university")
	}
	return unis, nil
}

func wrapNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}

func isUniqueViolation(err error) bool {
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func GetUserByTelegramID(db *gorm.DB, telegramID int64) (*models.User, error) {
	var user models.User
	if err := db.Preload("MyUniversity").Preload("PreferredUniversities").Where("telegram_id = ?", telegramID).First(&user).Error; err != nil {
		return nil, wrapNotFound(err)
	}
	if user.PreferredUniversities == nil {
		user.PreferredUniversities = []models.University{}
	}
	return &user, nil
}
