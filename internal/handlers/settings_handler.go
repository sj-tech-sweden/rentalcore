package handlers

import (
	"log"
	"net/http"
	"strings"

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

// currencyRequest is the request body for updating the currency symbol.
type currencyRequest struct {
	CurrencySymbol string `json:"currencySymbol" binding:"required"`
}

// currencyResponse is the response body for currency endpoints.
type currencyResponse struct {
	Success        bool   `json:"success"`
	CurrencySymbol string `json:"currencySymbol"`
}

// errorResponse is a generic error response body.
type errorResponse struct {
	Error string `json:"error"`
}

// GetCurrencySettings returns the current currency symbol.
//
// @Summary     Get currency symbol
// @Description Returns the application currency symbol stored in app_settings.
// @Tags        admin
// @Produce     json
// @Success     200 {object} currencyResponse
// @Router      /admin/currency [get]
// @Security    SessionCookie
func (h *SettingsHandler) GetCurrencySettings(c *gin.Context) {
	symbol := h.settingsService.GetCurrencySymbol()
	c.JSON(http.StatusOK, currencyResponse{
		Success:        true,
		CurrencySymbol: symbol,
	})
}

// UpdateCurrencySettings updates the currency symbol.
//
// @Summary     Update currency symbol
// @Description Updates the application currency symbol. Must be non-empty and at most 8 characters.
// @Tags        admin
// @Accept      json
// @Produce     json
// @Param       body body currencyRequest true "Currency payload"
// @Success     200 {object} currencyResponse
// @Failure     400 {object} errorResponse
// @Failure     500 {object} errorResponse
// @Router      /admin/currency [put]
// @Security    SessionCookie
func (h *SettingsHandler) UpdateCurrencySettings(c *gin.Context) {
	var req currencyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid request body: " + err.Error()})
		return
	}
	symbol := strings.TrimSpace(req.CurrencySymbol)
	if symbol == "" {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "currencySymbol must not be empty or whitespace"})
		return
	}
	if len([]rune(symbol)) > 8 {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "currency symbol must be at most 8 characters"})
		return
	}
	if err := h.settingsService.UpdateCurrencySymbol(symbol); err != nil {
		log.Printf("UpdateCurrencySettings: failed to save currency symbol: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse{Error: "failed to save currency symbol"})
		return
	}
	c.JSON(http.StatusOK, currencyResponse{
		Success:        true,
		CurrencySymbol: symbol,
	})
}
