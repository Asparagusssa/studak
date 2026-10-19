package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"studak/internal/bots/maxbot"
	"studak/internal/crud"
)

type Handler struct {
	DB  *gorm.DB
	Max *maxbot.Bot
}

type Config struct {
	AllowedOrigins []string
	APIToken       string
	TelegramSecret string
	Max            *maxbot.Bot
}

func NewRouter(db *gorm.DB, cfg Config) *gin.Engine {
	h := &Handler{DB: db, Max: cfg.Max}

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), cors(cfg.AllowedOrigins))
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

	if cfg.TelegramSecret != "" {
		r.POST("/webhook/tg", webhookSecret("X-Telegram-Bot-Api-Secret-Token", cfg.TelegramSecret), limitBody(1<<20), h.webhookStub("telegram"))
	}
	if h.Max != nil {
		r.POST("/webhook/max", h.maxWebhook)
	}

	return r
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
	if err := c.ShouldBindJSON(&in); err != nil {
		respondError(c, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	user, err := crud.CreateUser(h.DB, in)
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
	user, err := crud.GetUser(h.DB, id)
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
	if err := c.ShouldBindJSON(&in); err != nil {
		respondError(c, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	user, err := crud.UpdateUser(h.DB, id, in)
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
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "from_user_id and to_user_id are required")
		return
	}
	isLike := req.IsLike == nil || *req.IsLike
	res, err := crud.SetLike(h.DB, req.FromUserID, req.ToUserID, isLike)
	if err != nil {
		handleErr(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *Handler) nextProfile(c *gin.Context) {
	id, ok := parseID(c, "userId")
	if !ok {
		return
	}
	profile, err := crud.GetNextProfile(h.DB, id)
	if err != nil {
		handleErr(c, err)
		return
	}
	c.JSON(http.StatusOK, profile)
}

func (h *Handler) searchUniversities(c *gin.Context) {
	unis, err := crud.SearchUniversities(h.DB, c.Query("q"))
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

// Telegram update handling is not implemented yet; acknowledge so Telegram
// does not keep retrying deliveries.
func (h *Handler) webhookStub(source string) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, _ := c.GetRawData()
		log.Printf("webhook %s: received %d bytes", source, len(body))
		c.Status(http.StatusOK)
	}
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
	case errors.Is(err, crud.ErrNoProfiles):
		respondError(c, http.StatusNotFound, "no more profiles")
	case errors.Is(err, crud.ErrAlreadyExists):
		respondError(c, http.StatusConflict, "user with this telegram_id, max_id or web_uuid already exists")
	default:
		log.Printf("internal error: %v", err)
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
