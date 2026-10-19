package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"gorm.io/gorm/logger"

	"studak/internal/api"
	"studak/internal/bots"
	"studak/internal/bots/maxbot"
	"studak/internal/database"
)

type config struct {
	Port           string
	Database       database.Config
	AllowedOrigins []string
	APIToken       string
	TelegramToken  string
	TelegramSecret string
	MaxToken       string
	MaxSecret      string
	WebhookBaseURL string
	DBLogLevel     logger.LogLevel
}

func Run() error {
	_ = godotenv.Load()
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	db, err := database.Open(cfg.Database, cfg.DBLogLevel)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("database pool: %w", err)
	}
	defer sqlDB.Close()

	if err := database.SeedUniversities(db); err != nil {
		return fmt.Errorf("seed: %w", err)
	}

	var maxBot *maxbot.Bot
	if cfg.MaxToken != "" {
		maxBot = maxbot.New(db, cfg.MaxToken, cfg.MaxSecret)
	}
	router := api.NewRouter(db, api.Config{
		AllowedOrigins: cfg.AllowedOrigins,
		APIToken:       cfg.APIToken,
		TelegramSecret: cfg.TelegramSecret,
		Max:            maxBot,
	})

	addr := net.JoinHostPort("", cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	serveErr := make(chan error, 1)
	go func() {
		err := srv.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()
	log.Printf("listening on %s", addr)
	bots.RegisterWebhooks(cfg.WebhookBaseURL)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(stop)

	select {
	case sig := <-stop:
		log.Printf("received %s, shutting down", sig)
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("server: %w", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

func loadConfig() (config, error) {
	cfg := config{
		Port:           getenv("PORT", "8080"),
		APIToken:       strings.TrimSpace(os.Getenv("API_TOKEN")),
		TelegramToken:  strings.TrimSpace(os.Getenv("TG_BOT_TOKEN")),
		TelegramSecret: strings.TrimSpace(os.Getenv("TG_WEBHOOK_SECRET")),
		MaxToken:       strings.TrimSpace(os.Getenv("MAX_BOT_TOKEN")),
		MaxSecret:      strings.TrimSpace(os.Getenv("MAX_WEBHOOK_SECRET")),
		WebhookBaseURL: strings.TrimSpace(os.Getenv("WEBHOOK_BASE_URL")),
		DBLogLevel:     logger.Warn,
	}

	port, err := strconv.Atoi(cfg.Port)
	if err != nil || port < 1 || port > 65535 {
		return config{}, fmt.Errorf("PORT must be between 1 and 65535")
	}
	if len(cfg.APIToken) < 32 {
		return config{}, fmt.Errorf("API_TOKEN must contain at least 32 characters")
	}
	if cfg.TelegramToken != "" && !validWebhookSecret(cfg.TelegramSecret, 16) {
		return config{}, fmt.Errorf("TG_WEBHOOK_SECRET must contain 16-256 characters A-Z, a-z, 0-9, _ or - when TG_BOT_TOKEN is set")
	}
	if cfg.MaxToken != "" && !validWebhookSecret(cfg.MaxSecret, 16) {
		return config{}, fmt.Errorf("MAX_WEBHOOK_SECRET must contain at least 16 characters when MAX_BOT_TOKEN is set")
	}

	origins, err := parseOrigins(os.Getenv("CORS_ORIGINS"), os.Getenv("APP_ENV"))
	if err != nil {
		return config{}, err
	}
	cfg.AllowedOrigins = origins

	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dsn == "" {
		dsn, err = databaseURLFromParts()
		if err != nil {
			return config{}, err
		}
	}
	cfg.Database = database.Config{
		DSN:             dsn,
		MaxOpenConns:    getenvInt("DB_MAX_OPEN_CONNS", 25),
		MaxIdleConns:    getenvInt("DB_MAX_IDLE_CONNS", 10),
		ConnMaxLifetime: getenvDuration("DB_CONN_MAX_LIFETIME", 30*time.Minute),
		ConnMaxIdleTime: getenvDuration("DB_CONN_MAX_IDLE_TIME", 5*time.Minute),
	}
	if os.Getenv("DB_DEBUG") == "1" {
		cfg.DBLogLevel = logger.Info
	}
	return cfg, nil
}

func validWebhookSecret(value string, min int) bool {
	if len(value) < min || len(value) > 256 {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func databaseURLFromParts() (string, error) {
	password := os.Getenv("DB_PASSWORD")
	if password == "" {
		return "", fmt.Errorf("DATABASE_URL or DB_PASSWORD is required")
	}
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(getenv("DB_USER", "dating"), password),
		Host:   net.JoinHostPort(getenv("DB_HOST", "127.0.0.1"), getenv("DB_PORT", "5432")),
		Path:   "/" + getenv("DB_NAME", "dating"),
	}
	q := u.Query()
	q.Set("sslmode", getenv("DB_SSLMODE", "require"))
	q.Set("TimeZone", "UTC")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func parseOrigins(raw, environment string) ([]string, error) {
	var origins []string
	for _, item := range strings.Split(raw, ",") {
		origin := strings.TrimRight(strings.TrimSpace(item), "/")
		if origin == "" {
			continue
		}
		if strings.EqualFold(environment, "production") && origin == "*" {
			return nil, fmt.Errorf("CORS_ORIGINS cannot contain * in production")
		}
		origins = append(origins, origin)
	}
	return origins, nil
}

func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
