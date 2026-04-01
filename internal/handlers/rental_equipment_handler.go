package handlers

import (
	"net/http"

	"go-barcode-webapp/internal/repository"

	"github.com/gin-gonic/gin"
)

type RentalEquipmentHandler struct {
	repo *repository.RentalEquipmentRepository
}

func NewRentalEquipmentHandler(repo *repository.RentalEquipmentRepository) *RentalEquipmentHandler {
	return &RentalEquipmentHandler{repo: repo}
}

// ShowRentalEquipmentList renders a deprecation notice for rental equipment management
func (h *RentalEquipmentHandler) ShowRentalEquipmentList(c *gin.Context) {
	user, _ := GetCurrentUser(c)

	c.HTML(http.StatusOK, "rental_equipment_standalone.html", gin.H{
		"title":       "Rental Equipment",
		"user":        user,
		"currentPage": "rental-equipment",
	})
}

// ShowRentalEquipmentForm renders a deprecation notice for the legacy form
func (h *RentalEquipmentHandler) ShowRentalEquipmentForm(c *gin.Context) {
	user, _ := GetCurrentUser(c)

	c.HTML(http.StatusOK, "rental_equipment_form_standalone.html", gin.H{
		"title":       "Rental Equipment",
		"user":        user,
		"currentPage": "rental-equipment",
	})
}

// ShowRentalAnalytics renders a deprecation notice for legacy analytics
func (h *RentalEquipmentHandler) ShowRentalAnalytics(c *gin.Context) {
	user, _ := GetCurrentUser(c)

	c.HTML(http.StatusOK, "rental_equipment_analytics_standalone.html", gin.H{
		"title":       "Rental Equipment Analytics",
		"user":        user,
		"currentPage": "rental-analytics",
	})
}

func (h *RentalEquipmentHandler) CreateRentalEquipment(c *gin.Context) {
	rentalEquipmentFeatureMovedJSON(c)
}

func (h *RentalEquipmentHandler) UpdateRentalEquipment(c *gin.Context) {
	rentalEquipmentFeatureMovedJSON(c)
}

func (h *RentalEquipmentHandler) DeleteRentalEquipment(c *gin.Context) {
	rentalEquipmentFeatureMovedJSON(c)
}

// GetRentalEquipmentAPI godoc
// @Summary      Rental equipment API deprecated
// @Description  This endpoint has moved to WarehouseCore and always returns HTTP 410 Gone.
// @Tags         rental-equipment
// @Produce      json
// @Failure      410  {object}  map[string]string  "Feature moved to WarehouseCore"
// @Security     SessionCookie
// @Router       /rental-equipment [get]
func (h *RentalEquipmentHandler) GetRentalEquipmentAPI(c *gin.Context) {
	rentalEquipmentFeatureMovedJSON(c)
}

func (h *RentalEquipmentHandler) AddRentalToJob(c *gin.Context) {
	rentalEquipmentFeatureMovedJSON(c)
}

func (h *RentalEquipmentHandler) CreateManualRentalEntry(c *gin.Context) {
	rentalEquipmentFeatureMovedJSON(c)
}

func (h *RentalEquipmentHandler) GetJobRentalEquipment(c *gin.Context) {
	rentalEquipmentFeatureMovedJSON(c)
}

func (h *RentalEquipmentHandler) RemoveRentalFromJob(c *gin.Context) {
	rentalEquipmentFeatureMovedJSON(c)
}

func (h *RentalEquipmentHandler) GetRentalAnalyticsAPI(c *gin.Context) {
	rentalEquipmentFeatureMovedJSON(c)
}

func rentalEquipmentFeatureMovedJSON(c *gin.Context) {
	c.JSON(http.StatusGone, gin.H{
		"error":   "Rental equipment functionality has moved to WarehouseCore",
		"message": "Use WarehouseCore to manage rental equipment and analytics.",
	})
}
