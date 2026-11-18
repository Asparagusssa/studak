package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"gorm.io/gorm"

	"studak/internal/bots/maxbot"
	"studak/internal/bots/telegrambot"
	"studak/internal/crud"
	"studak/internal/logx"
)

type Handler struct {
	DB       *gorm.DB
	Max      *maxbot.Bot
	Telegram *telegrambot.Bot
}

type Config struct {
	AllowedOrigins []string
	APIToken       string
	TelegramSecret string
	Max            *maxbot.Bot
	Telegram       *telegrambot.Bot
}

var (
	httpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "studak",
		Subsystem: "http",
		Name:      "requests_total",
		Help:      "Total number of HTTP requests handled by the API.",
	}, []string{"method", "route", "status"})
	httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "studak",
		Subsystem: "http",
		Name:      "request_duration_seconds",
		Help:      "HTTP request duration in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "route"})
	httpInFlight = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "studak",
		Subsystem: "http",
		Name:      "in_flight_requests",
		Help:      "Current number of HTTP requests being handled.",
	})
)

func init() {
	prometheus.MustRegister(httpRequests, httpDuration, httpInFlight)
}

func NewRouter(db *gorm.DB, cfg Config) *gin.Engine {
	h := &Handler{DB: db, Max: cfg.Max, Telegram: cfg.Telegram}

	r := gin.New()
	r.Use(gin.Logger(), httpMetrics(), gin.Recovery(), requestTimeout(25*time.Second), securityHeaders(), cors(cfg.AllowedOrigins))
	_ = r.SetTrustedProxies(nil)

	r.GET("/health", h.health)

	protected := r.Group("/")
	protected.Use(bearerAuth(cfg.APIToken), limitBody(1<<20))
	protected.POST("/users", h.createUser)
	protected.GET("/users/next/:userId", h.nextProfile)
	protected.GET("/users/:id", h.getUser)
	protected.PATCH("/users/:id", h.updateUser)
	protected.POST("/like", h.setLike)
	protected.GET("/universities", h.searchUniversities)

	if h.Telegram != nil {
		r.POST("/webhook/tg", func(c *gin.Context) { h.Telegram.ServeHTTP(c.Writer, c.Request) })
	}
	if h.Max != nil {
		r.POST("/webhook/max", h.maxWebhook)
	}

	return r
}

func httpMetrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		httpInFlight.Inc()
		defer httpInFlight.Dec()

		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		httpRequests.WithLabelValues(c.Request.Method, route, strconv.Itoa(c.Writer.Status())).Inc()
		httpDuration.WithLabelValues(c.Request.Method, route).Observe(time.Since(started).Seconds())
	}
}

func (h *Handler) health(c *gin.Context) {
	sqlDB, err := h.DB.DB()
	if err != nil {
		respondError(c, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		respondError(c, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) createUser(c *gin.Context) {
	var in crud.CreateUserInput
	if err := decodeJSON(c, &in); err != nil {
		respondError(c, http.StatusBadRequest, "invalid JSON body")
		return
	}
	user, err := crud.CreateUser(h.DB.WithContext(c.Request.Context()), in)
	if err != nil {
		handleErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, user)
}

func (h *Handler) getUser(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	user, err := crud.GetUser(h.DB.WithContext(c.Request.Context()), id)
	if err != nil {
		handleErr(c, err)
		return
	}
	c.JSON(http.StatusOK, user)
}

func (h *Handler) updateUser(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var in crud.UpdateUserInput
	if err := decodeJSON(c, &in); err != nil {
		respondError(c, http.StatusBadRequest, "invalid JSON body")
		return
	}
	user, err := crud.UpdateUser(h.DB.WithContext(c.Request.Context()), id, in)
	if err != nil {
		handleErr(c, err)
		return
	}
	c.JSON(http.StatusOK, user)
}

type likeRequest struct {
	FromUserID uint  `json:"from_user_id" binding:"required"`
	ToUserID   uint  `json:"to_user_id" binding:"required"`
	IsLike     *bool `json:"is_like"`
}

func (h *Handler) setLike(c *gin.Context) {
	var req likeRequest
	if err := decodeJSON(c, &req); err != nil || req.FromUserID == 0 || req.ToUserID == 0 {
		respondError(c, http.StatusBadRequest, "from_user_id and to_user_id are required")
		return
	}
	isLike := req.IsLike == nil || *req.IsLike
	res, err := crud.SetLike(h.DB.WithContext(c.Request.Context()), req.FromUserID, req.ToUserID, isLike)
	if err != nil {
		handleErr(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func decodeJSON(c *gin.Context, dst any) error {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain a single JSON value")
	}
	return nil
}

func (h *Handler) nextProfile(c *gin.Context) {
	id, ok := parseID(c, "userId")
	if !ok {
		return
	}
	profile, err := crud.GetNextProfile(h.DB.WithContext(c.Request.Context()), id)
	if err != nil {
		handleErr(c, err)
		return
	}
	c.JSON(http.StatusOK, profile)
}

func (h *Handler) searchUniversities(c *gin.Context) {
	unis, err := crud.SearchUniversities(h.DB.WithContext(c.Request.Context()), c.Query("q"))
	if err != nil {
		handleErr(c, err)
		return
	}
	c.JSON(http.StatusOK, unis)
}

func (h *Handler) maxWebhook(c *gin.Context) {
	if h.Max == nil {
		c.Status(http.StatusOK)
		return
	}
	h.Max.ServeHTTP(c.Writer, c.Request)
}

func parseID(c *gin.Context, param string) (uint, bool) {
	id, err := strconv.ParseUint(c.Param(param), 10, 64)
	if err != nil || id == 0 {
		respondError(c, http.StatusBadRequest, param+" must be a positive integer")
		return 0, false
	}
	return uint(id), true
}

func handleErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, crud.ErrInvalidInput):
		respondError(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, crud.ErrNotFound):
		respondError(c, http.StatusNotFound, "not found")
	case errors.Is(err, crud.ErrProfilePaused):
		respondError(c, http.StatusConflict, "profile is paused")
	case errors.Is(err, crud.ErrNoProfiles):
		respondError(c, http.StatusNotFound, "no more profiles")
	case errors.Is(err, crud.ErrAlreadyExists):
		respondError(c, http.StatusConflict, "user with this telegram_id, max_id or web_uuid already exists")
	default:
		route := c.FullPath()
		if route == "" {
			route = c.Request.URL.Path
		}
		logx.Error(c.Request.Method+" "+route+" failed", err)
		respondError(c, http.StatusInternalServerError, "internal server error")
	}
}

func respondError(c *gin.Context, status int, msg string) {
	c.AbortWithStatusJSON(status, gin.H{"error": msg})
}

func cors(allowed []string) gin.HandlerFunc {
	allowAll := false
	set := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		if o == "*" {
			allowAll = true
		}
		set[strings.TrimRight(o, "/")] = struct{}{}
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			if _, ok := set[origin]; ok || allowAll {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Vary", "Origin")
				c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
				c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
			}
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Next()
	}
}

func requestTimeout(timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

func bearerAuth(token string) gin.HandlerFunc {
	expected := []byte("Bearer " + token)
	return func(c *gin.Context) {
		provided := []byte(c.GetHeader("Authorization"))
		if token == "" || len(provided) != len(expected) || subtle.ConstantTimeCompare(provided, expected) != 1 {
			c.Header("WWW-Authenticate", "Bearer")
			respondError(c, http.StatusUnauthorized, "unauthorized")
			return
		}
		c.Next()
	}
}

func limitBody(bytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, bytes)
		}
		c.Next()
	}
}

func webhookSecret(header, secret string) gin.HandlerFunc {
	expected := []byte(secret)
	return func(c *gin.Context) {
		provided := []byte(c.GetHeader(header))
		if secret == "" || len(provided) != len(expected) || subtle.ConstantTimeCompare(provided, expected) != 1 {
			respondError(c, http.StatusUnauthorized, "unauthorized")
			return
		}
		c.Next()
	}
}
