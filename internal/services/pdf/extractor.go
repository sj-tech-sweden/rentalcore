package pdf

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
}

// NewPDFExtractor creates a new PDF extractor instance
func NewPDFExtractor(uploadDir string) *PDFExtractor {
	return &PDFExtractor{
		UploadDir: uploadDir,
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

// ParseInvoiceData parses invoice data from extracted text
func (e *PDFExtractor) ParseInvoiceData(text string) (*ParsedInvoiceData, error) {
	data := &ParsedInvoiceData{
		Items: []ParsedLineItem{},
	}

	lines := strings.Split(text, "\n")

	// Regular expressions for common invoice patterns
	customerRegex := regexp.MustCompile(`(?i)(?:kunde|customer|rechnung an|bill to|empfänger)[\s:]+(.+)`)
	dateRegex := regexp.MustCompile(`(\d{1,2})[\./-](\d{1,2})[\./-](\d{2,4})`)
	invoiceNumberRegex := regexp.MustCompile(`(?i)(?:rechnung|invoice|angebot|offer)[\s#:]+([A-Z0-9\-]+)`)
	totalRegex := regexp.MustCompile(`(?i)(?:gesamt|total|summe|sum)[\s:]*€?\s*([0-9,]+\.?\d*)`)
	discountRegex := regexp.MustCompile(`(?i)(?:rabatt|discount|nachlass)[\s:]*€?\s*([0-9,]+\.?\d*)`)

	// Parse line items (position, quantity, description, price)
	// Format examples:
	// 1  2x  LED PAR 64  €50.00  €100.00
	// Pos. | Menge | Beschreibung | Einzelpreis | Gesamt
	itemRegex := regexp.MustCompile(`(\d+)\s+(\d+)x?\s+(.+?)\s+€?\s*([0-9,]+\.?\d*)\s+€?\s*([0-9,]+\.?\d*)`)

	for i, line := range lines {
		line = strings.TrimSpace(line)

		// Extract customer name
		if matches := customerRegex.FindStringSubmatch(line); len(matches) > 1 {
			data.CustomerName = strings.TrimSpace(matches[1])
		}

		// Extract document date
		if matches := dateRegex.FindStringSubmatch(line); len(matches) > 3 {
			day, _ := strconv.Atoi(matches[1])
			month, _ := strconv.Atoi(matches[2])
			year, _ := strconv.Atoi(matches[3])
			if year < 100 {
				year += 2000 // Assume 2000s for 2-digit years
			}
			data.DocumentDate = time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
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

		// Extract line items
		if matches := itemRegex.FindStringSubmatch(line); len(matches) > 5 {
			lineNumber, _ := strconv.Atoi(matches[1])
			quantity, _ := strconv.Atoi(matches[2])
			description := strings.TrimSpace(matches[3])
			unitPriceStr := strings.ReplaceAll(matches[4], ",", ".")
			unitPrice, _ := strconv.ParseFloat(unitPriceStr, 64)
			lineTotalStr := strings.ReplaceAll(matches[5], ",", ".")
			lineTotal, _ := strconv.ParseFloat(lineTotalStr, 64)

			item := ParsedLineItem{
				LineNumber:     lineNumber,
				Quantity:       quantity,
				ProductText:    description,
				UnitPrice:      unitPrice,
				LineTotal:      lineTotal,
				OriginalLine:   i + 1,
			}
			data.Items = append(data.Items, item)
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

// ParsedInvoiceData represents structured data extracted from invoice
type ParsedInvoiceData struct {
	CustomerName    string
	CustomerID      *int
	DocumentNumber  string
	DocumentDate    time.Time
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
