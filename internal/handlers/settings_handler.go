package handlers

import (
	"net/http"

	"go-barcode-webapp/internal/services"

	"github.com/gin-gonic/gin"
)

// SettingsHandler provides API endpoints for application-wide settings.
type SettingsHandler struct {
	settingsService *services.SettingsService
}

// NewSettingsHandler creates a new SettingsHandler.
func NewSettingsHandler(settingsService *services.SettingsService) *SettingsHandler {
	return &SettingsHandler{settingsService: settingsService}
}

// GetCurrencySettings returns the current currency symbol.
//
// @Summary     Get currency symbol
// @Description Returns the application currency symbol stored in app_settings.
// @Tags        admin
// @Produce     json
// @Success     200 {object} map[string]string
// @Router      /admin/currency [get]
// @Security    SessionCookie
func (h *SettingsHandler) GetCurrencySettings(c *gin.Context) {
	symbol := h.settingsService.GetCurrencySymbol()
	c.JSON(http.StatusOK, gin.H{
		"success":        true,
		"currencySymbol": symbol,
	})
}

// UpdateCurrencySettings updates the currency symbol.
//
// @Summary     Update currency symbol
// @Description Updates the application currency symbol. Must be non-empty and at most 8 characters.
// @Tags        admin
// @Accept      json
// @Produce     json
// @Param       body body map[string]string true "Currency payload"
// @Success     200 {object} map[string]string
// @Failure     400 {object} map[string]string
// @Failure     500 {object} map[string]string
// @Router      /admin/currency [put]
// @Security    SessionCookie
func (h *SettingsHandler) UpdateCurrencySettings(c *gin.Context) {
	var req struct {
		CurrencySymbol string `json:"currencySymbol" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "currencySymbol is required"})
		return
	}
	if len([]rune(req.CurrencySymbol)) > 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "currency symbol must be at most 8 characters"})
		return
	}
	if err := h.settingsService.UpdateCurrencySymbol(req.CurrencySymbol); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save currency symbol"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success":        true,
		"currencySymbol": req.CurrencySymbol,
	})
}
