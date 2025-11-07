package pdf

import (
	"database/sql"
	"strings"
	"time"

	"go-barcode-webapp/internal/models"
	"gorm.io/gorm"
)

// ProductMapper handles product mapping between PDF text and database products
type ProductMapper struct {
	DB *gorm.DB
}

// NewProductMapper creates a new product mapper instance
func NewProductMapper(db *gorm.DB) *ProductMapper {
	return &ProductMapper{DB: db}
}

// FindBestMatch finds the best matching product for given text
func (m *ProductMapper) FindBestMatch(productText string) (*models.PDFProductMapping, *models.Product, float64, error) {
	// 1. Check for exact mapping in saved mappings
	var existingMapping models.PDFProductMapping
	err := m.DB.Where("pdf_product_text = ? AND is_active = ?", productText, true).
		First(&existingMapping).Error

	if err == nil {
		return m.markMappingUsage(&existingMapping)
	}

	// 1b. Check normalized mappings if no exact match
	normalized := normalizeProductText(productText)
	if normalized != "" {
		if err := m.DB.Where("normalized_text = ? AND is_active = ?", normalized, true).
			First(&existingMapping).Error; err == nil {
			return m.markMappingUsage(&existingMapping)
		}
	}

	// 2. Try fuzzy matching with all products

	var products []models.Product
	if err := m.DB.Find(&products).Error; err != nil {
		return nil, nil, 0, err
	}

	var bestMatch *models.Product
	var bestScore float64 = 0

	for i := range products {
		score := calculateSimilarity(normalized, normalizeProductText(products[i].Name))
		if score > bestScore {
			bestScore = score
			bestMatch = &products[i]
		}
	}

	// Return best match if confidence is above threshold
	if bestScore >= 70.0 && bestMatch != nil {
		return nil, bestMatch, bestScore, nil
	}

	return nil, nil, 0, nil
}

// SaveMapping saves a manual product mapping
func (m *ProductMapper) SaveMapping(pdfText string, productID int, userID int64) error {
	normalized := normalizeProductText(pdfText)

	confidence := 100.0
	lastUsed := time.Now()
	normalizedVal := nullStringPtr(sql.NullString{String: normalized, Valid: normalized != ""})
	createdBy := nullIntPtr(sql.NullInt64{Int64: userID, Valid: userID > 0})

	query := `
		INSERT INTO pdf_product_mappings
			(pdf_product_text, normalized_text, product_id, mapping_type, confidence_score, usage_count, last_used_at, created_by, is_active)
		VALUES
			(?, ?, ?, 'manual', ?, 1, ?, ?, 1)
		ON DUPLICATE KEY UPDATE
			normalized_text = VALUES(normalized_text),
			product_id = VALUES(product_id),
			mapping_type = 'manual',
			confidence_score = VALUES(confidence_score),
			usage_count = usage_count + 1,
			last_used_at = VALUES(last_used_at),
			is_active = 1
	`

	return m.DB.Exec(query,
		pdfText,
		normalizedVal,
		productID,
		confidence,
		lastUsed,
		createdBy,
	).Error
}

// GetAllMappings retrieves all active mappings
func (m *ProductMapper) GetAllMappings() ([]models.PDFProductMapping, error) {
	var mappings []models.PDFProductMapping
	err := m.DB.Where("is_active = ?", true).
		Order("usage_count DESC, updated_at DESC").
		Find(&mappings).Error
	return mappings, err
}

// DeleteMapping deletes or deactivates a mapping
func (m *ProductMapper) DeleteMapping(mappingID uint64) error {
	return m.DB.Model(&models.PDFProductMapping{}).
		Where("mapping_id = ?", mappingID).
		Update("is_active", false).Error
}

func (m *ProductMapper) markMappingUsage(mapping *models.PDFProductMapping) (*models.PDFProductMapping, *models.Product, float64, error) {
	m.DB.Model(mapping).Updates(map[string]interface{}{
		"usage_count":  gorm.Expr("usage_count + 1"),
		"last_used_at": time.Now(),
	})

	var product models.Product
	if err := m.DB.First(&product, mapping.ProductID).Error; err != nil {
		return nil, nil, 0, err
	}

	return mapping, &product, 100.0, nil
}

func nullStringPtr(value sql.NullString) interface{} {
	if value.Valid {
		return value.String
	}
	return nil
}

func nullIntPtr(value sql.NullInt64) interface{} {
	if value.Valid {
		return value.Int64
	}
	return nil
}

// normalizeProductText normalizes text for comparison
func normalizeProductText(text string) string {
	// Convert to lowercase
	text = strings.ToLower(text)

	// Replace non-alphanumeric characters with spaces to keep word boundaries
	text = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return ' '
	}, text)

	// Normalize whitespace
	text = strings.Join(strings.Fields(text), " ")

	return strings.TrimSpace(text)
}

// calculateSimilarity calculates similarity between two strings (0-100)
// Uses a simple approach: Levenshtein distance ratio
func calculateSimilarity(s1, s2 string) float64 {
	// Quick checks
	if s1 == s2 {
		return 100.0
	}
	if len(s1) == 0 || len(s2) == 0 {
		return 0.0
	}

	// Check if one contains the other
	if strings.Contains(s1, s2) || strings.Contains(s2, s1) {
		shorter := len(s1)
		if len(s2) < shorter {
			shorter = len(s2)
		}
		longer := len(s1)
		if len(s2) > longer {
			longer = len(s2)
		}
		return float64(shorter) / float64(longer) * 100.0
	}

	// Calculate Levenshtein distance
	distance := levenshteinDistance(s1, s2)
	maxLen := len(s1)
	if len(s2) > maxLen {
		maxLen = len(s2)
	}

	// Convert to similarity percentage
	similarity := (1.0 - float64(distance)/float64(maxLen)) * 100.0
	if similarity < 0 {
		similarity = 0
	}

	return similarity
}

// levenshteinDistance calculates the Levenshtein distance between two strings
func levenshteinDistance(s1, s2 string) int {
	if len(s1) == 0 {
		return len(s2)
	}
	if len(s2) == 0 {
		return len(s1)
	}

	// Create matrix
	matrix := make([][]int, len(s1)+1)
	for i := range matrix {
		matrix[i] = make([]int, len(s2)+1)
		matrix[i][0] = i
	}
	for j := range matrix[0] {
		matrix[0][j] = j
	}

	// Fill matrix
	for i := 1; i <= len(s1); i++ {
		for j := 1; j <= len(s2); j++ {
			cost := 0
			if s1[i-1] != s2[j-1] {
				cost = 1
			}

			matrix[i][j] = min3(
				matrix[i-1][j]+1,      // deletion
				matrix[i][j-1]+1,      // insertion
				matrix[i-1][j-1]+cost, // substitution
			)
		}
	}

	return matrix[len(s1)][len(s2)]
}

// min3 returns the minimum of three integers
func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// FindSimilarProducts finds products similar to the given text
func (m *ProductMapper) FindSimilarProducts(productText string, limit int) ([]models.ProductMappingSuggestion, error) {
	normalized := normalizeProductText(productText)

	var products []models.Product
	if err := m.DB.Limit(500).Find(&products).Error; err != nil {
		return nil, err
	}

	var suggestions []models.ProductMappingSuggestion

	for i := range products {
		score := calculateSimilarity(normalized, normalizeProductText(products[i].Name))
		if score >= 50.0 { // Minimum 50% similarity
			suggestion := models.ProductMappingSuggestion{
				RawProductText:   productText,
				SuggestedProduct: &products[i],
				Confidence:       score,
				MappingType:      "fuzzy",
			}
			suggestions = append(suggestions, suggestion)
		}
	}

	// Sort by confidence (descending)
	for i := 0; i < len(suggestions)-1; i++ {
		for j := i + 1; j < len(suggestions); j++ {
			if suggestions[j].Confidence > suggestions[i].Confidence {
				suggestions[i], suggestions[j] = suggestions[j], suggestions[i]
			}
		}
	}

	// Limit results
	if len(suggestions) > limit {
		suggestions = suggestions[:limit]
	}

	return suggestions, nil
}
