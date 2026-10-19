package database

import (
	"fmt"

	"gorm.io/gorm"

	"studak/internal/models"
)

var defaultUniversities = []models.University{
	{Name: "Московский государственный университет имени М.В. Ломоносова", ShortName: "МГУ"},
	{Name: "Санкт-Петербургский государственный университет", ShortName: "СПбГУ"},
	{Name: "Московский физико-технический институт", ShortName: "МФТИ"},
	{Name: "Национальный исследовательский университет «Высшая школа экономики»", ShortName: "НИУ ВШЭ"},
	{Name: "Московский государственный технический университет имени Н.Э. Баумана", ShortName: "МГТУ им. Баумана"},
	{Name: "Национальный исследовательский ядерный университет «МИФИ»", ShortName: "НИЯУ МИФИ"},
	{Name: "Университет ИТМО", ShortName: "ИТМО"},
	{Name: "Российский университет дружбы народов имени Патриса Лумумбы", ShortName: "РУДН"},
	{Name: "Московский государственный институт международных отношений", ShortName: "МГИМО"},
	{Name: "Российская академия народного хозяйства и государственной службы", ShortName: "РАНХиГС"},
	{Name: "Финансовый университет при Правительстве Российской Федерации", ShortName: "Финансовый университет"},
	{Name: "Новосибирский национальный исследовательский государственный университет", ShortName: "НГУ"},
	{Name: "Уральский федеральный университет имени Б.Н. Ельцина", ShortName: "УрФУ"},
	{Name: "Казанский (Приволжский) федеральный университет", ShortName: "КФУ"},
	{Name: "Национальный исследовательский Томский государственный университет", ShortName: "ТГУ"},
}

// SeedUniversities is idempotent: existing rows (matched by Name) are left as is.
func SeedUniversities(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for _, u := range defaultUniversities {
			uni := u
			if err := tx.Where(models.University{Name: uni.Name}).FirstOrCreate(&uni).Error; err != nil {
				return fmt.Errorf("seed %q: %w", uni.Name, err)
			}
		}
		return nil
	})
}
