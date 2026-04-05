package handlers

import (
	"errors"
	"log"
	"net/http"
	"regexp"
	"strings"

	"go-barcode-webapp/internal/models"
	"go-barcode-webapp/internal/services"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// iso4217Re validates ISO 4217 currency codes: exactly 3 uppercase ASCII letters.
var iso4217Re = regexp.MustCompile(`^[A-Z]{3}$`)

// maxWebhookBodyBytes is the maximum size (1 MiB) accepted from the public
// Twenty webhook endpoint. Requests that exceed this limit are rejected with 413.
const maxWebhookBodyBytes int64 = 1 << 20 // 1 MiB

// TwentyHandler handles the Twenty CRM integration settings pages and API.
type TwentyHandler struct {
	twentyService *services.TwentyService
	db            *gorm.DB
}

// NewTwentyHandler creates a new TwentyHandler.
func NewTwentyHandler(twentyService *services.TwentyService, db *gorm.DB) *TwentyHandler {
	return &TwentyHandler{twentyService: twentyService, db: db}
}

// isAdmin checks whether the current user has the admin role.
// System admin (username "admin") always returns true.
// For other users, the user's active roles are checked against the DB,
// matching the same logic used by the RequireAdmin RBAC middleware.
func (h *TwentyHandler) isAdmin(c *gin.Context) bool {
	user, exists := GetCurrentUser(c)
	if !exists || user == nil {
		return false
	}
	if user.Username == "admin" {
		return true
	}
	var userRoles []models.UserRole
	if err := h.db.Preload("Role").Where(
		"userID = ? AND is_active = ?",
		user.UserID, true,
	).Find(&userRoles).Error; err != nil {
		return false
	}
	for _, ur := range userRoles {
		if ur.Role != nil && ur.Role.IsActive && ur.Role.Name == "admin" {
			return true
		}
	}
	return false
}

// TwentySettingsForm renders the Twenty CRM integration settings page.
// Defense-in-depth: admin role is required (the route is also protected by middleware).
func (h *TwentyHandler) TwentySettingsForm(c *gin.Context) {
	user, exists := GetCurrentUser(c)
	if !exists {
		c.Redirect(http.StatusSeeOther, "/login")
		return
	}

	if !h.isAdmin(c) {
		c.HTML(http.StatusForbidden, "error.html", gin.H{
			"error": "You do not have permission to access this page.",
			"user":  user,
		})
		return
	}

	cfg := h.twentyService.GetConfig()

	var successMsg string
	if c.Query("success") == "1" {
		successMsg = "Twenty integration settings saved successfully!"
	}

	c.HTML(http.StatusOK, "twenty_settings.html", gin.H{
		"title":       "Twenty CRM Integration",
		"user":        user,
		"config":      cfg,
		"success":     successMsg,
		"currentPage": "integrations",
	})
}

// twentySettingsRequest is the request body for updating Twenty integration settings.
type twentySettingsRequest struct {
	Enabled       bool   `json:"enabled"`
	APIURL        string `json:"apiUrl"`
	APIKey        string `json:"apiKey"`        // empty = keep existing stored key
	WebhookSecret string `json:"webhookSecret"` // empty = keep existing stored secret
	CurrencyCode  string `json:"currencyCode"`
}

// twentySettingsResponse is the response body for Twenty integration settings.
type twentySettingsResponse struct {
	Success      bool   `json:"success"`
	Enabled      bool   `json:"enabled"`
	APIURL       string `json:"apiUrl"`
	CurrencyCode string `json:"currencyCode"`
	// APIKey and WebhookSecret are intentionally omitted for security.
}

// GetTwentySettings returns the current Twenty integration configuration.
//
// @Summary     Get Twenty CRM integration settings
// @Description Returns the current configuration for the Twenty CRM integration.
// @Tags        admin
// @Produce     json
// @Success     200 {object} twentySettingsResponse
// @Router      /api/v1/admin/integrations/twenty [get]
// @Security    SessionCookie
func (h *TwentyHandler) GetTwentySettings(c *gin.Context) {
	cfg := h.twentyService.GetConfig()
	c.JSON(http.StatusOK, twentySettingsResponse{
		Success:      true,
		Enabled:      cfg.Enabled,
		APIURL:       cfg.APIURL,
		CurrencyCode: cfg.CurrencyCode,
	})
}

// UpdateTwentySettings saves updated Twenty integration settings.
// An empty apiKey or webhookSecret keeps the previously stored value.
//
// @Summary     Update Twenty CRM integration settings
// @Description Saves the Twenty CRM integration configuration.
// @Tags        admin
// @Accept      json
// @Produce     json
// @Param       body body twentySettingsRequest true "Twenty settings"
// @Success     200 {object} twentySettingsResponse
// @Failure     400 {object} errorResponse
// @Failure     500 {object} errorResponse
// @Router      /api/v1/admin/integrations/twenty [put]
// @Security    SessionCookie
func (h *TwentyHandler) UpdateTwentySettings(c *gin.Context) {
	var req twentySettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid request body: " + err.Error()})
		return
	}

	req.APIURL = strings.TrimSpace(req.APIURL)
	req.APIKey = strings.TrimSpace(req.APIKey)
	req.WebhookSecret = strings.TrimSpace(req.WebhookSecret)
	// Normalise currency code: strip whitespace and uppercase.
	req.CurrencyCode = strings.ToUpper(strings.TrimSpace(req.CurrencyCode))

	// Load existing config so we can preserve secrets that were not supplied.
	existing := h.twentyService.GetConfig()

	if req.APIKey == "" {
		req.APIKey = existing.APIKey
	}
	if req.WebhookSecret == "" {
		req.WebhookSecret = existing.WebhookSecret
	}
	if req.CurrencyCode == "" {
		req.CurrencyCode = existing.CurrencyCode
	}

	// Validate ISO 4217 currency code (exactly 3 uppercase ASCII letters).
	if !iso4217Re.MatchString(req.CurrencyCode) {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "currency code must be a 3-letter ISO 4217 code (e.g. EUR, USD, GBP)"})
		return
	}

	if req.Enabled && req.APIURL == "" {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "API URL is required when enabling the Twenty integration"})
		return
	}
	if req.Enabled && req.APIKey == "" {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "API key is required when enabling the Twenty integration"})
		return
	}
	if req.Enabled && req.WebhookSecret == "" {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "webhook secret is required when enabling the Twenty integration"})
		return
	}

	cfg := services.TwentyConfig{
		Enabled:       req.Enabled,
		APIURL:        req.APIURL,
		APIKey:        req.APIKey,
		WebhookSecret: req.WebhookSecret,
		CurrencyCode:  req.CurrencyCode,
	}
	if err := h.twentyService.SaveConfig(cfg); err != nil {
		log.Printf("UpdateTwentySettings: failed to save config: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse{Error: "failed to save Twenty settings"})
		return
	}
	c.JSON(http.StatusOK, twentySettingsResponse{
		Success:      true,
		Enabled:      cfg.Enabled,
		APIURL:       cfg.APIURL,
		CurrencyCode: cfg.CurrencyCode,
	})
}

// TestTwentyConnection tests connectivity to the configured Twenty CRM instance.
//
// @Summary     Test Twenty CRM connection
// @Description Attempts to connect to the Twenty CRM API and returns the result.
// @Tags        admin
// @Produce     json
// @Success     200 {object} map[string]interface{}
// @Failure     400 {object} errorResponse
// @Router      /api/v1/admin/integrations/twenty/test [post]
// @Security    SessionCookie
func (h *TwentyHandler) TestTwentyConnection(c *gin.Context) {
	if err := h.twentyService.TestConnection(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Connection to Twenty CRM successful",
	})
}

// HandleTwentyWebhook processes an incoming webhook event from Twenty CRM and
// applies any customer updates back to RentalCore.
// This endpoint is public (not behind session auth) but protected by the webhook
// token configured in the integration settings.
func (h *TwentyHandler) HandleTwentyWebhook(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxWebhookBodyBytes)

	body, err := c.GetRawData()
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}

	webhookToken := c.GetHeader("X-Twenty-Webhook-Token")
	if err := h.twentyService.ApplyInboundWebhook(body, webhookToken); err != nil {
		if errors.Is(err, services.ErrInvalidWebhookToken) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid webhook token"})
			return
		}
		// Client-side errors (bad payload, missing fields, misconfiguration) → 400.
		// The detailed error is logged server-side; the generic message is returned
		// to avoid leaking internal configuration state to unauthenticated callers.
		if errors.Is(err, services.ErrWebhookBadRequest) {
			log.Printf("HandleTwentyWebhook: bad request: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "bad request"})
			return
		}
		log.Printf("HandleTwentyWebhook: error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "webhook processing failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}
