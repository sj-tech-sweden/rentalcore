package services

import (
	"go-barcode-webapp/internal/models"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// AppCurrencyKey is the key used in app_settings to store the currency symbol.
	// This key is intentionally identical to the one used in WarehouseCore so that
	// both applications share the same value when they point at the same database.
	AppCurrencyKey            = "app.currency"
	defaultCurrencySymbol     = "€"
	currencyCacheTTL          = 5 * time.Minute
)

// SettingsService reads and writes application-wide settings from/to the
// shared app_settings table.
type SettingsService struct {
	db             *gorm.DB
	mu             sync.RWMutex
	cachedCurrency string
	cacheExpiry    time.Time
}

// NewSettingsService creates a new SettingsService.
func NewSettingsService(db *gorm.DB) *SettingsService {
	return &SettingsService{db: db}
}

// GetCurrencySymbol returns the configured currency symbol.
// It reads from an in-memory cache (TTL 5 min) and falls back to the database.
// If no value is stored it returns the default "€".
func (s *SettingsService) GetCurrencySymbol() string {
	s.mu.RLock()
	if s.cachedCurrency != "" && time.Now().Before(s.cacheExpiry) {
		defer s.mu.RUnlock()
		return s.cachedCurrency
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock.
	if s.cachedCurrency != "" && time.Now().Before(s.cacheExpiry) {
		return s.cachedCurrency
	}

	var setting models.AppSetting
	if err := s.db.Where("key = ?", AppCurrencyKey).First(&setting).Error; err != nil {
		s.cachedCurrency = defaultCurrencySymbol
	} else {
		s.cachedCurrency = setting.Value
	}
	s.cacheExpiry = time.Now().Add(currencyCacheTTL)
	return s.cachedCurrency
}

// UpdateCurrencySymbol persists a new currency symbol and invalidates the cache.
func (s *SettingsService) UpdateCurrencySymbol(symbol string) error {
	setting := models.AppSetting{
		Key:   AppCurrencyKey,
		Value: symbol,
	}
	if err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&setting).Error; err != nil {
		return err
	}

	s.mu.Lock()
	s.cachedCurrency = symbol
	s.cacheExpiry = time.Now().Add(currencyCacheTTL)
	s.mu.Unlock()
	return nil
}
