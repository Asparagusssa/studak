package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"gorm.io/gorm/logger"

	"github.com/student-dating/internal/api"
	"github.com/student-dating/internal/database"
)

func main() {
	port := getenv("PORT", "8484")
	dbPath := getenv("DB_PATH", "data/app.db")

	logLevel := logger.Warn
	if os.Getenv("DB_DEBUG") == "1" {
		logLevel = logger.Info
	}

	db, err := database.Open(dbPath, logLevel)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	if err := database.SeedUniversities(db); err != nil {
		log.Fatalf("seed: %v", err)
	}

	var origins []string
	if v := os.Getenv("CORS_ORIGINS"); v != "" {
		origins = strings.Split(v, ",")
	}
	router := api.NewRouter(db, api.Config{AllowedOrigins: origins})

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("listening on :%s (db: %s)", port, dbPath)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
