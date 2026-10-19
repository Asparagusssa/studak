package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"studak/internal/database"
	"studak/internal/models"
)

func TestProtectedAPIAndPrivateFields(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "api.db")+"?_pragma=foreign_keys(1)"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.University{}, &models.User{}, &models.Like{}, &models.BotSession{}); err != nil {
		t.Fatal(err)
	}
	if err := database.SeedUniversities(db); err != nil {
		t.Fatal(err)
	}

	router := NewRouter(db, Config{APIToken: "test-api-token-with-at-least-32-characters"})

	req := httptest.NewRequest(http.MethodGet, "/universities", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("request without token: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	body := []byte(`{"telegram_id":123,"max_id":456,"name":"Анна","age":20,"search_mode":"all"}`)
	req = httptest.NewRequest(http.MethodPost, "/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-api-token-with-at-least-32-characters")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user: got %d body=%s", rec.Code, rec.Body.String())
	}
	response := rec.Body.String()
	for _, private := range []string{"telegram_id", "max_id", "web_uuid"} {
		if strings.Contains(response, private) {
			t.Fatalf("response leaked %s: %s", private, response)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("health: got %d body=%s", rec.Code, rec.Body.String())
	}
}
