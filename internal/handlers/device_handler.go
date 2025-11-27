package handlers

import (
	"database/sql"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-barcode-webapp/internal/models"
	"go-barcode-webapp/internal/repository"
	"go-barcode-webapp/internal/services"

	"github.com/gin-gonic/gin"
)

// Simple cache for devices
type DeviceCache struct {
	data      []models.DeviceWithJobInfo
	timestamp time.Time
	mutex     sync.RWMutex
}

// Tree cache for optimized tree data
type TreeCache struct {
	data      []TreeCategory
	timestamp time.Time
	mutex     sync.RWMutex
}

var deviceCache = &DeviceCache{
	timestamp: time.Time{}, // Force cache miss initially - CLEARED FOR CATEGORY RELATIONSHIP FIX
}

type DeviceHandler struct {
	deviceRepo     *repository.DeviceRepository
	barcodeService *services.BarcodeService
	productRepo    *repository.ProductRepository
}

func NewDeviceHandler(deviceRepo *repository.DeviceRepository, barcodeService *services.BarcodeService, productRepo *repository.ProductRepository) *DeviceHandler {
	return &DeviceHandler{
		deviceRepo:     deviceRepo,
		barcodeService: barcodeService,
		productRepo:    productRepo,
	}
}

// Web interface handlers
func (h *DeviceHandler) ListDevices(c *gin.Context) {

	user, _ := GetCurrentUser(c)

	params := &models.FilterParams{}
	if err := c.ShouldBindQuery(params); err != nil {
		c.Redirect(http.StatusSeeOther, fmt.Sprintf("/error?code=400&message=Bad Request&details=%s", err.Error()))
		return
	}

	// FIX: Ensure search parameter is properly handled
	searchParam := c.Query("search")
	if searchParam != "" {
		params.SearchTerm = searchParam
	}

	// Handle pagination
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}

	limit := 20 // Devices per page
	params.Limit = limit
	params.Offset = (page - 1) * limit
	params.Page = page

	viewType := c.DefaultQuery("view", "list") // Default to list view

	// Use cache for basic list view without search (but not for tree or categorized views)
	var devices []models.DeviceWithJobInfo
	var err error

	if params.SearchTerm == "" && page == 1 && viewType == "list" {
		// Try to use cache for first page without search
		deviceCache.mutex.RLock()
		if time.Since(deviceCache.timestamp) < 30*time.Second && len(deviceCache.data) > 0 {
			// Use cached data
			devices = deviceCache.data
			if len(devices) > limit {
				devices = devices[:limit]
			}
			deviceCache.mutex.RUnlock()
		} else {
			deviceCache.mutex.RUnlock()

			// Fetch fresh data using ListWithCategories to ensure categories are loaded
			deviceList, err := h.deviceRepo.ListWithCategories(params)

			// Convert to DeviceWithJobInfo format with proper assignment checking
			devices = make([]models.DeviceWithJobInfo, len(deviceList))
			for i, device := range deviceList {
				// Check if device is currently assigned to an active job
				isAssigned, jobID, err := h.deviceRepo.IsDeviceCurrentlyAssigned(device.DeviceID)
				if err != nil {
					isAssigned = false
					jobID = nil
				}

				devices[i] = models.DeviceWithJobInfo{
					Device:     device,
					JobID:      jobID,
					IsAssigned: isAssigned,
				}
			}

			if err != nil {
				c.Redirect(http.StatusSeeOther, fmt.Sprintf("/error?code=500&message=Database Error&details=%s", err.Error()))
				return
			}

			// Cache the result
			deviceCache.mutex.Lock()
			deviceCache.data = devices
			deviceCache.timestamp = time.Now()
			deviceCache.mutex.Unlock()
		}
	} else {
		// For search or pagination, use ListWithCategories to ensure categories are loaded
		deviceList, err := h.deviceRepo.ListWithCategories(params)

		// Convert to DeviceWithJobInfo format with proper assignment checking
		devices = make([]models.DeviceWithJobInfo, len(deviceList))
		for i, device := range deviceList {
			// Check if device is currently assigned to an active job
			isAssigned, jobID, err := h.deviceRepo.IsDeviceCurrentlyAssigned(device.DeviceID)
			if err != nil {
				isAssigned = false
				jobID = nil
			}

			devices[i] = models.DeviceWithJobInfo{
				Device:     device,
				JobID:      jobID,
				IsAssigned: isAssigned,
			}
		}

		if err != nil {
			c.Redirect(http.StatusSeeOther, fmt.Sprintf("/error?code=500&message=Database Error&details=%s", err.Error()))
			return
		}
	}

	// Calculate pagination info for all list view requests (both cached and fresh)
	var totalDevices int
	var totalPages int
	if viewType == "list" {
		// Get total device count for pagination
		totalDevices, err = h.deviceRepo.GetTotalCount()
		if err != nil {
			totalDevices = 0
		}

		totalPages = (totalDevices + limit - 1) / limit // Ceiling division
		if totalPages == 0 {
			totalPages = 1
		}
	}
	if viewType == "tree" {
		// For tree view, load tree data and render in the main template
		treeData, err := h.buildTreeData()
		if err != nil {
			// Fall back to list view instead of error page
			SafeHTML(c, http.StatusOK, "devices_standalone.html", gin.H{
				"title":       "Devices (Tree Error - Showing List)",
				"devices":     devices,
				"params":      params,
				"user":        user,
				"viewType":    "list", // Force list view
				"currentPage": "devices",
				"treeError":   err.Error(),
			})
			return
		}

		if len(treeData) == 0 {
			SafeHTML(c, http.StatusOK, "devices_standalone.html", gin.H{
				"title":       "Devices (Empty Tree - Showing List)",
				"devices":     devices,
				"params":      params,
				"user":        user,
				"viewType":    "list", // Force list view
				"currentPage": "devices",
				"treeError":   "No categories found for tree view",
			})
			return
		}

		SafeHTML(c, http.StatusOK, "devices_standalone.html", gin.H{
			"title":       "Device Tree View",
			"params":      params,
			"user":        user,
			"viewType":    "tree",
			"currentPage": "devices",
			"treeData":    treeData,
		})
	} else {
		// Safe template rendering with error handling
		SafeHTML(c, http.StatusOK, "devices_standalone.html", gin.H{
			"title":        "Devices",
			"devices":      devices,
			"params":       params,
			"user":         user,
			"viewType":     "list",
			"categorized":  false,
			"currentPage":  "devices", // For navbar highlighting
			"pageNumber":   page,      // For pagination
			"hasNextPage":  page < totalPages,
			"totalPages":   totalPages,
			"totalDevices": totalDevices,
		})
	}
}

func (h *DeviceHandler) NewDeviceForm(c *gin.Context) {
	// Only allow fetch requests from modals, block direct browser access
	acceptHeader := c.GetHeader("Accept")
	xRequestedWith := c.GetHeader("X-Requested-With")

	// Block direct browser access - only allow modal/fetch requests
	if xRequestedWith != "XMLHttpRequest" && !strings.Contains(acceptHeader, "application/json") && !strings.Contains(acceptHeader, "text/html") {
		c.Redirect(http.StatusFound, "/devices")
		return
	}

	// If it's a direct browser request (Accept: text/html without XMLHttpRequest), redirect
	if strings.Contains(acceptHeader, "text/html") && xRequestedWith != "XMLHttpRequest" {
		c.Redirect(http.StatusFound, "/devices")
		return
	}

	user, _ := GetCurrentUser(c)

	products, err := h.productRepo.List(&models.FilterParams{})
	if err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"error": err.Error(), "user": user})
		return
	}

	c.HTML(http.StatusOK, "device_form.html", gin.H{
		"title":    "New Device",
		"device":   &models.Device{},
		"products": products,
		"user":     user,
	})
}

func (h *DeviceHandler) CreateDevice(c *gin.Context) {

	// Get form values
	serialNumber := c.PostForm("serialnumber")
	status := c.PostForm("status")
	notes := c.PostForm("notes")
	quantityStr := c.PostForm("quantity")

	if status == "" {
		status = "free"
	}

	// Parse quantity (default to 1 if not provided or invalid)
	quantity := 1
	if quantityStr != "" {
		if q, err := strconv.Atoi(quantityStr); err == nil && q > 0 && q <= 100 {
			quantity = q
		}
	}

	var productID *uint
	if productIDStr := c.PostForm("productID"); productIDStr != "" {
		if pid, err := strconv.ParseUint(productIDStr, 10, 32); err == nil {
			prodID := uint(pid)
			productID = &prodID
		}
	}

	if productID == nil {
		user, _ := GetCurrentUser(c)
		products, _ := h.productRepo.List(&models.FilterParams{})
		c.HTML(http.StatusBadRequest, "device_form.html", gin.H{
			"title":    "New Device",
			"device":   &models.Device{},
			"products": products,
			"error":    "Please select a product",
			"user":     user,
		})
		return
	}

	// Create multiple devices
	createdDevices := make([]models.Device, 0, quantity)
	var lastError error

	for i := 0; i < quantity; i++ {
		device := models.Device{
			DeviceID:  "", // Let database generate the ID automatically
			ProductID: productID,
			Status:    status,
		}

		// Handle optional string fields
		// For serial numbers, append index if creating multiple devices
		if serialNumber != "" {
			if quantity > 1 {
				indexedSerial := fmt.Sprintf("%s-%02d", serialNumber, i+1)
				device.SerialNumber = &indexedSerial
			} else {
				device.SerialNumber = &serialNumber
			}
		}

		if notes != "" {
			device.Notes = &notes
		}

		// Handle date fields
		if purchaseDateStr := c.PostForm("purchase_date"); purchaseDateStr != "" {
			if purchaseDate, err := time.Parse("2006-01-02", purchaseDateStr); err == nil {
				device.PurchaseDate = &purchaseDate
			}
		}
		if lastMaintenanceStr := c.PostForm("last_maintenance"); lastMaintenanceStr != "" {
			if lastMaintenance, err := time.Parse("2006-01-02", lastMaintenanceStr); err == nil {
				device.LastMaintenance = &lastMaintenance
			}
		}

		if err := h.deviceRepo.Create(&device); err != nil {
			lastError = err
			break
		}

		createdDevices = append(createdDevices, device)
	}

	// Handle errors
	if lastError != nil {
		user, _ := GetCurrentUser(c)
		products, _ := h.productRepo.List(&models.FilterParams{})
		errorMsg := fmt.Sprintf("Error creating device %d of %d: %v", len(createdDevices)+1, quantity, lastError)
		if len(createdDevices) > 0 {
			errorMsg += fmt.Sprintf(" (%d devices were created successfully before the error)", len(createdDevices))
		}
		c.HTML(http.StatusInternalServerError, "device_form.html", gin.H{
			"title":    "New Device",
			"device":   &models.Device{},
			"products": products,
			"error":    errorMsg,
			"user":     user,
		})
		return
	}

	c.Redirect(http.StatusFound, "/devices")
}

func (h *DeviceHandler) GetDevice(c *gin.Context) {
	// Only allow fetch requests from modals, block direct browser access
	acceptHeader := c.GetHeader("Accept")
	xRequestedWith := c.GetHeader("X-Requested-With")

	// Block direct browser access - only allow modal/fetch requests
	if xRequestedWith != "XMLHttpRequest" && !strings.Contains(acceptHeader, "application/json") && !strings.Contains(acceptHeader, "text/html") {
		c.Redirect(http.StatusFound, "/devices")
		return
	}

	// If it's a direct browser request (Accept: text/html without XMLHttpRequest), redirect
	if strings.Contains(acceptHeader, "text/html") && xRequestedWith != "XMLHttpRequest" {
		c.Redirect(http.StatusFound, "/devices")
		return
	}

	user, _ := GetCurrentUser(c)

	deviceID := c.Param("id")

	device, err := h.deviceRepo.GetByID(deviceID)
	if err != nil {
		c.HTML(http.StatusNotFound, "error.html", gin.H{"error": "Device not found", "user": user})
		return
	}

	c.HTML(http.StatusOK, "device_detail.html", gin.H{
		"device": device,
		"user":   user,
	})
}

func (h *DeviceHandler) EditDeviceForm(c *gin.Context) {
	// Only allow fetch requests from modals, block direct browser access
	acceptHeader := c.GetHeader("Accept")
	xRequestedWith := c.GetHeader("X-Requested-With")

	// Block direct browser access - only allow modal/fetch requests
	if xRequestedWith != "XMLHttpRequest" && !strings.Contains(acceptHeader, "application/json") && !strings.Contains(acceptHeader, "text/html") {
		c.Redirect(http.StatusFound, "/devices")
		return
	}

	// If it's a direct browser request (Accept: text/html without XMLHttpRequest), redirect
	if strings.Contains(acceptHeader, "text/html") && xRequestedWith != "XMLHttpRequest" {
		c.Redirect(http.StatusFound, "/devices")
		return
	}

	user, _ := GetCurrentUser(c)

	deviceID := c.Param("id")

	device, err := h.deviceRepo.GetByID(deviceID)
	if err != nil {
		c.HTML(http.StatusNotFound, "error.html", gin.H{"error": "Device not found", "user": user})
		return
	}

	products, err := h.productRepo.List(&models.FilterParams{})
	if err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"error": err.Error(), "user": user})
		return
	}

	c.HTML(http.StatusOK, "device_form.html", gin.H{
		"title":    "Edit Device",
		"device":   device,
		"products": products,
		"user":     user,
	})
}

func (h *DeviceHandler) UpdateDevice(c *gin.Context) {
	deviceID := c.Param("id")
	serialNumber := c.PostForm("serialnumber")
	status := c.PostForm("status")
	notes := c.PostForm("notes")

	var productID *uint
	if productIDStr := c.PostForm("productID"); productIDStr != "" {
		if pid, err := strconv.ParseUint(productIDStr, 10, 32); err == nil {
			prodID := uint(pid)
			productID = &prodID
		}
	}

	device := models.Device{
		DeviceID:  deviceID,
		ProductID: productID,
		Status:    status,
	}

	// Handle optional string fields
	if serialNumber != "" {
		device.SerialNumber = &serialNumber
	}
	if notes != "" {
		device.Notes = &notes
	}

	// Handle date fields
	if purchaseDateStr := c.PostForm("purchase_date"); purchaseDateStr != "" {
		if purchaseDate, err := time.Parse("2006-01-02", purchaseDateStr); err == nil {
			device.PurchaseDate = &purchaseDate
		}
	}
	if lastMaintenanceStr := c.PostForm("last_maintenance"); lastMaintenanceStr != "" {
		if lastMaintenance, err := time.Parse("2006-01-02", lastMaintenanceStr); err == nil {
			device.LastMaintenance = &lastMaintenance
		}
	}

	if err := h.deviceRepo.Update(&device); err != nil {
		user, _ := GetCurrentUser(c)
		products, _ := h.productRepo.List(&models.FilterParams{})
		c.HTML(http.StatusInternalServerError, "device_form.html", gin.H{
			"title":    "Edit Device",
			"device":   &device,
			"products": products,
			"error":    err.Error(),
			"user":     user,
		})
		return
	}

	c.Redirect(http.StatusFound, "/devices")
}

func (h *DeviceHandler) DeleteDevice(c *gin.Context) {
	deviceID := c.Param("id")

	if err := h.deviceRepo.Delete(deviceID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Device deleted successfully"})
}

func (h *DeviceHandler) GetDeviceQR(c *gin.Context) {
	user, _ := GetCurrentUser(c)

	deviceID := c.Param("id")

	device, err := h.deviceRepo.GetByID(deviceID)
	if err != nil {
		c.HTML(http.StatusNotFound, "error.html", gin.H{"error": "Device not found", "user": user})
		return
	}

	// Use serial number if available, otherwise use device ID
	identifier := deviceID
	if device.SerialNumber != nil && *device.SerialNumber != "" {
		identifier = *device.SerialNumber
	}

	qrCode, err := h.barcodeService.GenerateQRCode(identifier, 256)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"error": err.Error(), "user": user})
		return
	}

	c.Data(http.StatusOK, "image/png", qrCode)
}

func (h *DeviceHandler) GetDeviceBarcode(c *gin.Context) {
	user, _ := GetCurrentUser(c)

	deviceID := c.Param("id")

	device, err := h.deviceRepo.GetByID(deviceID)
	if err != nil {
		c.HTML(http.StatusNotFound, "error.html", gin.H{"error": "Device not found", "user": user})
		return
	}

	// Use serial number if available, otherwise use device ID
	identifier := deviceID
	if device.SerialNumber != nil && *device.SerialNumber != "" {
		identifier = *device.SerialNumber
	}

	barcode, err := h.barcodeService.GenerateBarcode(identifier)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"error": err.Error(), "user": user})
		return
	}

	c.Data(http.StatusOK, "image/png", barcode)
}

func (h *DeviceHandler) GetAvailableDevices(c *gin.Context) {
	devices, err := h.deviceRepo.GetAvailableDevices()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, devices)
}

// API handlers for tree view
func (h *DeviceHandler) GetDevicesByCategory(c *gin.Context) {
	categoryID := c.Param("id")

	categoryIDUint, err := strconv.ParseUint(categoryID, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid category ID"})
		return
	}

	devices, err := h.productRepo.GetDevicesByCategory(uint(categoryIDUint))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, devices)
}

func (h *DeviceHandler) GetDevicesBySubcategory(c *gin.Context) {
	subcategoryID := c.Param("id")

	devices, err := h.productRepo.GetDevicesBySubcategory(subcategoryID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, devices)
}

func (h *DeviceHandler) GetDevicesBySubbiercategory(c *gin.Context) {
	subbiercategoryID := c.Param("id")

	devices, err := h.productRepo.GetDevicesBySubbiercategory(subbiercategoryID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, devices)
}

// API handlers
func (h *DeviceHandler) ListDevicesAPI(c *gin.Context) {
	params := &models.FilterParams{}
	if err := c.ShouldBindQuery(params); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Use the new method with categories for case management
	devices, err := h.deviceRepo.ListWithCategories(params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, devices)
}

func (h *DeviceHandler) CreateDeviceAPI(c *gin.Context) {
	var device models.Device
	if err := c.ShouldBindJSON(&device); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.deviceRepo.Create(&device); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, device)
}

func (h *DeviceHandler) GetDeviceAPI(c *gin.Context) {
	deviceID := c.Param("id")
	device, err := h.deviceRepo.GetByID(deviceID)
	if err != nil {
		// Try by serial number if not found by ID
		device, err = h.deviceRepo.GetBySerialNo(deviceID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"device": device})
}

func (h *DeviceHandler) UpdateDeviceAPI(c *gin.Context) {
	deviceID := c.Param("id")

	var device models.Device
	if err := c.ShouldBindJSON(&device); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	device.DeviceID = deviceID
	if err := h.deviceRepo.Update(&device); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, device)
}

func (h *DeviceHandler) DeleteDeviceAPI(c *gin.Context) {
	deviceID := c.Param("id")

	if err := h.deviceRepo.Delete(deviceID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Device deleted successfully"})
}

func (h *DeviceHandler) GetDeviceStatsAPI(c *gin.Context) {
	deviceID := c.Param("id")

	// Get device details
	device, err := h.deviceRepo.GetByID(deviceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Device not found"})
		return
	}

	// Get device statistics
	stats, err := h.deviceRepo.GetDeviceStats(deviceID)
	if err != nil {
		// Return basic device info even if stats fail
		c.JSON(http.StatusOK, gin.H{
			"device": device,
			"stats": gin.H{
				"totalJobs":             0,
				"totalEarnings":         0.0,
				"totalDaysRented":       0,
				"averageRentalDuration": 0.0,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"device": device,
		"stats":  stats,
	})
}

func (h *DeviceHandler) GetAvailableDevicesAPI(c *gin.Context) {
	devices, err := h.deviceRepo.GetAvailableDevices()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"devices": devices})
}

// GetAvailableDevicesForJobAPI returns devices available for a specific job's date range
func (h *DeviceHandler) GetAvailableDevicesForJobAPI(c *gin.Context) {
	jobIDStr := c.Param("jobId")
	jobID, err := strconv.ParseUint(jobIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid job ID"})
		return
	}

	// Get job details to extract dates
	// We need access to job repository for this - let me create a simple query
	var job models.Job
	// This is a bit hacky, but we need the job dates. In a better design,
	// this would be passed as query parameters or we'd inject job repository
	db := h.deviceRepo.GetDB() // We need to add this method to device repo
	err = db.First(&job, uint(jobID)).Error
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
		return
	}

	devices, err := h.deviceRepo.GetAvailableDevicesForJob(uint(jobID), job.StartDate, job.EndDate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"devices": devices})
}

// GetDeviceTreeWithAvailability returns the device tree structure with availability checking
func (h *DeviceHandler) GetDeviceTreeWithAvailability(c *gin.Context) {
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	jobID := c.Query("job_id") // Optional - exclude this job from availability check

	var treeData interface{}
	var err error

	// If dates are provided, use availability checking
	if startDate != "" && endDate != "" {
		// Parse dates
		start, parseErr := time.Parse("2006-01-02", startDate)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid start_date format. Use YYYY-MM-DD"})
			return
		}

		end, parseErr := time.Parse("2006-01-02", endDate)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid end_date format. Use YYYY-MM-DD"})
			return
		}

		// Get tree data with availability information
		treeData, err = h.buildTreeDataWithAvailability(start, end, jobID)
	} else {
		// No dates provided - return all devices as available
		treeData, err = h.buildTreeDataWithAllAvailable(jobID)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"treeData": treeData})
}

// Hierarchical tree data structures
type TreeCategory struct {
	ID             uint              `json:"id"`
	Name           string            `json:"name"`
	DeviceCount    int               `json:"device_count"`
	AvailableCount int               `json:"available_count"`
	DirectDevices  []TreeDevice      `json:"direct_devices"` // Devices directly in category
	Products       []TreeProduct     `json:"products"`
	Subcategories  []TreeSubcategory `json:"subcategories"`
}

type TreeSubcategory struct {
	ID                string                `json:"id"`
	Name              string                `json:"name"`
	DeviceCount       int                   `json:"device_count"`
	AvailableCount    int                   `json:"available_count"`
	DirectDevices     []TreeDevice          `json:"direct_devices"` // Devices directly in subcategory
	Products          []TreeProduct         `json:"products"`
	Subbiercategories []TreeSubbiercategory `json:"subbiercategories"`
}

type TreeSubbiercategory struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	DeviceCount    int           `json:"device_count"`
	AvailableCount int           `json:"available_count"`
	Devices        []TreeDevice  `json:"devices"`
	Products       []TreeProduct `json:"products"`
}

type TreeProduct struct {
	ID             uint     `json:"id"`
	Name           string   `json:"name"`
	DeviceCount    int      `json:"device_count"`
	AvailableCount int      `json:"available_count"`
	BrandName      string   `json:"brand_name,omitempty"`
	Manufacturer   string   `json:"manufacturer_name,omitempty"`
	GenericBarcode string   `json:"generic_barcode,omitempty"`
	CountTypeAbbr  string   `json:"count_type_abbr,omitempty"`
	StockQuantity  *float64 `json:"stock_quantity,omitempty"`
	IsAccessory    bool     `json:"is_accessory,omitempty"`
	IsConsumable   bool     `json:"is_consumable,omitempty"`
}

type TreeDevice struct {
	DeviceID     string `json:"device_id"`
	ProductName  string `json:"product_name"`
	ProductID    uint   `json:"product_id,omitempty"`
	SerialNumber string `json:"serial_number"`
	Status       string `json:"status"`
	Available    bool   `json:"available,omitempty"`    // Only included in availability checks
	ConflictJob  string `json:"conflict_job,omitempty"` // Job ID that conflicts
	CaseID       *uint  `json:"case_id,omitempty"`
	CaseName     string `json:"case_name,omitempty"`
}

// buildTreeData creates a hierarchical tree structure with categories, subcategories, subbiercategories, and devices
// OPTIMIZED VERSION - Single query approach with caching to eliminate N+1 problem
func (h *DeviceHandler) buildTreeData() ([]TreeCategory, error) {
	return h.buildProductTreeData(nil, nil, "")
}

// buildTreeDataWithAvailability creates tree structure with device availability for date range
// buildTreeDataWithAllAvailable returns tree data with all devices marked as available
func (h *DeviceHandler) buildTreeDataWithAllAvailable(excludeJobID string) ([]TreeCategory, error) {
	return h.buildProductTreeData(nil, nil, excludeJobID)
}

func (h *DeviceHandler) buildTreeDataWithAvailability(startDate, endDate time.Time, excludeJobID string) ([]TreeCategory, error) {
	return h.buildProductTreeData(&startDate, &endDate, excludeJobID)
}

// updateTreeAvailability recursively updates availability info in tree structure
func (h *DeviceHandler) updateTreeAvailability(categories []TreeCategory, conflicts map[string]string) {
	totalDevices := 0
	unavailableDevices := 0

	for i := range categories {
		// Update direct devices in category
		for j := range categories[i].DirectDevices {
			device := &categories[i].DirectDevices[j]
			totalDevices++
			if conflictJob, hasConflict := conflicts[device.DeviceID]; hasConflict {
				device.Available = false
				device.ConflictJob = conflictJob
				unavailableDevices++
			} else {
				device.Available = true
			}
		}

		// Update subcategories
		for k := range categories[i].Subcategories {
			subcategory := &categories[i].Subcategories[k]

			// Update direct devices in subcategory
			for j := range subcategory.DirectDevices {
				device := &subcategory.DirectDevices[j]
				if conflictJob, hasConflict := conflicts[device.DeviceID]; hasConflict {
					device.Available = false
					device.ConflictJob = conflictJob
				} else {
					device.Available = true
				}
			}

			// Update subbiercategories
			for l := range subcategory.Subbiercategories {
				subbiercategory := &subcategory.Subbiercategories[l]

				// Update devices in subbiercategory
				for j := range subbiercategory.Devices {
					device := &subbiercategory.Devices[j]
					totalDevices++
					if conflictJob, hasConflict := conflicts[device.DeviceID]; hasConflict {
						device.Available = false
						device.ConflictJob = conflictJob
						unavailableDevices++
					} else {
						device.Available = true
					}
				}
			}
		}
	}

}

func (h *DeviceHandler) populateProductAggregates(categories []TreeCategory) {
	for i := range categories {
		category := &categories[i]
		categoryProducts := make(map[uint]*TreeProduct)

		aggregateDevicesIntoProducts(category.DirectDevices, categoryProducts)

		for si := range category.Subcategories {
			subcategory := &category.Subcategories[si]
			subcategoryProducts := make(map[uint]*TreeProduct)

			aggregateDevicesIntoProducts(subcategory.DirectDevices, subcategoryProducts)

			for li := range subcategory.Subbiercategories {
				subbiercategory := &subcategory.Subbiercategories[li]
				subbiercategoryProducts := make(map[uint]*TreeProduct)

				aggregateDevicesIntoProducts(subbiercategory.Devices, subbiercategoryProducts)
				subbiercategory.Products = productMapToSlice(subbiercategoryProducts)

				mergeProductMaps(subcategoryProducts, subbiercategoryProducts)
			}

			subcategory.Products = productMapToSlice(subcategoryProducts)
			mergeProductMaps(categoryProducts, subcategoryProducts)
		}

		category.Products = productMapToSlice(categoryProducts)
	}
}

func aggregateDevicesIntoProducts(devices []TreeDevice, productMap map[uint]*TreeProduct) {
	for _, device := range devices {
		if device.ProductID == 0 {
			continue
		}

		product, exists := productMap[device.ProductID]
		if !exists {
			product = &TreeProduct{
				ID:   device.ProductID,
				Name: device.ProductName,
			}
			productMap[device.ProductID] = product
		}

		product.DeviceCount++
		if device.Available {
			product.AvailableCount++
		}
	}
}

func mergeProductMaps(dest, src map[uint]*TreeProduct) {
	for id, product := range src {
		target, exists := dest[id]
		if !exists {
			dest[id] = &TreeProduct{
				ID:             product.ID,
				Name:           product.Name,
				DeviceCount:    product.DeviceCount,
				AvailableCount: product.AvailableCount,
			}
			continue
		}

		target.DeviceCount += product.DeviceCount
		target.AvailableCount += product.AvailableCount
	}
}

func productMapToSlice(m map[uint]*TreeProduct) []TreeProduct {
	if len(m) == 0 {
		return nil
	}

	products := make([]TreeProduct, 0, len(m))
	for _, product := range m {
		products = append(products, *product)
	}

	sort.Slice(products, func(i, j int) bool {
		if products[i].Name == products[j].Name {
			return products[i].ID < products[j].ID
		}
		return products[i].Name < products[j].Name
	})

	return products
}

// buildOptimizedTreeData performs a single query to get all data and builds the tree structure
func (h *DeviceHandler) buildOptimizedTreeData() ([]TreeCategory, error) {
	// Single query to get all devices with their complete hierarchy
	var devices []models.Device

	err := h.productRepo.GetDB().Model(&models.Device{}).
		Preload("Product").
		Preload("Product.Category").
		Preload("Product.Subcategory").
		Preload("Product.Subbiercategory").
		Joins("LEFT JOIN products ON products.productID = devices.productID").
		Joins("LEFT JOIN categories ON categories.categoryID = products.categoryID").
		Joins("LEFT JOIN subcategories ON subcategories.subcategoryID = products.subcategoryID").
		Joins("LEFT JOIN subbiercategories ON subbiercategories.subbiercategoryID = products.subbiercategoryID").
		Order("categories.name ASC, subcategories.name ASC, subbiercategories.name ASC, devices.serialnumber ASC").
		Find(&devices).Error

	if err != nil {
		return nil, fmt.Errorf("failed to fetch devices with hierarchy: %v", err)
	}

	if len(devices) == 0 {
		return []TreeCategory{}, nil
	}

	// Build the tree structure from the single result set
	return h.buildTreeFromDevices(devices)
}

type productAvailability struct {
	total     int
	available int
}

// buildProductTreeData builds a hierarchy that always includes the latest categories/products (even without devices)
// and applies availability based on date range conflicts.
func (h *DeviceHandler) buildProductTreeData(startDate, endDate *time.Time, excludeJobID string) ([]TreeCategory, error) {
	conflicts := make(map[string]bool)
	if startDate != nil && endDate != nil {
		conflictMap, err := h.getConflictingDevices(*startDate, *endDate, excludeJobID)
		if err != nil {
			return nil, err
		}
		conflicts = conflictMap
	}

	// Query similar to WarehouseCore tree to guarantee all categories/subcategories/products appear
	type row struct {
		CategoryID        uint
		CategoryName      string
		SubcategoryID     sql.NullString
		SubcategoryName   sql.NullString
		SubbiercategoryID sql.NullString
		SubbierName       sql.NullString
		ProductID         sql.NullInt64
		ProductName       sql.NullString
		IsConsumable      int
		IsAccessory       int
		StockQuantity     sql.NullFloat64
		Unit              sql.NullString
		BrandName         sql.NullString
		ManufacturerName  sql.NullString
		GenericBarcode    sql.NullString
		ModelNumber       sql.NullString
		DeviceID          sql.NullString
	}

	query := `
		SELECT
			c.categoryID,
			c.name as category_name,
			sc.subcategoryID,
			sc.name as subcategory_name,
			sbc.subbiercategoryID,
			sbc.name as subbiercategory_name,
			p.productID,
			p.name as product_name,
			COALESCE(p.is_consumable, 0) as is_consumable,
			COALESCE(p.is_accessory, 0) as is_accessory,
			COALESCE(p.stock_quantity, 0) as stock_quantity,
			COALESCE(ct.abbreviation, '') as unit,
			b.name as brand_name,
			m.name as manufacturer_name,
			p.generic_barcode,
			p.model_number,
			d.deviceID
		FROM categories c
		LEFT JOIN subcategories sc ON c.categoryID = sc.categoryID
		LEFT JOIN subbiercategories sbc ON sc.subcategoryID = sbc.subcategoryID
		LEFT JOIN products p ON (p.categoryID = c.categoryID AND (p.subcategoryID IS NULL OR p.subcategoryID = sc.subcategoryID) AND (p.subbiercategoryID IS NULL OR p.subbiercategoryID = sbc.subbiercategoryID))
		LEFT JOIN count_types ct ON p.count_type_id = ct.count_type_id
		LEFT JOIN brands b ON p.brandID = b.brandID
		LEFT JOIN manufacturer m ON p.manufacturerID = m.manufacturerID
		LEFT JOIN devices d ON p.productID = d.productID
		ORDER BY c.name, sc.name, sbc.name, p.name, d.deviceID
	`

	db := h.productRepo.GetDB()
	rows, err := db.Raw(query).Rows()
	if err != nil {
		return nil, fmt.Errorf("failed to query product tree: %v", err)
	}
	defer rows.Close()

	categories := make(map[uint]*TreeCategory)
	subcategories := make(map[string]*TreeSubcategory)
	subbiercategories := make(map[string]*TreeSubbiercategory)

	type productRef struct {
		product *TreeProduct
	}

	catProducts := make(map[uint]map[uint]*productRef)
	subProducts := make(map[string]map[uint]*productRef)
	subBierProducts := make(map[string]map[uint]*productRef)

	for rows.Next() {
		var r row
		if err := rows.Scan(
			&r.CategoryID,
			&r.CategoryName,
			&r.SubcategoryID,
			&r.SubcategoryName,
			&r.SubbiercategoryID,
			&r.SubbierName,
			&r.ProductID,
			&r.ProductName,
			&r.IsConsumable,
			&r.IsAccessory,
			&r.StockQuantity,
			&r.Unit,
			&r.BrandName,
			&r.ManufacturerName,
			&r.GenericBarcode,
			&r.ModelNumber,
			&r.DeviceID,
		); err != nil {
			return nil, fmt.Errorf("failed to scan product tree row: %v", err)
		}

		cat := categories[r.CategoryID]
		if cat == nil {
			cat = &TreeCategory{
				ID:            r.CategoryID,
				Name:          r.CategoryName,
				DirectDevices: []TreeDevice{},
				Products:      []TreeProduct{},
				Subcategories: []TreeSubcategory{},
			}
			categories[r.CategoryID] = cat
		}

		var sub *TreeSubcategory
		if r.SubcategoryID.Valid && r.SubcategoryID.String != "" {
			sub = subcategories[r.SubcategoryID.String]
			if sub == nil {
				newSub := TreeSubcategory{
					ID:                r.SubcategoryID.String,
					Name:              r.SubcategoryName.String,
					DirectDevices:     []TreeDevice{},
					Products:          []TreeProduct{},
					Subbiercategories: []TreeSubbiercategory{},
				}
				cat.Subcategories = append(cat.Subcategories, newSub)
				sub = &cat.Subcategories[len(cat.Subcategories)-1]
				subcategories[r.SubcategoryID.String] = sub
			}
		}

		var subbier *TreeSubbiercategory
		if r.SubbiercategoryID.Valid && r.SubbiercategoryID.String != "" && sub != nil {
			subbier = subbiercategories[r.SubbiercategoryID.String]
			if subbier == nil {
				newSubbier := TreeSubbiercategory{
					ID:             r.SubbiercategoryID.String,
					Name:           r.SubbierName.String,
					Devices:        []TreeDevice{},
					Products:       []TreeProduct{},
					DeviceCount:    0,
					AvailableCount: 0,
				}
				sub.Subbiercategories = append(sub.Subbiercategories, newSubbier)
				subbier = &sub.Subbiercategories[len(sub.Subbiercategories)-1]
				subbiercategories[r.SubbiercategoryID.String] = subbier
			}
		}

		if !r.ProductID.Valid {
			continue
		}

		containerKey := fmt.Sprintf("cat-%d", r.CategoryID)
		productMap := catProducts[r.CategoryID]
		productSlice := &cat.Products
		if sub != nil {
			containerKey = fmt.Sprintf("sub-%s", sub.ID)
			productMap = subProducts[sub.ID]
			productSlice = &sub.Products
			if subbier != nil {
				containerKey = fmt.Sprintf("subbier-%s", subbier.ID)
				productMap = subBierProducts[subbier.ID]
				productSlice = &subbier.Products
			}
		}
		if productMap == nil {
			productMap = make(map[uint]*productRef)
			switch {
			case strings.HasPrefix(containerKey, "cat-"):
				catProducts[r.CategoryID] = productMap
			case strings.HasPrefix(containerKey, "subbier-"):
				subBierProducts[subbier.ID] = productMap
			default:
				subProducts[sub.ID] = productMap
			}
		}

		pid := uint(r.ProductID.Int64)
		ref := productMap[pid]
		if ref == nil {
			tp := TreeProduct{
				ID:             pid,
				Name:           r.ProductName.String,
				DeviceCount:    0,
				AvailableCount: 0,
				IsAccessory:    r.IsAccessory == 1,
				IsConsumable:   r.IsConsumable == 1,
				StockQuantity:  nil,
			}
			if r.BrandName.Valid {
				tp.BrandName = r.BrandName.String
			}
			if r.ManufacturerName.Valid {
				tp.Manufacturer = r.ManufacturerName.String
			}
			if r.GenericBarcode.Valid {
				tp.GenericBarcode = r.GenericBarcode.String
			}
			if r.Unit.Valid {
				tp.CountTypeAbbr = r.Unit.String
			}
			productMap[pid] = &productRef{product: &tp}
			*productSlice = append(*productSlice, tp)
			ref = productMap[pid]
		}

		// Update counts from stock for accessories/consumables
		if (r.IsAccessory == 1 || r.IsConsumable == 1) && r.StockQuantity.Valid {
			stockCount := int(math.Round(r.StockQuantity.Float64))
			if stockCount > ref.product.DeviceCount {
				ref.product.DeviceCount = stockCount
				ref.product.AvailableCount = stockCount
			}
		}

		// Update counts per device for availability
		if r.DeviceID.Valid && r.DeviceID.String != "" {
			ref.product.DeviceCount++
			if !conflicts[r.DeviceID.String] {
				ref.product.AvailableCount++
			}
		}
	}

	// Aggregate counts to parents and sort
	treeCategories := make([]TreeCategory, 0, len(categories))
	for _, cat := range categories {
		for si := range cat.Subcategories {
			sub := &cat.Subcategories[si]
			for bi := range sub.Subbiercategories {
				subbier := &sub.Subbiercategories[bi]
				for pi := range subbier.Products {
					p := &subbier.Products[pi]
					subbier.DeviceCount += p.DeviceCount
					subbier.AvailableCount += p.AvailableCount
					sub.DeviceCount += p.DeviceCount
					sub.AvailableCount += p.AvailableCount
					cat.DeviceCount += p.DeviceCount
					cat.AvailableCount += p.AvailableCount
				}
				sort.Slice(subbier.Products, func(i, j int) bool {
					return strings.ToLower(subbier.Products[i].Name) < strings.ToLower(subbier.Products[j].Name)
				})
			}
			for pi := range sub.Products {
				p := &sub.Products[pi]
				sub.DeviceCount += p.DeviceCount
				sub.AvailableCount += p.AvailableCount
				cat.DeviceCount += p.DeviceCount
				cat.AvailableCount += p.AvailableCount
			}
			sort.Slice(sub.Subbiercategories, func(i, j int) bool {
				return strings.ToLower(sub.Subbiercategories[i].Name) < strings.ToLower(sub.Subbiercategories[j].Name)
			})
			sort.Slice(sub.Products, func(i, j int) bool {
				return strings.ToLower(sub.Products[i].Name) < strings.ToLower(sub.Products[j].Name)
			})
		}
		for pi := range cat.Products {
			p := &cat.Products[pi]
			cat.DeviceCount += p.DeviceCount
			cat.AvailableCount += p.AvailableCount
		}
		sort.Slice(cat.Subcategories, func(i, j int) bool {
			return strings.ToLower(cat.Subcategories[i].Name) < strings.ToLower(cat.Subcategories[j].Name)
		})
		sort.Slice(cat.Products, func(i, j int) bool {
			return strings.ToLower(cat.Products[i].Name) < strings.ToLower(cat.Products[j].Name)
		})
		treeCategories = append(treeCategories, *cat)
	}

	sort.Slice(treeCategories, func(i, j int) bool {
		return strings.ToLower(treeCategories[i].Name) < strings.ToLower(treeCategories[j].Name)
	})

	return treeCategories, nil
}

func (h *DeviceHandler) getConflictingDevices(startDate, endDate time.Time, excludeJobID string) (map[string]bool, error) {
	var conflicts []struct {
		DeviceID string `json:"device_id" gorm:"column:deviceID"`
	}

	query := h.deviceRepo.GetDB().
		Table("jobdevices jd").
		Select("jd.deviceID").
		Joins("JOIN jobs j ON jd.jobID = j.jobID").
		Where("NOT (COALESCE(j.endDate, j.startDate) < ? OR j.startDate > ?)", startDate, endDate)

	if excludeJobID != "" {
		query = query.Where("j.jobID != ?", excludeJobID)
	}

	if err := query.Scan(&conflicts).Error; err != nil {
		return nil, fmt.Errorf("failed to check device availability: %v", err)
	}

	conflictMap := make(map[string]bool, len(conflicts))
	for _, row := range conflicts {
		conflictMap[row.DeviceID] = true
	}

	return conflictMap, nil
}

func (h *DeviceHandler) buildProductAvailabilityMap(conflicts map[string]bool) (map[uint]productAvailability, error) {
	type deviceRow struct {
		DeviceID  string `gorm:"column:deviceID"`
		ProductID uint   `gorm:"column:productID"`
	}

	var rows []deviceRow
	if err := h.productRepo.GetDB().
		Table("devices").
		Select("deviceID, productID").
		Where("productID IS NOT NULL").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to load devices for availability: %v", err)
	}

	availability := make(map[uint]productAvailability)
	for _, row := range rows {
		counts := availability[row.ProductID]
		counts.total++
		if !conflicts[row.DeviceID] {
			counts.available++
		}
		availability[row.ProductID] = counts
	}
	return availability, nil
}

// buildTreeFromDevices constructs the hierarchical tree from a flat list of devices
// COMPLETELY REWRITTEN with proper nested structure building
func (h *DeviceHandler) buildTreeFromDevices(devices []models.Device) ([]TreeCategory, error) {

	// Group devices by their hierarchy path
	categoryGroups := make(map[uint]map[string]map[string][]models.Device)
	uncategorizedDevices := []models.Device{} // Track devices without proper category

	for _, device := range devices {
		if device.Product == nil || device.Product.Category == nil {
			// Instead of skipping, collect uncategorized devices
			uncategorizedDevices = append(uncategorizedDevices, device)
			continue
		}

		// Debug logging for MIX1001 devices
		if device.Product.Subbiercategory != nil && device.Product.Subbiercategory.SubbiercategoryID == "MIX1001" {
			fmt.Printf("🔧 DEBUG MIX1001 Device: %s, Product: %s, SerialNumber: %v\n",
				device.DeviceID, device.Product.Name, device.SerialNumber)
		}

		categoryID := device.Product.Category.CategoryID

		// Initialize category group if needed
		if categoryGroups[categoryID] == nil {
			categoryGroups[categoryID] = make(map[string]map[string][]models.Device)
		}

		var subcategoryID string = "DIRECT"     // For devices directly in category
		var subbiercategoryID string = "DIRECT" // For devices directly in subcategory

		if device.Product.Subcategory != nil {
			subcategoryID = device.Product.Subcategory.SubcategoryID

			if device.Product.Subbiercategory != nil {
				subbiercategoryID = device.Product.Subbiercategory.SubbiercategoryID
			}
		}

		// Initialize subcategory group if needed
		if categoryGroups[categoryID][subcategoryID] == nil {
			categoryGroups[categoryID][subcategoryID] = make(map[string][]models.Device)
		}

		// Add device to appropriate subbiercategory
		categoryGroups[categoryID][subcategoryID][subbiercategoryID] = append(
			categoryGroups[categoryID][subcategoryID][subbiercategoryID], device)
	}

	// Build the tree structure
	var treeCategories []TreeCategory

	for categoryID, subcategoryGroups := range categoryGroups {
		// Find the category info from first device
		var categoryName string
		for _, subGroup := range subcategoryGroups {
			for _, deviceList := range subGroup {
				if len(deviceList) > 0 && deviceList[0].Product != nil && deviceList[0].Product.Category != nil {
					categoryName = deviceList[0].Product.Category.Name
					break
				}
			}
			if categoryName != "" {
				break
			}
		}

		treeCategory := TreeCategory{
			ID:            categoryID,
			Name:          categoryName,
			DeviceCount:   0,
			DirectDevices: []TreeDevice{},
			Subcategories: []TreeSubcategory{},
		}

		for subcategoryID, subbiercategoryGroups := range subcategoryGroups {
			if subcategoryID == "DIRECT" {
				// Devices directly in category (no subcategory)
				if deviceList, exists := subbiercategoryGroups["DIRECT"]; exists {
					for _, device := range deviceList {
						treeCategory.DirectDevices = append(treeCategory.DirectDevices, h.convertToTreeDevice(device))
						treeCategory.DeviceCount++
					}
				}
			} else {
				// Build subcategory
				var subcategoryName string
				var totalDevicesInSubcategory int

				// Find subcategory name from first device
				for _, deviceList := range subbiercategoryGroups {
					if len(deviceList) > 0 && deviceList[0].Product != nil && deviceList[0].Product.Subcategory != nil {
						subcategoryName = deviceList[0].Product.Subcategory.Name
						break
					}
				}

				treeSubcategory := TreeSubcategory{
					ID:                subcategoryID,
					Name:              subcategoryName,
					DeviceCount:       0,
					DirectDevices:     []TreeDevice{},
					Subbiercategories: []TreeSubbiercategory{},
				}

				for subbiercategoryID, deviceList := range subbiercategoryGroups {
					if subbiercategoryID == "DIRECT" {
						// Devices directly in subcategory (no subbiercategory)
						for _, device := range deviceList {
							treeSubcategory.DirectDevices = append(treeSubcategory.DirectDevices, h.convertToTreeDevice(device))
							treeSubcategory.DeviceCount++
							totalDevicesInSubcategory++
						}
					} else {
						// Build subbiercategory
						var subbiercategoryName string
						if len(deviceList) > 0 && deviceList[0].Product != nil && deviceList[0].Product.Subbiercategory != nil {
							subbiercategoryName = deviceList[0].Product.Subbiercategory.Name
						}

						var treeDevices []TreeDevice
						for _, device := range deviceList {
							treeDevices = append(treeDevices, h.convertToTreeDevice(device))
						}

						treeSubbiercategory := TreeSubbiercategory{
							ID:          subbiercategoryID,
							Name:        subbiercategoryName,
							DeviceCount: len(treeDevices),
							Devices:     treeDevices,
						}

						// Debug logging for MIX1001
						if subbiercategoryID == "MIX1001" {
							fmt.Printf("🔧 DEBUG Creating MIX1001 TreeSubbiercategory: Name='%s', DeviceCount=%d\n",
								subbiercategoryName, len(treeDevices))
							for i, device := range treeDevices {
								fmt.Printf("🔧 DEBUG MIX1001 TreeDevice[%d]: %s - %s\n",
									i, device.DeviceID, device.ProductName)
							}
						}

						treeSubcategory.Subbiercategories = append(treeSubcategory.Subbiercategories, treeSubbiercategory)
						treeSubcategory.DeviceCount += len(treeDevices)
						totalDevicesInSubcategory += len(treeDevices)

					}
				}

				// Sort subbiercategories by name
				sort.Slice(treeSubcategory.Subbiercategories, func(i, j int) bool {
					return treeSubcategory.Subbiercategories[i].Name < treeSubcategory.Subbiercategories[j].Name
				})

				treeCategory.Subcategories = append(treeCategory.Subcategories, treeSubcategory)
				treeCategory.DeviceCount += totalDevicesInSubcategory
			}
		}

		// Sort subcategories by name
		sort.Slice(treeCategory.Subcategories, func(i, j int) bool {
			return treeCategory.Subcategories[i].Name < treeCategory.Subcategories[j].Name
		})

		treeCategories = append(treeCategories, treeCategory)
	}

	// Sort categories by name
	sort.Slice(treeCategories, func(i, j int) bool {
		return treeCategories[i].Name < treeCategories[j].Name
	})

	// Add uncategorized devices as a separate category if any exist
	if len(uncategorizedDevices) > 0 {
		uncategorizedCategory := TreeCategory{
			ID:            0, // Special ID for uncategorized
			Name:          "⚠️ Uncategorized Devices",
			DeviceCount:   len(uncategorizedDevices),
			DirectDevices: []TreeDevice{},
			Subcategories: []TreeSubcategory{},
		}

		for _, device := range uncategorizedDevices {
			uncategorizedCategory.DirectDevices = append(uncategorizedCategory.DirectDevices, h.convertToTreeDevice(device))
		}

		// Add uncategorized category at the end
		treeCategories = append(treeCategories, uncategorizedCategory)
	}

	return treeCategories, nil
}

// Helper function to convert Device to TreeDevice
func (h *DeviceHandler) convertToTreeDevice(device models.Device) TreeDevice {
	serialNum := ""
	if device.SerialNumber != nil {
		serialNum = *device.SerialNumber
	}

	productName := "Unknown Product"
	if device.Product != nil && device.Product.Name != "" {
		productName = device.Product.Name
	}

	var productID uint
	if device.ProductID != nil {
		productID = *device.ProductID
	}

	return TreeDevice{
		DeviceID:     device.DeviceID,
		ProductName:  productName,
		ProductID:    productID,
		SerialNumber: serialNum,
		Status:       device.Status,
	}
}

// Helper function to get devices directly in category (without subcategory)
func (h *DeviceHandler) getDirectCategoryDevices(categoryID uint) ([]models.DeviceWithJobInfo, error) {
	// For now, return empty slice - we'll focus on the hierarchical structure first
	// Direct category devices are rare in most setups
	return []models.DeviceWithJobInfo{}, nil
}
