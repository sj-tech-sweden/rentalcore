package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"go-barcode-webapp/internal/models"
	"log"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// AppCurrencyKey is the key used in app_settings to store the currency symbol.
	// This key is intentionally identical to the one used in WarehouseCore so that
	// both applications share the same value when they point at the same database.
	AppCurrencyKey        = "app.currency"
	defaultCurrencySymbol = "€"
	currencyCacheTTL      = 5 * time.Minute
)

// SettingsService reads and writes application-wide settings from/to the
// shared app_settings table.
type SettingsService struct {
	db             *gorm.DB
	mu             sync.RWMutex
	cachedCurrency string
	cacheValid     bool // true once the cache has been populated (even for empty-string values)
	cacheExpiry    time.Time
}

// NewSettingsService creates a new SettingsService.
func NewSettingsService(db *gorm.DB) *SettingsService {
	return &SettingsService{db: db}
}

// GetCurrencySymbol returns the configured currency symbol.
// It reads from an in-memory cache (TTL 5 min) and falls back to the database.
// On ErrRecordNotFound the default "€" is stored in the cache.
// On any other DB error the previous cached value (if any) is preserved and
// the default is returned without caching, to avoid masking transient errors.
func (s *SettingsService) GetCurrencySymbol() string {
	s.mu.RLock()
	if s.cacheValid && time.Now().Before(s.cacheExpiry) {
		defer s.mu.RUnlock()
		return s.cachedCurrency
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock.
	if s.cacheValid && time.Now().Before(s.cacheExpiry) {
		return s.cachedCurrency
	}

	symbol, cacheable := s.readCurrencyFromDB()
	if cacheable {
		s.cachedCurrency = symbol
		s.cacheValid = true
		s.cacheExpiry = time.Now().Add(currencyCacheTTL)
	} else if s.cacheValid {
		// Preserve stale cached value on transient DB errors.
		return s.cachedCurrency
	}
	return symbol
}

// readCurrencyFromDB tries scope='global' then scope='warehousecore', parsing
// the shared JSON format {"symbol":"..."} used by both services.
// Returns (symbol, cacheable) where cacheable is false on transient DB errors
// (in which case the caller should retain any previously cached value).
func (s *SettingsService) readCurrencyFromDB() (string, bool) {
	cacheable := true
	for _, scope := range []string{"global", "warehousecore"} {
		var setting models.AppSetting
		err := s.db.Where("scope = ? AND key = ?", scope, AppCurrencyKey).First(&setting).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			log.Printf("SettingsService: failed to read %q (scope=%s): %v", AppCurrencyKey, scope, err)
			cacheable = false
			continue
		}
		// Value is stored as JSON {"symbol":"..."} in both scopes.
		var m map[string]interface{}
		if json.Unmarshal([]byte(setting.Value), &m) == nil {
			if sym, ok := m["symbol"].(string); ok && sym != "" {
				return sym, true
			}
		}
		// Fall back to treating the raw value as the symbol (plain-text legacy rows).
		if setting.Value != "" {
			return setting.Value, true
		}
	}
	return defaultCurrencySymbol, cacheable
}

// UpdateCurrencySymbol persists a new currency symbol and invalidates the cache.
// The value is written to scope='global' in the JSON format {"symbol":"..."} so
// that WarehouseCore can also read it via the same shared table.
// Uses a single atomic upsert to avoid races under concurrent writes.
func (s *SettingsService) UpdateCurrencySymbol(symbol string) error {
	jsonBytes, err := json.Marshal(map[string]string{"symbol": symbol})
	if err != nil {
		return fmt.Errorf("failed to encode currency symbol: %w", err)
	}
	jsonValue := string(jsonBytes)

	setting := models.AppSetting{
		Scope: "global",
		Key:   AppCurrencyKey,
		Value: jsonValue,
	}
	if err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "scope"}, {Name: "key"}},
		DoUpdates: clause.Assignments(map[string]interface{}{"value": jsonValue, "updated_at": time.Now()}),
	}).Create(&setting).Error; err != nil {
		return fmt.Errorf("failed to save currency symbol: %w", err)
	}

	s.mu.Lock()
	s.cachedCurrency = symbol
	s.cacheValid = true
	s.cacheExpiry = time.Now().Add(currencyCacheTTL)
	s.mu.Unlock()
	return nil
}
