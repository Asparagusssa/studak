package database

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"studak/internal/crud"
	"studak/internal/models"
)

func TestUdmurtiaCatalogSeedingAndSearch(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "catalog.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := db.AutoMigrate(&models.University{}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := SeedUniversities(db); err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	if err := db.Model(&models.University{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != int64(len(defaultUniversities)+66) {
		t.Fatalf("seed lost institutions or created duplicates: %d", count)
	}
	for query, name := range map[string]string{
		"УдГУ":        "Удмуртский государственный университет",
		"ИПЭК":        "Ижевский промышленно-экономический колледж",
		"ИГМА":        "Ижевский государственный медицинский университет",
		"Дебес":       "Дебёсский политехникум",
		"Асановский":  "Асановский аграрно-технический техникум",
		"музыкальный": "Республиканский музыкальный колледж (Ижевск)",
		"Кез":         "Дебёсский политехникум — филиал в Кезе",
	} {
		rows, err := crud.SearchUniversities(db, query)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, row := range rows {
			found = found || row.Name == name
		}
		if !found {
			t.Errorf("query %q did not find %q: %+v", query, name, rows)
		}
	}
}

func TestInstitutionSearchHandlesTyposAndRanksExactMatchesFirst(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "search.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := db.AutoMigrate(&models.University{}); err != nil {
		t.Fatal(err)
	}
	if err := SeedUniversities(db); err != nil {
		t.Fatal(err)
	}
	for query, want := range map[string]string{
		"удгц":     "Удмуртский государственный университет",
		"ипек":     "Ижевский промышленно-экономический колледж",
		"иж гту":   "Ижевский государственный технический университет имени М.Т. Калашникова",
		"  УдГУ  ": "Удмуртский государственный университет",
		"глазовский политхнический": "Глазовский политехнический колледж",
		"воткинский промышлненый":   "Воткинский промышленный техникум",
	} {
		got, err := crud.SearchUniversities(db, query)
		if err != nil || len(got) == 0 || got[0].Name != want {
			t.Errorf("query %q: got %+v, %v; want %q first", query, got, err, want)
		}
	}
	nearby, err := crud.SearchUniversities(db, "ипек")
	if err != nil || len(nearby) < 2 {
		t.Fatalf("nearby suggestions missing: %+v, %v", nearby, err)
	}
	noise, err := crud.SearchUniversities(db, "%_'")
	if err != nil || len(noise) != 0 {
		t.Fatalf("punctuation matched arbitrary institutions: %+v, %v", noise, err)
	}
}
