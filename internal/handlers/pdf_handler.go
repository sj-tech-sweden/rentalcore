package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go-barcode-webapp/internal/models"
	"go-barcode-webapp/internal/services/pdf"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// PDFHandler handles PDF upload and processing requests
type PDFHandler struct {
	DB             *gorm.DB
	Extractor      *pdf.PDFExtractor
	Mapper         *pdf.ProductMapper
	CustomerMapper *pdf.CustomerMapper
	JobHandler     *JobHandler
}

// NewPDFHandler creates a new PDF handler
func NewPDFHandler(db *gorm.DB, uploadDir string, jobHandler *JobHandler) *PDFHandler {
	return &PDFHandler{
		DB:             db,
		Extractor:      pdf.NewPDFExtractor(uploadDir),
		Mapper:         pdf.NewProductMapper(db),
		CustomerMapper: pdf.NewCustomerMapper(db),
		JobHandler:     jobHandler,
	}
}

// UploadPDF handles PDF file upload
// POST /api/v1/pdf/upload
func (h *PDFHandler) UploadPDF(c *gin.Context) {
	// Get job ID if provided
	jobIDStr := c.PostForm("job_id")
	var jobID sql.NullInt64
	if jobIDStr != "" {
		id, err := strconv.ParseInt(jobIDStr, 10, 64)
		if err == nil {
			jobID = sql.NullInt64{Int64: id, Valid: true}
		}
	}

	// Get uploaded file
	file, err := c.FormFile("pdf")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
		return
	}

	// Validate file type
	if file.Header.Get("Content-Type") != "application/pdf" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Only PDF files are allowed"})
		return
	}

	// Save file
	upload, err := h.Extractor.SaveUploadedFile(file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to save file: %v", err)})
		return
	}

	// Set job ID if provided
	upload.JobID = jobID

	// Get user ID from session
	if userID, exists := c.Get("userID"); exists {
		if uid, ok := userID.(int64); ok {
			upload.UploadedBy = sql.NullInt64{Int64: uid, Valid: true}
		}
	}

	// Save upload record to database
	if err := h.DB.Create(upload).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save upload record"})
		return
	}

	// Start processing asynchronously
	go h.processUploadAsync(upload.UploadID)

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"upload_id": upload.UploadID,
		"message":   "PDF uploaded successfully, processing started",
	})
}

// processUploadAsync processes the PDF asynchronously
func (h *PDFHandler) processUploadAsync(uploadID uint64) {
	// Update status to processing
	h.DB.Model(&models.PDFUpload{}).Where("upload_id = ?", uploadID).Updates(map[string]interface{}{
		"processing_status":     "processing",
		"processing_started_at": time.Now(),
	})

	// Get upload record
	var upload models.PDFUpload
	if err := h.DB.First(&upload, uploadID).Error; err != nil {
		h.markProcessingFailed(uploadID, fmt.Sprintf("Upload not found: %v", err))
		return
	}

	// Extract text
	rawText, err := h.Extractor.ExtractText(upload.FilePath)
	if err != nil {
		h.markProcessingFailed(uploadID, fmt.Sprintf("Text extraction failed: %v", err))
		return
	}

	// Parse invoice data
	parsedData, err := h.Extractor.ParseInvoiceData(rawText)
	if err != nil {
		h.markProcessingFailed(uploadID, fmt.Sprintf("Data parsing failed: %v", err))
		return
	}

	// Attempt customer auto-mapping
	if parsedData.CustomerName != "" && h.CustomerMapper != nil {
		if _, customer, confidence, err := h.CustomerMapper.FindBestMatch(parsedData.CustomerName); err == nil && customer != nil && confidence >= 70 {
			customerID := int(customer.CustomerID)
			parsedData.CustomerID = &customerID
		}
	}

	parsedData.RawText = rawText

	// Convert to JSON
	extractedDataJSON, err := parsedData.ToJSON()
	if err != nil {
		h.markProcessingFailed(uploadID, fmt.Sprintf("JSON conversion failed: %v", err))
		return
	}

	// Create extraction record
	extraction := models.PDFExtraction{
		UploadID:         uploadID,
		RawText:          sql.NullString{String: rawText, Valid: true},
		ExtractedData:    sql.NullString{String: extractedDataJSON, Valid: true},
		ConfidenceScore:  sql.NullFloat64{Float64: parsedData.ConfidenceScore, Valid: true},
		PageCount:        1, // TODO: Get actual page count
		ExtractionMethod: "regex_parser",
		CustomerName:     sql.NullString{String: parsedData.CustomerName, Valid: parsedData.CustomerName != ""},
		DocumentNumber:   sql.NullString{String: parsedData.DocumentNumber, Valid: parsedData.DocumentNumber != ""},
		TotalAmount:      sql.NullFloat64{Float64: parsedData.TotalAmount, Valid: parsedData.TotalAmount > 0},
		DiscountAmount:   sql.NullFloat64{Float64: parsedData.DiscountAmount, Valid: parsedData.DiscountAmount > 0},
	}
	if parsedData.CustomerID != nil && *parsedData.CustomerID > 0 {
		extraction.CustomerID = sql.NullInt64{Int64: int64(*parsedData.CustomerID), Valid: true}
	}

	if !parsedData.DocumentDate.IsZero() {
		extraction.DocumentDate = sql.NullTime{Time: parsedData.DocumentDate, Valid: true}
	}

	metadata := map[string]interface{}{}
	if !parsedData.StartDate.IsZero() {
		metadata["start_date"] = parsedData.StartDate.Format(time.RFC3339)
	}
	if !parsedData.EndDate.IsZero() {
		metadata["end_date"] = parsedData.EndDate.Format(time.RFC3339)
	}
	if len(metadata) > 0 {
		if metaBytes, err := json.Marshal(metadata); err == nil {
			extraction.Metadata = sql.NullString{String: string(metaBytes), Valid: true}
		}
	}

	// Save extraction
	if err := h.DB.Create(&extraction).Error; err != nil {
		h.markProcessingFailed(uploadID, fmt.Sprintf("Failed to save extraction: %v", err))
		return
	}

	// Create extraction items
	for _, item := range parsedData.Items {
		extractionItem := models.PDFExtractionItem{
			ExtractionID:   extraction.ExtractionID,
			LineNumber:     sql.NullInt64{Int64: int64(item.LineNumber), Valid: true},
			RawProductText: item.ProductText,
			Quantity:       sql.NullInt64{Int64: int64(item.Quantity), Valid: true},
			UnitPrice:      sql.NullFloat64{Float64: item.UnitPrice, Valid: item.UnitPrice > 0},
			LineTotal:      sql.NullFloat64{Float64: item.LineTotal, Valid: item.LineTotal > 0},
			MappingStatus:  "pending",
		}

		// Try to find product mapping
		_, product, confidence, err := h.Mapper.FindBestMatch(item.ProductText)
		if err == nil && product != nil && confidence >= 70 {
			extractionItem.MappedProductID = sql.NullInt64{Int64: int64(product.ProductID), Valid: true}
			extractionItem.MappingConfidence = sql.NullFloat64{Float64: confidence, Valid: true}
			extractionItem.MappingStatus = "auto_mapped"
		}

		h.DB.Create(&extractionItem)
	}

	// Mark as completed
	h.DB.Model(&models.PDFUpload{}).Where("upload_id = ?", uploadID).Updates(map[string]interface{}{
		"processing_status":       "completed",
		"processing_completed_at": time.Now(),
	})
}

// markProcessingFailed marks upload as failed
func (h *PDFHandler) markProcessingFailed(uploadID uint64, errorMsg string) {
	h.DB.Model(&models.PDFUpload{}).Where("upload_id = ?", uploadID).Updates(map[string]interface{}{
		"processing_status":       "failed",
		"processing_completed_at": time.Now(),
		"error_message":           errorMsg,
	})
}

// GetExtractionResult retrieves extraction results
// GET /api/v1/pdf/extraction/:upload_id
func (h *PDFHandler) GetExtractionResult(c *gin.Context) {
	uploadID := c.Param("upload_id")

	var upload models.PDFUpload
	if err := h.DB.First(&upload, uploadID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Upload not found"})
		return
	}

	var extraction models.PDFExtraction
	if err := h.DB.Where("upload_id = ?", uploadID).First(&extraction).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Extraction not found"})
		return
	}

	var items []models.PDFExtractionItem
	h.DB.Where("extraction_id = ?", extraction.ExtractionID).Find(&items)

	// Build response
	response := models.PDFExtractionResponse{
		UploadID:     upload.UploadID,
		ExtractionID: extraction.ExtractionID,
		Items:        items,
	}

	if extraction.CustomerName.Valid {
		response.CustomerName = extraction.CustomerName.String
	}
	if extraction.CustomerID.Valid {
		customerID := int(extraction.CustomerID.Int64)
		response.CustomerID = &customerID
	}
	if extraction.DocumentNumber.Valid {
		response.DocumentNumber = extraction.DocumentNumber.String
	}
	if extraction.DocumentDate.Valid {
		response.DocumentDate = extraction.DocumentDate.Time.Format("2006-01-02")
	}
	if extraction.Metadata.Valid {
		var meta map[string]string
		if err := json.Unmarshal([]byte(extraction.Metadata.String), &meta); err == nil {
			if start, ok := meta["start_date"]; ok {
				response.StartDate = start
			}
			if end, ok := meta["end_date"]; ok {
				response.EndDate = end
			}
		}
	}
	if extraction.TotalAmount.Valid {
		response.TotalAmount = extraction.TotalAmount.Float64
	}
	if extraction.DiscountAmount.Valid {
		response.DiscountAmount = extraction.DiscountAmount.Float64
	}
	if extraction.RawText.Valid {
		response.RawText = extraction.RawText.String
	}
	if extraction.ConfidenceScore.Valid {
		response.ConfidenceScore = extraction.ConfidenceScore.Float64
	}

	c.JSON(http.StatusOK, response)
}

// SaveProductMapping saves a manual product mapping
// POST /api/v1/pdf/mapping
func (h *PDFHandler) SaveProductMapping(c *gin.Context) {
	var req struct {
		PDFText   string `json:"pdf_text" binding:"required"`
		ProductID int    `json:"product_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID := int64(1) // TODO: Get from session
	if uid, exists := c.Get("userID"); exists {
		if id, ok := uid.(int64); ok {
			userID = id
		}
	}

	if err := h.Mapper.SaveMapping(req.PDFText, req.ProductID, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save mapping"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Mapping saved successfully"})
}

// GetProductSuggestions gets product suggestions for PDF text
// GET /api/v1/pdf/suggestions?text=...
func (h *PDFHandler) GetProductSuggestions(c *gin.Context) {
	productText := c.Query("text")
	if productText == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Text parameter required"})
		return
	}

	suggestions, err := h.Mapper.FindSimilarProducts(productText, 10)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to find suggestions"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"suggestions": suggestions})
}

// UpdateItemMapping updates the product mapping for an extraction item
// PUT /api/v1/pdf/items/:item_id/mapping
func (h *PDFHandler) UpdateItemMapping(c *gin.Context) {
	itemID := c.Param("item_id")

	var req struct {
		ProductID int    `json:"product_id" binding:"required"`
		Status    string `json:"status"` // 'user_confirmed', 'user_rejected', etc.
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	status := req.Status
	if status == "" {
		status = "user_confirmed"
	}

	updates := map[string]interface{}{
		"mapped_product_id":  req.ProductID,
		"mapping_status":     status,
		"mapping_confidence": 100.0, // User confirmed = 100%
	}

	if err := h.DB.Model(&models.PDFExtractionItem{}).Where("item_id = ?", itemID).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update mapping"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Mapping updated successfully"})
}

// ShowReviewScreen displays the PDF extraction review screen
// GET /pdf/review/:upload_id
func (h *PDFHandler) ShowReviewScreen(c *gin.Context) {
	uploadID := c.Param("upload_id")

	// Get upload record
	var upload models.PDFUpload
	if err := h.DB.First(&upload, uploadID).Error; err != nil {
		c.HTML(http.StatusNotFound, "error.html", gin.H{
			"error": "Upload not found",
		})
		return
	}

	// Get extraction record
	var extraction models.PDFExtraction
	if err := h.DB.Where("upload_id = ?", uploadID).First(&extraction).Error; err != nil {
		c.HTML(http.StatusNotFound, "error.html", gin.H{
			"error": "Extraction not found - processing may still be in progress",
		})
		return
	}

	// Get extraction items
	var items []models.PDFExtractionItem
	h.DB.Where("extraction_id = ?", extraction.ExtractionID).Order("line_number").Find(&items)

	mappedProducts := make(map[uint64]models.Product)
	productIDs := make([]int64, 0)
	for _, item := range items {
		if item.MappedProductID.Valid {
			productIDs = append(productIDs, item.MappedProductID.Int64)
		}
	}

	if len(productIDs) > 0 {
		var products []models.Product
		if err := h.DB.Where("productID IN ?", productIDs).Find(&products).Error; err == nil {
			productLookup := make(map[int64]models.Product, len(products))
			for _, product := range products {
				productLookup[int64(product.ProductID)] = product
			}
			for _, item := range items {
				if item.MappedProductID.Valid {
					if product, ok := productLookup[item.MappedProductID.Int64]; ok {
						mappedProducts[item.ItemID] = product
					}
				}
			}
		}
	}

	// Prepare response data
	data := gin.H{
		"upload":         upload,
		"extraction":     extraction,
		"items":          items,
		"mappedProducts": mappedProducts,
		"pageTitle":      "PDF Extraction Review",
	}

	// Add optional fields
	if extraction.CustomerName.Valid {
		data["customerName"] = extraction.CustomerName.String
	}
	if extraction.DocumentNumber.Valid {
		data["documentNumber"] = extraction.DocumentNumber.String
	}
	if extraction.DocumentDate.Valid {
		data["documentDate"] = extraction.DocumentDate.Time.Format("2006-01-02")
	}
	if extraction.Metadata.Valid {
		var meta map[string]string
		if err := json.Unmarshal([]byte(extraction.Metadata.String), &meta); err == nil {
			if startStr, ok := meta["start_date"]; ok {
				if t, err := time.Parse(time.RFC3339, startStr); err == nil {
					data["startDate"] = t
				}
			}
			if endStr, ok := meta["end_date"]; ok {
				if t, err := time.Parse(time.RFC3339, endStr); err == nil {
					data["endDate"] = t
				}
			}
		}
	}
	if extraction.TotalAmount.Valid {
		data["totalAmount"] = extraction.TotalAmount.Float64
	}
	if extraction.DiscountAmount.Valid {
		data["discountAmount"] = extraction.DiscountAmount.Float64
	}

	c.HTML(http.StatusOK, "pdf_review.html", data)
}

// ShowMappingScreen displays the PDF product mapping screen
// GET /pdf/mapping/:extraction_id
func (h *PDFHandler) ShowMappingScreen(c *gin.Context) {
	extractionID := c.Param("extraction_id")

	// Get extraction record
	var extraction models.PDFExtraction
	if err := h.DB.First(&extraction, extractionID).Error; err != nil {
		c.HTML(http.StatusNotFound, "error.html", gin.H{
			"error": "Extraction not found",
		})
		return
	}

	// Get upload record
	var upload models.PDFUpload
	h.DB.First(&upload, extraction.UploadID)

	// Get extraction items with product mappings
	var items []models.PDFExtractionItem
	h.DB.Where("extraction_id = ?", extractionID).Order("line_number").Find(&items)

	// For each item, get mapping suggestions
	itemsWithSuggestions := make([]gin.H, 0, len(items))
	for _, item := range items {
		suggestions, _ := h.Mapper.FindSimilarProducts(item.RawProductText, 5)

		itemData := gin.H{
			"item":        item,
			"suggestions": suggestions,
		}

		// Add mapped product if exists
		if item.MappedProductID.Valid {
			var product models.Product
			if err := h.DB.First(&product, item.MappedProductID.Int64).Error; err == nil {
				itemData["mappedProduct"] = product
			}
		}

		itemsWithSuggestions = append(itemsWithSuggestions, itemData)
	}

	var meta map[string]string
	if extraction.Metadata.Valid {
		_ = json.Unmarshal([]byte(extraction.Metadata.String), &meta)
	}

	parseMetaDate := func(key string) *time.Time {
		if meta == nil {
			return nil
		}
		if value, ok := meta[key]; ok && value != "" {
			if t, err := time.Parse(time.RFC3339, value); err == nil {
				return &t
			}
		}
		return nil
	}

	startDate := parseMetaDate("start_date")
	endDate := parseMetaDate("end_date")

	if startDate == nil && extraction.DocumentDate.Valid {
		value := extraction.DocumentDate.Time
		startDate = &value
	}

	discountType := "amount"
	if extraction.Metadata.Valid {
		var meta map[string]string
		if err := json.Unmarshal([]byte(extraction.Metadata.String), &meta); err == nil {
			if dt := strings.TrimSpace(meta["discount_type"]); dt != "" {
				discountType = dt
			}
		}
	}

	data := gin.H{
		"extraction":   extraction,
		"upload":       upload,
		"items":        itemsWithSuggestions,
		"pageTitle":    "PDF Product Mapping",
		"startDate":    formatDateInput(startDate),
		"endDate":      formatDateInput(endDate),
		"discountType": discountType,
	}

	if extraction.TotalAmount.Valid {
		data["totalAmount"] = extraction.TotalAmount.Float64
	}

	if extraction.DiscountAmount.Valid {
		data["discountAmount"] = extraction.DiscountAmount.Float64
	}

	if extraction.TotalAmount.Valid {
		net := extraction.TotalAmount.Float64
		if extraction.DiscountAmount.Valid {
			net -= extraction.DiscountAmount.Float64
			if net < 0 {
				net = 0
			}
		}
		data["netAmount"] = net
	}

	if extraction.CustomerName.Valid {
		data["extractedCustomerName"] = extraction.CustomerName.String
	}
	if extraction.CustomerID.Valid {
		data["selectedCustomerID"] = extraction.CustomerID.Int64
		var customer models.Customer
		if err := h.DB.First(&customer, extraction.CustomerID.Int64).Error; err == nil {
			data["selectedCustomerName"] = customer.GetDisplayName()
		}
	}

	if prefill := h.buildCustomerPrefill(&extraction); prefill != nil {
		data["hasCustomerPrefill"] = true
		if raw, err := json.Marshal(prefill); err == nil {
			data["customerPrefillJSON"] = template.JS(raw)
		} else {
			data["customerPrefillJSON"] = template.JS("null")
		}
	} else {
		data["customerPrefillJSON"] = template.JS("null")
		data["hasCustomerPrefill"] = false
	}

	c.HTML(http.StatusOK, "pdf_mapping.html", data)
}

// RunAutoMapping runs automatic product mapping for an extraction
// POST /api/v1/pdf/auto-map/:extraction_id
func (h *PDFHandler) RunAutoMapping(c *gin.Context) {
	extractionID := c.Param("extraction_id")

	// Get all items for this extraction
	var items []models.PDFExtractionItem
	if err := h.DB.Where("extraction_id = ? AND mapping_status = ?", extractionID, "pending").Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load items"})
		return
	}

	autoMappedCount := 0
	lowConfidenceCount := 0

	// Run auto-mapping for each item
	for _, item := range items {
		_, product, confidence, err := h.Mapper.FindBestMatch(item.RawProductText)

		if err == nil && product != nil {
			updates := map[string]interface{}{
				"mapped_product_id":  product.ProductID,
				"mapping_confidence": confidence,
			}

			// Auto-accept if confidence >= 80%
			if confidence >= 80.0 {
				updates["mapping_status"] = "auto_mapped"
				autoMappedCount++
			} else {
				updates["mapping_status"] = "pending" // Keep pending for manual review
				lowConfidenceCount++
			}

			h.DB.Model(&models.PDFExtractionItem{}).Where("item_id = ?", item.ItemID).Updates(updates)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success":        true,
		"auto_mapped":    autoMappedCount,
		"low_confidence": lowConfidenceCount,
		"message":        fmt.Sprintf("Auto-mapped %d items with high confidence, %d items need manual review", autoMappedCount, lowConfidenceCount),
	})
}

// SearchProducts searches products for manual mapping
// GET /api/v1/pdf/products/search?q=term
func (h *PDFHandler) SearchProducts(c *gin.Context) {
	query := c.Query("q")
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Query parameter required"})
		return
	}

	var products []models.Product
	searchPattern := "%" + query + "%"

	if err := h.DB.Where("name LIKE ? OR description LIKE ?", searchPattern, searchPattern).
		Limit(20).
		Find(&products).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Search failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"products": products})
}

// SearchCustomers searches customers for manual mapping
// GET /api/v1/pdf/customers/search?q=term
func (h *PDFHandler) SearchCustomers(c *gin.Context) {
	query := strings.TrimSpace(c.Query("q"))
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Query parameter required"})
		return
	}

	pattern := "%" + query + "%"
	var customers []models.Customer
	if err := h.DB.Where("companyname LIKE ? OR lastname LIKE ? OR firstname LIKE ?", pattern, pattern, pattern).
		Limit(20).
		Find(&customers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Customer search failed"})
		return
	}

	results := make([]gin.H, 0, len(customers))
	for _, customer := range customers {
		results = append(results, gin.H{
			"customerID":   customer.CustomerID,
			"displayName":  customer.GetDisplayName(),
			"companyname":  customer.CompanyName,
			"firstname":    customer.FirstName,
			"lastname":     customer.LastName,
			"city":         customer.City,
			"email":        customer.Email,
			"phonenumber":  customer.PhoneNumber,
			"customertype": customer.CustomerType,
		})
	}

	c.JSON(http.StatusOK, gin.H{"customers": results})
}

// SaveManualMapping saves a manual product mapping
// POST /api/v1/pdf/manual-map/:item_id
func (h *PDFHandler) SaveManualMapping(c *gin.Context) {
	itemID := c.Param("item_id")

	var req struct {
		ProductID int    `json:"product_id" binding:"required"`
		ItemType  string `json:"item_type"` // 'product', 'customer', 'discount', 'other'
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get the extraction item
	var item models.PDFExtractionItem
	if err := h.DB.First(&item, itemID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Item not found"})
		return
	}

	// Update the item
	updates := map[string]interface{}{
		"mapped_product_id":  req.ProductID,
		"mapping_status":     "user_confirmed",
		"mapping_confidence": 100.0,
	}

	if err := h.DB.Model(&models.PDFExtractionItem{}).Where("item_id = ?", itemID).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save mapping"})
		return
	}

	var product models.Product
	if err := h.DB.First(&product, req.ProductID).Error; err != nil {
		product = models.Product{}
	}

	// Save to learning table for future auto-mapping
	userID := int64(1)
	if uid, exists := c.Get("userID"); exists {
		if id, ok := uid.(int64); ok {
			userID = id
		}
	}

	if err := h.Mapper.SaveMapping(item.RawProductText, req.ProductID, userID); err != nil {
		log.Printf("warning: failed to persist manual mapping for item %d: %v", item.ItemID, err)
	}

	h.recordMappingEvent(item.ExtractionID, item.ItemID, req.ProductID, item.RawProductText, userID)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Mapping saved and learned for future use",
		"product": product,
		"item_id": item.ItemID,
	})
}

// SaveCustomerMapping saves a manual customer mapping for the extraction
// POST /api/v1/pdf/customer-map/:extraction_id
func (h *PDFHandler) SaveCustomerMapping(c *gin.Context) {
	extractionID := c.Param("extraction_id")

	var req struct {
		CustomerID   int    `json:"customer_id" binding:"required"`
		CustomerText string `json:"customer_text"`
	}

	if err := c.ShouldBindJSON(&req); err != nil || req.CustomerID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Valid customer_id is required"})
		return
	}

	updates := map[string]interface{}{
		"customer_id": req.CustomerID,
	}

	if strings.TrimSpace(req.CustomerText) != "" {
		updates["customer_name"] = req.CustomerText
	}

	if err := h.DB.Model(&models.PDFExtraction{}).Where("extraction_id = ?", extractionID).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update extraction"})
		return
	}

	text := strings.TrimSpace(req.CustomerText)
	if text == "" {
		var extraction models.PDFExtraction
		if err := h.DB.Select("customer_name").First(&extraction, extractionID).Error; err == nil && extraction.CustomerName.Valid {
			text = extraction.CustomerName.String
		}
	}

	if text != "" && h.CustomerMapper != nil {
		userID := int64(1)
		if uid, exists := c.Get("userID"); exists {
			if id, ok := uid.(int64); ok {
				userID = id
			}
		}
		_ = h.CustomerMapper.SaveMapping(text, req.CustomerID, userID)
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// CreateCustomerFromExtraction creates a CRM customer based on OCR data
func (h *PDFHandler) CreateCustomerFromExtraction(c *gin.Context) {
	extractionID := c.Param("extraction_id")

	var extraction models.PDFExtraction
	if err := h.DB.First(&extraction, extractionID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Extraction not found"})
		return
	}

	var req struct {
		CompanyName string `json:"company_name"`
		FirstName   string `json:"first_name"`
		LastName    string `json:"last_name"`
		Street      string `json:"street"`
		Zip         string `json:"zip"`
		City        string `json:"city"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid payload"})
		return
	}

	if strings.TrimSpace(req.CompanyName) == "" && strings.TrimSpace(req.LastName) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Company or last name is required"})
		return
	}

	customer := &models.Customer{}
	customer.CompanyName = optionalString(req.CompanyName)
	customer.FirstName = optionalString(req.FirstName)
	customer.LastName = optionalString(req.LastName)
	customer.Street = optionalString(req.Street)
	customer.ZIP = optionalString(req.Zip)
	customer.City = optionalString(req.City)

	if err := h.JobHandler.customerRepo.Create(customer); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create customer"})
		return
	}

	displayName := customer.GetDisplayName()
	updates := map[string]interface{}{
		"customer_id": sql.NullInt64{Int64: int64(customer.CustomerID), Valid: true},
	}
	if displayName != "" {
		updates["customer_name"] = sql.NullString{String: displayName, Valid: true}
	}

	if err := h.DB.Model(&models.PDFExtraction{}).Where("extraction_id = ?", extraction.ExtractionID).Updates(updates).Error; err != nil {
		log.Printf("warning: failed to update extraction with new customer: %v", err)
	}

	if h.CustomerMapper != nil && displayName != "" {
		_ = h.CustomerMapper.SaveMapping(displayName, int(customer.CustomerID), 1)
	}

	c.JSON(http.StatusOK, gin.H{
		"success":      true,
		"customer_id":  customer.CustomerID,
		"display_name": displayName,
	})
}

// FinalizeExtraction creates or links a job for the PDF extraction
// POST /api/v1/pdf/extractions/:extraction_id/finalize
func (h *PDFHandler) FinalizeExtraction(c *gin.Context) {
	extractionID := c.Param("extraction_id")

	var extraction models.PDFExtraction
	if err := h.DB.First(&extraction, extractionID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Extraction not found"})
		return
	}

	var upload models.PDFUpload
	if err := h.DB.First(&upload, extraction.UploadID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Upload not found"})
		return
	}

	var req struct {
		StartDate     string   `json:"start_date"`
		EndDate       string   `json:"end_date"`
		CustomerID    *int     `json:"customer_id"`
		DiscountValue *float64 `json:"discount_value"`
		DiscountType  string   `json:"discount_type"`
	}

	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid finalize payload"})
			return
		}
	}

	if req.CustomerID != nil && *req.CustomerID > 0 {
		extraction.CustomerID = sql.NullInt64{Int64: int64(*req.CustomerID), Valid: true}
		h.DB.Model(&models.PDFExtraction{}).Where("extraction_id = ?", extraction.ExtractionID).
			Update("customer_id", extraction.CustomerID)
	}

	if err := h.persistExtractionMappings(c, extraction.ExtractionID); err != nil {
		log.Printf("warning: failed to persist mappings for extraction %d: %v", extraction.ExtractionID, err)
	}

	var meta map[string]string
	if extraction.Metadata.Valid {
		_ = json.Unmarshal([]byte(extraction.Metadata.String), &meta)
	}
	if meta == nil {
		meta = map[string]string{}
	}

	discountType := strings.TrimSpace(meta["discount_type"])
	if discountType != "percent" {
		discountType = "amount"
	}

	if req.DiscountType != "" {
		candidate := strings.ToLower(strings.TrimSpace(req.DiscountType))
		if candidate == "percent" {
			discountType = "percent"
		} else {
			discountType = "amount"
		}
	}

	if req.DiscountValue != nil {
		if *req.DiscountValue > 0 {
			extraction.DiscountAmount = sql.NullFloat64{Float64: *req.DiscountValue, Valid: true}
		} else {
			extraction.DiscountAmount = sql.NullFloat64{}
		}
		if err := h.DB.Model(&models.PDFExtraction{}).
			Where("extraction_id = ?", extraction.ExtractionID).
			Update("discount_amount", extraction.DiscountAmount).Error; err != nil {
			log.Printf("warning: failed to update extraction discount: %v", err)
		}
	}

	meta["discount_type"] = discountType

	discountValue := 0.0
	if extraction.DiscountAmount.Valid {
		discountValue = extraction.DiscountAmount.Float64
	}

	// If job already linked, keep it in sync with the latest mappings
	if upload.JobID.Valid {
		var job models.Job
		if err := h.DB.First(&job, upload.JobID.Int64).Error; err == nil {
			warningMsg, assignErr := h.assignProductsToJob(&job, extraction.ExtractionID)
			if assignErr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": assignErr.Error()})
				return
			}

			if err := h.DB.Model(&models.Job{}).Where("job_id = ?", job.JobID).
				Updates(map[string]interface{}{
					"discount":      discountValue,
					"discount_type": discountType,
				}).Error; err != nil {
				log.Printf("warning: failed to persist updated discount for job %d: %v", job.JobID, err)
			}

			_ = h.JobHandler.jobRepo.CalculateAndUpdateRevenue(job.JobID)

			response := gin.H{
				"success":  true,
				"job_id":   job.JobID,
				"jobs_url": fmt.Sprintf("/jobs?editJob=%d", job.JobID),
			}
			if warningMsg != "" {
				response["warning"] = warningMsg
			}
			c.JSON(http.StatusOK, response)
			return
		}
	}

	customerID, err := h.ensureCustomerForExtraction(&extraction)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	statusID, err := h.findDefaultJobStatus()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	parseMetaDate := func(value string) *time.Time {
		if value == "" {
			return nil
		}
		if t, err := time.Parse(time.RFC3339, value); err == nil {
			return &t
		}
		return nil
	}

	startDate := parseMetaDate(meta["start_date"])
	endDate := parseMetaDate(meta["end_date"])

	if req.StartDate != "" {
		if t, err := time.Parse("2006-01-02", req.StartDate); err == nil {
			startDate = &t
			meta["start_date"] = t.Format(time.RFC3339)
		}
	}

	if req.EndDate != "" {
		if t, err := time.Parse("2006-01-02", req.EndDate); err == nil {
			endDate = &t
			meta["end_date"] = t.Format(time.RFC3339)
		}
	}

	if startDate == nil && extraction.DocumentDate.Valid {
		value := extraction.DocumentDate.Time
		startDate = &value
		meta["start_date"] = value.Format(time.RFC3339)
	}

	if len(meta) > 0 {
		if metaBytes, err := json.Marshal(meta); err == nil {
			h.DB.Model(&models.PDFExtraction{}).Where("extraction_id = ?", extraction.ExtractionID).
				Update("metadata", string(metaBytes))
			extraction.Metadata = sql.NullString{String: string(metaBytes), Valid: true}
		}
	}

	revenue := 0.0
	if extraction.TotalAmount.Valid {
		revenue = extraction.TotalAmount.Float64
	}

	desc := fmt.Sprintf("Generated from %s (Extraction %d)", upload.OriginalFilename, extraction.ExtractionID)
	truncatedDesc := truncateString(desc, 48)

	job := models.Job{
		CustomerID:   customerID,
		StatusID:     statusID,
		Discount:     discountValue,
		DiscountType: discountType,
		Revenue:      revenue,
	}
	job.Description = &truncatedDesc
	if startDate != nil {
		job.StartDate = startDate
	}
	if endDate != nil {
		job.EndDate = endDate
	}

	if err := h.DB.Create(&job).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to create job: %v", err),
		})
		return
	}

	warningMsg, assignErr := h.assignProductsToJob(&job, extraction.ExtractionID)
	if assignErr != nil {
		h.DB.Delete(&job)
		c.JSON(http.StatusBadRequest, gin.H{"error": assignErr.Error()})
		return
	}

	h.DB.Model(&models.PDFUpload{}).Where("upload_id = ?", upload.UploadID).
		Update("job_id", job.JobID)

	response := gin.H{
		"success":  true,
		"job_id":   job.JobID,
		"jobs_url": fmt.Sprintf("/jobs?editJob=%d", job.JobID),
	}
	if warningMsg != "" {
		response["warning"] = warningMsg
	}

	c.JSON(http.StatusOK, response)
}

func (h *PDFHandler) ensureCustomerForExtraction(extraction *models.PDFExtraction) (uint, error) {
	if extraction.CustomerID.Valid && extraction.CustomerID.Int64 > 0 {
		var existing models.Customer
		if err := h.DB.First(&existing, extraction.CustomerID.Int64).Error; err == nil {
			return existing.CustomerID, nil
		}
	}

	return 0, fmt.Errorf("Please select a customer before creating this job")
}

func (h *PDFHandler) findDefaultJobStatus() (uint, error) {
	var status models.Status
	if err := h.DB.Where("status LIKE ?", "%Draft%").First(&status).Error; err == nil {
		return status.StatusID, nil
	}

	if err := h.DB.Order("statusID").First(&status).Error; err == nil {
		return status.StatusID, nil
	}

	return 0, fmt.Errorf("no job status configured")
}

func truncateString(value string, max int) string {
	if len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}

type productPricingAggregate struct {
	totalAmount    float64
	pricedQuantity int
}

func (h *PDFHandler) assignProductsToJob(job *models.Job, extractionID uint64) (string, error) {
	if h.JobHandler == nil {
		return "", nil
	}

	var items []models.PDFExtractionItem
	if err := h.DB.Where("extraction_id = ?", extractionID).Find(&items).Error; err != nil {
		return "", err
	}

	productCounts := make(map[uint]int)
	pricingAggregates := make(map[uint]*productPricingAggregate)
	for _, item := range items {
		if !item.MappedProductID.Valid {
			continue
		}
		pid := uint(item.MappedProductID.Int64)
		qty := 1
		if item.Quantity.Valid && item.Quantity.Int64 > 0 {
			qty = int(item.Quantity.Int64)
		}
		productCounts[pid] += qty

		lineTotal := 0.0
		if item.LineTotal.Valid && item.LineTotal.Float64 > 0 {
			lineTotal = item.LineTotal.Float64
		} else if item.UnitPrice.Valid && item.UnitPrice.Float64 > 0 {
			lineTotal = item.UnitPrice.Float64 * float64(qty)
		}
		if lineTotal > 0 && qty > 0 {
			agg := pricingAggregates[pid]
			if agg == nil {
				agg = &productPricingAggregate{}
				pricingAggregates[pid] = agg
			}
			agg.totalAmount += lineTotal
			agg.pricedQuantity += qty
		}
	}

	if len(productCounts) == 0 {
		return "", nil
	}

	priceOverrides, err := h.computePriceOverrides(pricingAggregates)
	if err != nil {
		return "", err
	}

	selections := make([]JobProductSelection, 0, len(productCounts))
	for pid, qty := range productCounts {
		if qty <= 0 {
			continue
		}
		selections = append(selections, JobProductSelection{
			ProductID: pid,
			Quantity:  qty,
		})
	}

	if len(selections) == 0 {
		return "", nil
	}

	if err := h.JobHandler.ApplyProductSelections(job, selections); err != nil {
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, "not enough available devices") {
			return err.Error(), nil
		}
		return fmt.Sprintf("Could not auto-assign devices: %s", err.Error()), nil
	}

	if err := h.applyCustomPriceOverrides(job, priceOverrides); err != nil {
		return "", err
	}

	return "", nil
}

type customerPrefill struct {
	CompanyName string   `json:"company_name,omitempty"`
	FirstName   string   `json:"first_name,omitempty"`
	LastName    string   `json:"last_name,omitempty"`
	Street      string   `json:"street,omitempty"`
	Zip         string   `json:"zip,omitempty"`
	City        string   `json:"city,omitempty"`
	RawLines    []string `json:"raw_lines,omitempty"`
}

var (
	streetRegex     = regexp.MustCompile(`(?i)^[\p{L}\d\s\.-]+\d+[A-Za-z]?$`)
	zipCityRegex    = regexp.MustCompile(`^(\d{4,5})\s+(.+)$`)
	honorificsRegex = regexp.MustCompile(`(?i)\b(herrn|herr|frau|mr|mrs|ms)\b\.?`)
	addressKeywords = []string{"angebotsnr", "kundennr", "rechnung", "invoice", "offer", "angebot"}
	vendorNoise     = []string{"tel", "telefon", "fax", "email", "info@", "iban", "bic", "sparkasse", "www.", "tsunami events"}
)

func (h *PDFHandler) buildCustomerPrefill(extraction *models.PDFExtraction) *customerPrefill {
	if extraction == nil || !extraction.RawText.Valid {
		return nil
	}
	lines := strings.Split(extraction.RawText.String, "\n")
	block := captureRecipientBlock(lines)
	if len(block) == 0 {
		return nil
	}
	cleaned := filterRecipientLines(block)
	prefill := &customerPrefill{RawLines: cleaned}

	nameIdx := -1
	for idx, line := range cleaned {
		if containsHonorific(strings.ToLower(line)) {
			nameIdx = idx
			clean := strings.TrimSpace(removeHonorifics(line))
			parts := strings.Fields(clean)
			if len(parts) >= 2 {
				prefill.FirstName = strings.Title(parts[0])
				prefill.LastName = strings.Title(strings.Join(parts[1:], " "))
			} else if len(parts) == 1 {
				prefill.LastName = strings.Title(parts[0])
			}
			break
		}
	}

	if nameIdx > 0 {
		prefill.CompanyName = strings.Join(cleaned[:nameIdx], " ")
	}

	for _, line := range cleaned {
		if prefill.Street == "" && streetRegex.MatchString(line) {
			prefill.Street = line
			continue
		}
		if prefill.Zip == "" {
			if matches := zipCityRegex.FindStringSubmatch(line); len(matches) == 3 {
				prefill.Zip = matches[1]
				prefill.City = strings.Title(matches[2])
			}
		}
		if nameIdx == -1 && !containsHonorific(strings.ToLower(line)) && prefill.LastName == "" {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				prefill.FirstName = strings.Title(parts[0])
				prefill.LastName = strings.Title(strings.Join(parts[1:], " "))
				nameIdx = 0
			}
		}
	}

	if prefill.CompanyName == "" && extraction.CustomerName.Valid {
		prefill.CompanyName = extraction.CustomerName.String
	}

	return prefill
}

func captureRecipientBlock(lines []string) []string {
	if block := captureBlockBeforeKeywords(lines); len(block) > 0 {
		return block
	}
	return captureBlockByHonorific(lines)
}

func captureBlockBeforeKeywords(lines []string) []string {
	for i, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if containsAny(lower, addressKeywords) {
			block := []string{}
			collected := 0
			for j := i - 1; j >= 0 && collected < 10; j-- {
				line := strings.TrimSpace(lines[j])
				if line == "" {
					if len(block) > 0 {
						break
					}
					continue
				}
				if isVendorNoise(line) {
					continue
				}
				block = append([]string{line}, block...)
				collected++
			}
			if len(block) > 0 {
				return block
			}
		}
	}
	return nil
}

func captureBlockByHonorific(lines []string) []string {
	best := []string{}
	for i, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if containsHonorific(lower) {
			start := i
			for start > 0 {
				prev := strings.TrimSpace(lines[start-1])
				if prev == "" {
					break
				}
				start--
			}
			block := make([]string, 0, 10)
			for j := start; j < len(lines); j++ {
				t := strings.TrimSpace(lines[j])
				if t == "" {
					if len(block) > 0 {
						break
					}
					continue
				}
				block = append(block, t)
				if len(block) >= 10 {
					break
				}
			}
			best = block
			break
		}
	}
	if len(best) == 0 {
		for _, raw := range lines {
			trimmed := strings.TrimSpace(raw)
			if trimmed != "" && !isVendorNoise(trimmed) {
				best = append(best, trimmed)
			}
			if len(best) >= 6 {
				break
			}
		}
	}
	return best
}

func containsAny(value string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(value, kw) {
			return true
		}
	}
	return false
}

func containsHonorific(value string) bool {
	return strings.Contains(value, "herr") || strings.Contains(value, "frau") || strings.Contains(value, "mr") || strings.Contains(value, "mrs") || strings.Contains(value, "ms")
}

func isVendorNoise(line string) bool {
	lower := strings.ToLower(line)
	for _, kw := range vendorNoise {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func removeHonorifics(value string) string {
	return strings.TrimSpace(honorificsRegex.ReplaceAllString(value, ""))
}

func filterRecipientLines(lines []string) []string {
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || isVendorNoise(trimmed) {
			continue
		}
		result = append(result, trimmed)
	}
	if len(result) == 0 {
		return lines
	}
	return result
}

func optionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func (h *PDFHandler) computePriceOverrides(aggregates map[uint]*productPricingAggregate) (map[uint]float64, error) {
	if len(aggregates) == 0 {
		return nil, nil
	}

	productIDs := make([]uint, 0, len(aggregates))
	for pid, agg := range aggregates {
		if agg == nil || agg.pricedQuantity == 0 || agg.totalAmount <= 0 {
			continue
		}
		productIDs = append(productIDs, pid)
	}

	if len(productIDs) == 0 {
		return nil, nil
	}

	var products []models.Product
	if err := h.DB.Where("productID IN ?", productIDs).Find(&products).Error; err != nil {
		return nil, err
	}

	productMap := make(map[uint]*models.Product, len(products))
	for i := range products {
		product := &products[i]
		productMap[product.ProductID] = product
	}

	overrides := make(map[uint]float64)
	for pid, agg := range aggregates {
		if agg == nil || agg.pricedQuantity == 0 || agg.totalAmount <= 0 {
			continue
		}
		unitPrice := agg.totalAmount / float64(agg.pricedQuantity)
		if unitPrice <= 0 {
			continue
		}

		defaultPrice := 0.0
		if product := productMap[pid]; product != nil && product.ItemCostPerDay != nil {
			defaultPrice = *product.ItemCostPerDay
		}

		if defaultPrice > 0 && math.Abs(defaultPrice-unitPrice) < 0.01 {
			continue
		}

		overrides[pid] = unitPrice
	}

	if len(overrides) == 0 {
		return nil, nil
	}

	return overrides, nil
}

func (h *PDFHandler) applyCustomPriceOverrides(job *models.Job, overrides map[uint]float64) error {
	if len(overrides) == 0 {
		return nil
	}

	jobDevices, err := h.JobHandler.jobRepo.GetJobDevices(job.JobID)
	if err != nil {
		return err
	}

	for _, jd := range jobDevices {
		if jd.Device.DeviceID == "" {
			continue
		}
		var productID uint
		if jd.Device.ProductID != nil {
			productID = *jd.Device.ProductID
		} else if jd.Device.Product != nil {
			productID = jd.Device.Product.ProductID
		}
		if productID == 0 {
			continue
		}
		price, ok := overrides[productID]
		if !ok || price <= 0 {
			continue
		}
		if err := h.JobHandler.jobRepo.UpdateDevicePrice(job.JobID, jd.DeviceID, price); err != nil {
			return err
		}
	}

	return nil
}

func formatDateInput(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}
func (h *PDFHandler) persistExtractionMappings(c *gin.Context, extractionID uint64) error {
	var mappedItems []models.PDFExtractionItem
	if err := h.DB.Where("extraction_id = ? AND mapped_product_id IS NOT NULL", extractionID).
		Find(&mappedItems).Error; err != nil {
		return err
	}

	if len(mappedItems) == 0 {
		return nil
	}

	userID := int64(0)
	if uid, exists := c.Get("userID"); exists {
		if id, ok := uid.(int64); ok {
			userID = id
		}
	}

	for _, item := range mappedItems {
		if !item.MappedProductID.Valid {
			continue
		}
		text := strings.TrimSpace(item.RawProductText)
		if text == "" {
			continue
		}

		if err := h.Mapper.SaveMapping(text, int(item.MappedProductID.Int64), userID); err != nil {
			log.Printf("warning: failed to save mapping for extraction item %d: %v", item.ItemID, err)
			continue
		}

		h.recordMappingEvent(item.ExtractionID, item.ItemID, int(item.MappedProductID.Int64), text, userID)
	}

	return nil
}

func (h *PDFHandler) recordMappingEvent(extractionID uint64, itemID uint64, productID int, rawText string, userID int64) {
	event := models.PDFMappingEvent{
		PDFProductText: rawText,
		ProductID:      productID,
	}

	if extractionID > 0 {
		event.ExtractionID = sql.NullInt64{Int64: int64(extractionID), Valid: true}
	}
	if itemID > 0 {
		event.ItemID = sql.NullInt64{Int64: int64(itemID), Valid: true}
	}

	normalized := strings.TrimSpace(strings.ToLower(rawText))
	if normalized != "" {
		event.NormalizedText = sql.NullString{String: normalized, Valid: true}
	}
	if userID > 0 {
		event.CreatedBy = sql.NullInt64{Int64: userID, Valid: true}
	}

	if err := h.DB.Create(&event).Error; err != nil {
		log.Printf("warning: failed to record mapping event: %v", err)
	}
}
