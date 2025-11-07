package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
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

	data := gin.H{
		"extraction": extraction,
		"upload":     upload,
		"items":      itemsWithSuggestions,
		"pageTitle":  "PDF Product Mapping",
		"startDate":  formatDateInput(startDate),
		"endDate":    formatDateInput(endDate),
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

	h.Mapper.SaveMapping(item.RawProductText, req.ProductID, userID)

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
		StartDate  string `json:"start_date"`
		EndDate    string `json:"end_date"`
		CustomerID *int   `json:"customer_id"`
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

	// If job already linked, return it
	if upload.JobID.Valid {
		var job models.Job
		if err := h.DB.First(&job, upload.JobID.Int64).Error; err == nil {
			c.JSON(http.StatusOK, gin.H{
				"success":  true,
				"job_id":   job.JobID,
				"jobs_url": fmt.Sprintf("/jobs?editJob=%d", job.JobID),
			})
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

	var meta map[string]string
	if extraction.Metadata.Valid {
		_ = json.Unmarshal([]byte(extraction.Metadata.String), &meta)
	}
	if meta == nil {
		meta = map[string]string{}
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

	discount := 0.0
	if extraction.DiscountAmount.Valid {
		discount = extraction.DiscountAmount.Float64
	}

	desc := fmt.Sprintf("Generated from %s (Extraction %d)", upload.OriginalFilename, extraction.ExtractionID)
	truncatedDesc := truncateString(desc, 48)

	job := models.Job{
		CustomerID: customerID,
		StatusID:   statusID,
		Discount:   discount,
		Revenue:    revenue,
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

	if extraction.CustomerName.Valid {
		name := strings.TrimSpace(extraction.CustomerName.String)
		if name != "" {
			var customer models.Customer
			if err := h.DB.Where("companyname = ?", name).First(&customer).Error; err == nil {
				return customer.CustomerID, nil
			}

			customer = models.Customer{}
			customer.CompanyName = &name
			if err := h.DB.Create(&customer).Error; err == nil {
				return customer.CustomerID, nil
			}
		}
	}

	var fallback models.Customer
	if err := h.DB.Order("customerID").First(&fallback).Error; err == nil {
		return fallback.CustomerID, nil
	}

	return 0, fmt.Errorf("no customers available to assign job")
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

func (h *PDFHandler) assignProductsToJob(job *models.Job, extractionID uint64) (string, error) {
	if h.JobHandler == nil {
		return "", nil
	}

	var items []models.PDFExtractionItem
	if err := h.DB.Where("extraction_id = ?", extractionID).Find(&items).Error; err != nil {
		return "", err
	}

	productCounts := make(map[uint]int)
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
	}

	if len(productCounts) == 0 {
		return "", nil
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
		if strings.Contains(strings.ToLower(err.Error()), "not enough available devices") {
			return err.Error(), nil
		}
		return "", err
	}

	return "", nil
}

func formatDateInput(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}
