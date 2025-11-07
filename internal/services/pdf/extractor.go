package pdf

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go-barcode-webapp/internal/models"

	"github.com/ledongthuc/pdf"
)

// PDFExtractor handles PDF text extraction and data parsing
type PDFExtractor struct {
	UploadDir string
	OCREngine *OCREngine
	Parser    *IntelligentParser
}

// NewPDFExtractor creates a new PDF extractor instance
func NewPDFExtractor(uploadDir string) *PDFExtractor {
	// Create temp directory for OCR processing
	tempDir := filepath.Join(uploadDir, "temp_ocr")
	os.MkdirAll(tempDir, 0755)

	return &PDFExtractor{
		UploadDir: uploadDir,
		OCREngine: NewOCREngine(tempDir),
		Parser:    NewIntelligentParser(),
	}
}

// SaveUploadedFile saves the uploaded PDF file to disk
func (e *PDFExtractor) SaveUploadedFile(file *multipart.FileHeader) (*models.PDFUpload, error) {
	// Create upload directory if it doesn't exist
	pdfDir := filepath.Join(e.UploadDir, "pdfs")
	if err := os.MkdirAll(pdfDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create upload directory: %v", err)
	}

	// Generate unique filename
	timestamp := time.Now().Format("20060102_150405")
	ext := filepath.Ext(file.Filename)
	storedFilename := fmt.Sprintf("%s_%s%s", timestamp, generateRandomString(8), ext)
	filePath := filepath.Join(pdfDir, storedFilename)

	// Open uploaded file
	src, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open uploaded file: %v", err)
	}
	defer src.Close()

	// Create destination file
	dst, err := os.Create(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create destination file: %v", err)
	}
	defer dst.Close()

	// Calculate file hash while copying
	hash := sha256.New()
	tee := io.TeeReader(src, hash)

	// Copy file
	size, err := io.Copy(dst, tee)
	if err != nil {
		return nil, fmt.Errorf("failed to save file: %v", err)
	}

	// Create upload record
	upload := &models.PDFUpload{
		OriginalFilename: file.Filename,
		StoredFilename:   storedFilename,
		FilePath:         filePath,
		FileSize:         size,
		MimeType:         file.Header.Get("Content-Type"),
		FileHash:         sql.NullString{String: hex.EncodeToString(hash.Sum(nil)), Valid: true},
		UploadedAt:       time.Now(),
		ProcessingStatus: "pending",
		IsActive:         true,
	}

	return upload, nil
}

// ExtractText extracts text from a PDF file using ledongthuc/pdf library
func (e *PDFExtractor) ExtractText(filePath string) (string, error) {
	file, reader, err := pdf.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open PDF: %v", err)
	}
	defer file.Close()

	var textBuilder strings.Builder
	numPages := reader.NumPage()

	for pageNum := 1; pageNum <= numPages; pageNum++ {
		page := reader.Page(pageNum)
		if page.V.IsNull() {
			continue
		}

		text, err := page.GetPlainText(nil)
		if err != nil {
			// Log error but continue with next page
			fmt.Printf("Warning: failed to extract text from page %d: %v\n", pageNum, err)
			continue
		}

		textBuilder.WriteString(text)
		textBuilder.WriteString("\n")
	}

	extractedText := textBuilder.String()
	if len(extractedText) == 0 {
		return "", fmt.Errorf("no text could be extracted from PDF")
	}

	return extractedText, nil
}

// ExtractWithOCR extracts text from PDF using OCR when needed
func (e *PDFExtractor) ExtractWithOCR(filePath string) (*OCRResult, error) {
	log.Printf("Starting OCR extraction for: %s", filePath)

	// Use OCR engine to extract text
	ocrResult, err := e.OCREngine.ExtractTextWithOCR(filePath)
	if err != nil {
		log.Printf("OCR extraction failed: %v", err)
		// Fallback to simple text extraction
		text, fallbackErr := e.ExtractText(filePath)
		if fallbackErr != nil {
			return nil, fmt.Errorf("both OCR and text extraction failed: %v, %v", err, fallbackErr)
		}

		return &OCRResult{
			Text:       text,
			Confidence: 85.0,
			PageCount:  1,
			Method:     "text_based",
		}, nil
	}

	log.Printf("OCR extraction successful: method=%s, confidence=%.2f, pages=%d",
		ocrResult.Method, ocrResult.Confidence, ocrResult.PageCount)

	return ocrResult, nil
}

// ParseDocumentIntelligently parses extracted text using intelligent parser
func (e *PDFExtractor) ParseDocumentIntelligently(rawText string) (*ParsedDocument, error) {
	log.Printf("Parsing document with intelligent parser (text length: %d)", len(rawText))

	doc, err := e.Parser.ParseDocument(rawText)
	if err != nil {
		return nil, fmt.Errorf("parsing failed: %v", err)
	}

	log.Printf("Document parsed successfully: type=%s, items=%d, confidence=%.2f",
		doc.DocumentType, len(doc.Items), doc.ConfidenceScore)

	return doc, nil
}

// ParseInvoiceData parses invoice data from extracted text
func (e *PDFExtractor) ParseInvoiceData(text string) (*ParsedInvoiceData, error) {
	data := &ParsedInvoiceData{
		Items: []ParsedLineItem{},
	}

	lines := strings.Split(text, "\n")

	// Blacklist for irrelevant content (addresses, cities, etc.)
	irrelevantKeywords := []string{
		"straße", "strasse", "str.", "plz", "postleitzahl",
		"telefon", "tel.", "fax", "email", "e-mail", "web", "www",
		"ust-id", "steuernummer", "amtsgericht", "geschäftsführer",
		"bankverbindung", "iban", "bic", "swift",
		// Common German cities that might appear
		"haiger", "dillenburg", "herborn", "wetzlar", "siegen",
		"gießen", "marburg", "köln", "frankfurt", "münchen",
	}

	// Regular expressions for common invoice patterns
	customerRegex := regexp.MustCompile(`(?i)(?:kunde|customer|rechnung an|bill to|empfänger)[\s:]+(.+)`)
	dateRegex := regexp.MustCompile(`(\d{1,2})[\./-](\d{1,2})[\./-](\d{2,4})`)
	dateRangeRegex := regexp.MustCompile(`(?i)(?:zeitraum|period|vom|from)[\s:]*(\d{1,2})[\./-](\d{1,2})[\./-](\d{2,4})[\s]*(?:bis|to|-|–)[\s]*(\d{1,2})[\./-](\d{1,2})[\./-](\d{2,4})`)
	invoiceNumberRegex := regexp.MustCompile(`(?i)(?:rechnung|invoice|angebot|offer|auftrag|order)[\s#:Nr.]+([A-Z0-9\-]+)`)
	totalRegex := regexp.MustCompile(`(?i)(?:gesamt|total|summe|sum)[\s:]*€?\s*([0-9,]+\.?\d*)`)
	discountRegex := regexp.MustCompile(`(?i)(?:rabatt|discount|nachlass)[\s:]*€?\s*([0-9,]+\.?\d*)`)

	// Parse line items with multiple patterns for flexibility
	// Pattern 1: Full format with prices: "1  2x  LED PAR 64  €50.00  €100.00"
	itemRegexFull := regexp.MustCompile(`^(\d+)\s+(\d+)x?\s+(.+?)\s+€?\s*([0-9.,]+)\s+€?\s*([0-9.,]+)\s*$`)
	// Pattern 2: Without position: "2x  LED PAR 64  €50.00  €100.00"
	itemRegexNoPosPrice := regexp.MustCompile(`^(\d+)x?\s+(.+?)\s+€?\s*([0-9.,]+)\s+€?\s*([0-9.,]+)\s*$`)
	// Pattern 3: Only quantity and description: "2x LED PAR 64"
	itemRegexSimple := regexp.MustCompile(`^(\d+)x?\s+(.+?)\s*$`)
	// Pattern 4: Position and description (common in tables): "0045  LD Systems Stinger Sub 18A G3"
	itemRegexPosDesc := regexp.MustCompile(`^(\d+)\s{2,}(.+?)\s*$`)

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || len(line) < 3 {
			continue
		}

		// Skip lines with irrelevant keywords
		lineLower := strings.ToLower(line)
		isIrrelevant := false
		for _, keyword := range irrelevantKeywords {
			if strings.Contains(lineLower, keyword) {
				isIrrelevant = true
				break
			}
		}

		// Skip postal codes (5 digits), phone numbers, IBANs
		if regexp.MustCompile(`^\d{5}$`).MatchString(line) || // PLZ
			regexp.MustCompile(`^[\d\s\-\+\(\)]{8,}$`).MatchString(line) || // Phone
			regexp.MustCompile(`^[A-Z]{2}\d{2}`).MatchString(line) { // IBAN
			isIrrelevant = true
		}

		// Extract customer name
		if matches := customerRegex.FindStringSubmatch(line); len(matches) > 1 {
			data.CustomerName = strings.TrimSpace(matches[1])
		}

		// Extract date range (job period: start - end date)
		if matches := dateRangeRegex.FindStringSubmatch(line); len(matches) > 6 {
			startDay, _ := strconv.Atoi(matches[1])
			startMonth, _ := strconv.Atoi(matches[2])
			startYear, _ := strconv.Atoi(matches[3])
			if startYear < 100 {
				startYear += 2000
			}
			data.StartDate = time.Date(startYear, time.Month(startMonth), startDay, 0, 0, 0, 0, time.UTC)

			endDay, _ := strconv.Atoi(matches[4])
			endMonth, _ := strconv.Atoi(matches[5])
			endYear, _ := strconv.Atoi(matches[6])
			if endYear < 100 {
				endYear += 2000
			}
			data.EndDate = time.Date(endYear, time.Month(endMonth), endDay, 0, 0, 0, 0, time.UTC)
		}

		// Extract document date (fallback if no range found)
		if data.DocumentDate.IsZero() {
			if matches := dateRegex.FindStringSubmatch(line); len(matches) > 3 {
				day, _ := strconv.Atoi(matches[1])
				month, _ := strconv.Atoi(matches[2])
				year, _ := strconv.Atoi(matches[3])
				if year < 100 {
					year += 2000 // Assume 2000s for 2-digit years
				}
				data.DocumentDate = time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
			}
		}

		// Extract invoice/offer number
		if matches := invoiceNumberRegex.FindStringSubmatch(line); len(matches) > 1 {
			data.DocumentNumber = strings.TrimSpace(matches[1])
		}

		// Extract total amount
		if matches := totalRegex.FindStringSubmatch(line); len(matches) > 1 {
			totalStr := strings.ReplaceAll(matches[1], ",", ".")
			if total, err := strconv.ParseFloat(totalStr, 64); err == nil {
				data.TotalAmount = total
			}
		}

		// Extract discount
		if matches := discountRegex.FindStringSubmatch(line); len(matches) > 1 {
			discountStr := strings.ReplaceAll(matches[1], ",", ".")
			if discount, err := strconv.ParseFloat(discountStr, 64); err == nil {
				data.DiscountAmount = discount
			}
		}

		// Skip irrelevant lines for item extraction
		if isIrrelevant {
			continue
		}

		// Try to extract line items with multiple patterns
		var item *ParsedLineItem

		// Pattern 1: Full format with position, quantity, description, prices
		if matches := itemRegexFull.FindStringSubmatch(line); len(matches) > 5 {
			lineNumber, _ := strconv.Atoi(matches[1])
			quantity, _ := strconv.Atoi(matches[2])
			description := strings.TrimSpace(matches[3])
			unitPriceStr := strings.ReplaceAll(strings.ReplaceAll(matches[4], ",", "."), " ", "")
			unitPrice, _ := strconv.ParseFloat(unitPriceStr, 64)
			lineTotalStr := strings.ReplaceAll(strings.ReplaceAll(matches[5], ",", "."), " ", "")
			lineTotal, _ := strconv.ParseFloat(lineTotalStr, 64)

			item = &ParsedLineItem{
				LineNumber:   lineNumber,
				Quantity:     quantity,
				ProductText:  description,
				UnitPrice:    unitPrice,
				LineTotal:    lineTotal,
				OriginalLine: i + 1,
			}
		} else if matches := itemRegexNoPosPrice.FindStringSubmatch(line); len(matches) > 4 {
			// Pattern 2: No position, but has quantity, description, prices
			quantity, _ := strconv.Atoi(matches[1])
			description := strings.TrimSpace(matches[2])
			unitPriceStr := strings.ReplaceAll(strings.ReplaceAll(matches[3], ",", "."), " ", "")
			unitPrice, _ := strconv.ParseFloat(unitPriceStr, 64)
			lineTotalStr := strings.ReplaceAll(strings.ReplaceAll(matches[4], ",", "."), " ", "")
			lineTotal, _ := strconv.ParseFloat(lineTotalStr, 64)

			item = &ParsedLineItem{
				Quantity:     quantity,
				ProductText:  description,
				UnitPrice:    unitPrice,
				LineTotal:    lineTotal,
				OriginalLine: i + 1,
			}
		} else if matches := itemRegexPosDesc.FindStringSubmatch(line); len(matches) > 2 {
			// Pattern 4: Position and description only (e.g., "0045  LD Systems Stinger Sub 18A G3")
			lineNumber, _ := strconv.Atoi(matches[1])
			description := strings.TrimSpace(matches[2])

			// Validate description is a meaningful product name
			if e.isValidProductDescription(description) {
				item = &ParsedLineItem{
					LineNumber:   lineNumber,
					Quantity:     1, // Default to 1 if not specified
					ProductText:  description,
					OriginalLine: i + 1,
				}
			}
		} else if matches := itemRegexSimple.FindStringSubmatch(line); len(matches) > 2 {
			// Pattern 3: Simple format (quantity and description only)
			quantity, _ := strconv.Atoi(matches[1])
			description := strings.TrimSpace(matches[2])

			// Validate description is a meaningful product name
			if e.isValidProductDescription(description) {
				item = &ParsedLineItem{
					Quantity:     quantity,
					ProductText:  description,
					OriginalLine: i + 1,
				}
			}
		}

		if item != nil {
			data.Items = append(data.Items, *item)
		}
	}

	// Calculate confidence score based on extracted data
	confidence := e.calculateConfidence(data)
	data.ConfidenceScore = confidence

	return data, nil
}

// calculateConfidence calculates extraction confidence based on found data
func (e *PDFExtractor) calculateConfidence(data *ParsedInvoiceData) float64 {
	score := 0.0
	maxScore := 7.0

	if data.CustomerName != "" {
		score += 1.0
	}
	if data.DocumentNumber != "" {
		score += 1.5
	}
	if !data.DocumentDate.IsZero() {
		score += 1.0
	}
	if data.TotalAmount > 0 {
		score += 1.5
	}
	if len(data.Items) > 0 {
		score += 2.0
	}

	return (score / maxScore) * 100.0
}

// isValidProductDescription checks if a string is a valid product name
func (e *PDFExtractor) isValidProductDescription(description string) bool {
	// Minimum length for a product name
	if len(description) < 8 {
		return false
	}

	// Must not be just numbers
	if regexp.MustCompile(`^\d+$`).MatchString(description) {
		return false
	}

	// Must not be a city name only (single word that's a city)
	cityNames := []string{
		"haiger", "dillenburg", "herborn", "wetzlar", "siegen",
		"gießen", "marburg", "köln", "frankfurt", "münchen",
		"berlin", "hamburg", "dortmund", "essen", "düsseldorf",
	}
	descLower := strings.ToLower(strings.TrimSpace(description))
	for _, city := range cityNames {
		if descLower == city {
			return false
		}
	}

	// Must contain at least one letter
	if !regexp.MustCompile(`[a-zA-Z]`).MatchString(description) {
		return false
	}

	// Should not be typical address patterns
	addressPatterns := []string{
		`^\d{5}\s+\w+$`,                    // "35708 Haiger"
		`(?i)^(straße|str\.|strasse)\s+`,   // "Straße 123"
		`(?i)postfach`,                      // Postbox
	}
	for _, pattern := range addressPatterns {
		if regexp.MustCompile(pattern).MatchString(description) {
			return false
		}
	}

	return true
}

// ParsedInvoiceData represents structured data extracted from invoice
type ParsedInvoiceData struct {
	CustomerName    string
	CustomerID      *int
	DocumentNumber  string
	DocumentDate    time.Time
	StartDate       time.Time // Job start date
	EndDate         time.Time // Job end date
	TotalAmount     float64
	DiscountAmount  float64
	Items           []ParsedLineItem
	ConfidenceScore float64
	RawText         string
}

// ParsedLineItem represents a line item from the invoice
type ParsedLineItem struct {
	LineNumber   int
	Quantity     int
	ProductText  string
	UnitPrice    float64
	LineTotal    float64
	OriginalLine int // Line number in original text
}

// ToJSON converts parsed data to JSON string
func (d *ParsedInvoiceData) ToJSON() (string, error) {
	data, err := json.Marshal(d)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// generateRandomString generates a random string for filename
func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, length)
	for i := range result {
		result[i] = charset[time.Now().UnixNano()%int64(len(charset))]
		time.Sleep(time.Nanosecond) // Ensure different random values
	}
	return string(result)
}
