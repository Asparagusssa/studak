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
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gorm.io/gorm/logger"

	"studak/internal/api"
	"studak/internal/bots"
	"studak/internal/bots/maxbot"
	"studak/internal/bots/telegrambot"
	"studak/internal/database"
)

type config struct {
	Port           string
	MetricsHost    string
	MetricsPort    string
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
	var telegramBot *telegrambot.Bot
	if cfg.TelegramToken != "" {
		telegramBot = telegrambot.New(db, cfg.TelegramToken, cfg.TelegramSecret)
	}
	transports := map[string]maxbot.Messenger{}
	if maxBot != nil {
		transports["max"] = maxBot.Client
	}
	if telegramBot != nil {
		transports["telegram"] = telegramBot.Client
		telegramBot.Engine.Peers = transports
	}
	if maxBot != nil {
		maxBot.Peers = transports
	}
	router := api.NewRouter(db, api.Config{
		AllowedOrigins: cfg.AllowedOrigins,
		APIToken:       cfg.APIToken,
		TelegramSecret: cfg.TelegramSecret,
		Max:            maxBot,
		Telegram:       telegramBot,
	})

	addr := net.JoinHostPort("", cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	metricsAddr := net.JoinHostPort(cfg.MetricsHost, cfg.MetricsPort)
	metricsLn, err := net.Listen("tcp", metricsAddr)
	if err != nil {
		_ = ln.Close()
		return fmt.Errorf("listen metrics %s: %w", metricsAddr, err)
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
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.Handler())
	metricsSrv := &http.Server{
		Addr:              metricsAddr,
		Handler:           metricsMux,
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    1 << 18,
	}

	type serverError struct {
		name string
		err  error
	}
	serveErr := make(chan serverError, 2)
	go func() {
		err := srv.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- serverError{name: "api", err: err}
		}
	}()
	go func() {
		err := metricsSrv.Serve(metricsLn)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- serverError{name: "metrics", err: err}
		}
	}()
	log.Printf("listening on %s", addr)
	log.Printf("metrics listening on %s", metricsAddr)
	bots.RegisterWebhooks(cfg.WebhookBaseURL)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(stop)

	select {
	case sig := <-stop:
		log.Printf("received %s, shutting down", sig)
	case serverErr := <-serveErr:
		err = fmt.Errorf("%s server: %w", serverErr.name, serverErr.err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if shutdownErr := metricsSrv.Shutdown(ctx); shutdownErr != nil {
		err = errors.Join(err, fmt.Errorf("metrics shutdown: %w", shutdownErr))
	}
	if shutdownErr := srv.Shutdown(ctx); shutdownErr != nil {
		err = errors.Join(err, fmt.Errorf("api shutdown: %w", shutdownErr))
	}
	return err
}

func loadConfig() (config, error) {
	cfg := config{
		Port:           getenv("PORT", "8080"),
		MetricsHost:    getenv("METRICS_HOST", "127.0.0.1"),
		MetricsPort:    getenv("METRICS_PORT", "9091"),
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
	metricsPort, err := strconv.Atoi(cfg.MetricsPort)
	if err != nil || metricsPort < 1 || metricsPort > 65535 {
		return config{}, fmt.Errorf("METRICS_PORT must be between 1 and 65535")
	}
	if cfg.Port == cfg.MetricsPort {
		return config{}, fmt.Errorf("METRICS_PORT must differ from PORT")
	}
	if cfg.MetricsHost != "localhost" && net.ParseIP(cfg.MetricsHost) == nil {
		return config{}, fmt.Errorf("METRICS_HOST must be localhost or an IP address")
	}
	if len(cfg.APIToken) < 32 {
		return config{}, fmt.Errorf("API_TOKEN must contain at least 32 characters")
	}
	if cfg.TelegramToken != "" && !bots.ValidTelegramToken(cfg.TelegramToken) {
		return config{}, fmt.Errorf("TG_BOT_TOKEN has invalid format")
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
	seen := make(map[string]struct{})
	for _, item := range strings.Split(raw, ",") {
		origin := strings.TrimRight(strings.TrimSpace(item), "/")
		if origin == "" {
			continue
		}
		if strings.EqualFold(environment, "production") && origin == "*" {
			return nil, fmt.Errorf("CORS_ORIGINS cannot contain * in production")
		}
		if origin != "*" {
			u, err := url.Parse(origin)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
				return nil, fmt.Errorf("CORS_ORIGINS contains invalid origin %q", origin)
			}
			if strings.EqualFold(environment, "production") && u.Scheme != "https" {
				host := u.Hostname()
				ip := net.ParseIP(host)
				if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
					return nil, fmt.Errorf("CORS_ORIGINS must use HTTPS in production: %q", origin)
				}
			}
		}
		if _, exists := seen[origin]; exists {
			continue
		}
		seen[origin] = struct{}{}
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
