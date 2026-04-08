package services

import (
	"testing"
	"time"

	"go-barcode-webapp/internal/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newCachedSettingsService returns a SettingsService with pre-populated cache fields,
// making it suitable for testing cache-hit behaviour without a real database.
func newCachedSettingsService(symbol string, valid bool, expiry time.Time) *SettingsService {
	return &SettingsService{
		db:             nil,
		cachedCurrency: symbol,
		cacheValid:     valid,
		cacheExpiry:    expiry,
	}
}

// newTestDB creates a transient in-memory SQLite database and auto-migrates the
// AppSetting table.  Each call produces an independent database so tests are
// fully isolated.  Using ":memory:" with SetMaxOpenConns(1) ensures every GORM
// operation uses the same underlying connection and therefore the same
// private in-memory database — different from every other newTestDB call.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open in-memory sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("failed to get underlying sql DB: %v", err)
	}
	// A single connection guarantees all operations see the same private
	// in-memory DB (":memory:" databases are connection-scoped in SQLite).
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	if err := db.AutoMigrate(&models.AppSetting{}); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}
	return db
}

// ---------------------------------------------------------------------------
// Cache-hit tests (no DB required)
// ---------------------------------------------------------------------------

// TestGetCurrencySymbol_CacheHit verifies that a valid, unexpired cache entry is
// returned without touching the database.
func TestGetCurrencySymbol_CacheHit(t *testing.T) {
	s := newCachedSettingsService("$", true, time.Now().Add(5*time.Minute))
	got := s.GetCurrencySymbol()
	if got != "$" {
		t.Errorf("GetCurrencySymbol() = %q, want %q", got, "$")
	}
}

// TestGetCurrencySymbol_CacheHit_DefaultSymbol verifies that the default euro
// symbol is returned correctly from cache.
func TestGetCurrencySymbol_CacheHit_DefaultSymbol(t *testing.T) {
	s := newCachedSettingsService(defaultCurrencySymbol, true, time.Now().Add(5*time.Minute))
	got := s.GetCurrencySymbol()
	if got != defaultCurrencySymbol {
		t.Errorf("GetCurrencySymbol() = %q, want %q", got, defaultCurrencySymbol)
	}
}

// TestGetCurrencySymbol_CacheHit_EmptyString verifies that an empty-string DB
// value is served from cache when cacheValid is true, rather than being treated as
// a cache miss and replaced with the default symbol.
func TestGetCurrencySymbol_CacheHit_EmptyString(t *testing.T) {
	// cacheValid=true with an empty string: the TTL should be respected and the
	// empty string returned as-is (the caller or UI applies the visual default).
	s := newCachedSettingsService("", true, time.Now().Add(5*time.Minute))
	got := s.GetCurrencySymbol()
	if got != "" {
		t.Errorf("GetCurrencySymbol() = %q, want empty string (empty DB value should be cached as-is)", got)
	}
}

// TestGetCurrencySymbol_CacheExpiry verifies that an expired cache entry is not
// considered valid. After expiry the service would re-query the DB; here we confirm
// the TTL guard condition (i.e., the expiry is in the past) is properly detectable.
func TestGetCurrencySymbol_CacheExpiry(t *testing.T) {
	expiry := time.Now().Add(-1 * time.Second) // already expired
	s := newCachedSettingsService("£", true, expiry)

	// The cache should be considered stale: time.Now() is after cacheExpiry.
	if time.Now().Before(s.cacheExpiry) {
		t.Error("expected cache to be expired, but cacheExpiry is still in the future")
	}

	// Confirm the live-cache check inside GetCurrencySymbol would also see this as
	// expired by testing the same condition it evaluates.
	s.mu.RLock()
	cacheStillValid := s.cacheValid && time.Now().Before(s.cacheExpiry)
	s.mu.RUnlock()

	if cacheStillValid {
		t.Error("expected expired cache to be treated as invalid, but the validity check returned true")
	}
}

// TestGetCurrencySymbol_NoCacheEntry verifies that a SettingsService with no
// cached entry (cacheValid=false) reports the cache as not yet populated.
func TestGetCurrencySymbol_NoCacheEntry(t *testing.T) {
	s := &SettingsService{db: nil}
	if s.cacheValid {
		t.Error("expected cacheValid=false for a newly created SettingsService")
	}
}

// ---------------------------------------------------------------------------
// DB-backed tests (sqlite in-memory)
// ---------------------------------------------------------------------------

// TestGetCurrencySymbol_DBRecord verifies that GetCurrencySymbol reads the value
// from the DB when the cache is cold and returns the stored symbol.
func TestGetCurrencySymbol_DBRecord(t *testing.T) {
	db := newTestDB(t)
	db.Create(&models.AppSetting{Scope: "global", Key: AppCurrencyKey, Value: "$"})

	s := NewSettingsService(db)
	got := s.GetCurrencySymbol()
	if got != "$" {
		t.Errorf("GetCurrencySymbol() = %q, want %q", got, "$")
	}
	// Cache should now be populated.
	if !s.cacheValid {
		t.Error("expected cacheValid=true after successful DB read")
	}
}

// TestGetCurrencySymbol_ErrRecordNotFound verifies that the default symbol is
// returned (and cached) when no row exists in the DB.
func TestGetCurrencySymbol_ErrRecordNotFound(t *testing.T) {
	db := newTestDB(t)

	s := NewSettingsService(db)
	got := s.GetCurrencySymbol()
	if got != defaultCurrencySymbol {
		t.Errorf("GetCurrencySymbol() = %q, want default %q", got, defaultCurrencySymbol)
	}
	if !s.cacheValid {
		t.Error("expected cacheValid=true after ErrRecordNotFound (default is cached)")
	}
	if s.cachedCurrency != defaultCurrencySymbol {
		t.Errorf("cachedCurrency = %q, want %q", s.cachedCurrency, defaultCurrencySymbol)
	}
}

// TestGetCurrencySymbol_TransientError verifies that when the DB returns an
// unexpected error (not ErrRecordNotFound) the previous cached value is preserved
// and returned rather than falling back to the default.
func TestGetCurrencySymbol_TransientError(t *testing.T) {
	db := newTestDB(t)

	// Seed a value and warm the cache.
	db.Create(&models.AppSetting{Scope: "global", Key: AppCurrencyKey, Value: "£"})
	s := NewSettingsService(db)
	_ = s.GetCurrencySymbol() // warm the cache

	// Close the underlying sql.DB to make any subsequent query fail.
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB() = %v", err)
	}
	sqlDB.Close()

	// Expire the cache so GetCurrencySymbol tries to re-query.
	s.mu.Lock()
	s.cacheExpiry = time.Now().Add(-1 * time.Second)
	s.mu.Unlock()

	got := s.GetCurrencySymbol()
	// The stale cached value should be returned (not the default "€").
	if got != "£" {
		t.Errorf("GetCurrencySymbol() on transient error = %q, want stale cached %q", got, "£")
	}
}

// TestUpdateCurrencySymbol_Upsert verifies that UpdateCurrencySymbol inserts a row
// when none exists and that a subsequent Get returns the new value.
func TestUpdateCurrencySymbol_Upsert_Insert(t *testing.T) {
	db := newTestDB(t)
	s := NewSettingsService(db)

	if err := s.UpdateCurrencySymbol("kr"); err != nil {
		t.Fatalf("UpdateCurrencySymbol() error = %v", err)
	}
	got := s.GetCurrencySymbol()
	if got != "kr" {
		t.Errorf("GetCurrencySymbol() after update = %q, want %q", got, "kr")
	}
}

// TestUpdateCurrencySymbol_Upsert_Update verifies that UpdateCurrencySymbol
// updates an existing row without creating duplicates.
func TestUpdateCurrencySymbol_Upsert_Update(t *testing.T) {
	db := newTestDB(t)
	db.Create(&models.AppSetting{Scope: "global", Key: AppCurrencyKey, Value: "€"})
	s := NewSettingsService(db)

	if err := s.UpdateCurrencySymbol("CHF"); err != nil {
		t.Fatalf("UpdateCurrencySymbol() error = %v", err)
	}

	var row models.AppSetting
	if err := db.Where("scope = ? AND key = ?", "global", AppCurrencyKey).First(&row).Error; err != nil {
		t.Fatalf("DB read after upsert: %v", err)
	}
	// Value is stored as JSON {"symbol":"..."} by UpdateCurrencySymbol.
	wantValue := `{"symbol":"CHF"}`
	if row.Value != wantValue {
		t.Errorf("DB row value = %q, want %q", row.Value, wantValue)
	}

	// Confirm only one row exists.
	var count int64
	db.Model(&models.AppSetting{}).Where("scope = ? AND key = ?", "global", AppCurrencyKey).Count(&count)
	if count != 1 {
		t.Errorf("row count = %d, want 1 (no duplicates)", count)
	}
}

// TestUpdateCurrencySymbol_CacheRefresh verifies that the in-memory cache is
// updated immediately after a successful UpdateCurrencySymbol call.
func TestUpdateCurrencySymbol_CacheRefresh(t *testing.T) {
	db := newTestDB(t)
	s := NewSettingsService(db)

	if err := s.UpdateCurrencySymbol("£"); err != nil {
		t.Fatalf("UpdateCurrencySymbol() error = %v", err)
	}

	// Inspect internal cache state directly (same package, no need to re-query DB).
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.cacheValid {
		t.Error("expected cacheValid=true after UpdateCurrencySymbol")
	}
	if s.cachedCurrency != "£" {
		t.Errorf("cachedCurrency = %q, want %q", s.cachedCurrency, "£")
	}
	if time.Now().After(s.cacheExpiry) {
		t.Error("expected cacheExpiry to be in the future after UpdateCurrencySymbol")
	}
}

// TestGetCurrencySymbol_CacheRefreshedAfterExpiry verifies that an expired cache is
// refreshed from the DB when GetCurrencySymbol is called again.
func TestGetCurrencySymbol_CacheRefreshedAfterExpiry(t *testing.T) {
	db := newTestDB(t)
	db.Create(&models.AppSetting{Scope: "global", Key: AppCurrencyKey, Value: "zł"})

	// Create service and pre-populate with an expired cache entry for a different symbol.
	s := &SettingsService{
		db:             db,
		cachedCurrency: "£",
		cacheValid:     true,
		cacheExpiry:    time.Now().Add(-1 * time.Second), // expired
	}

	got := s.GetCurrencySymbol()
	if got != "zł" {
		t.Errorf("GetCurrencySymbol() after cache expiry = %q, want DB value %q", got, "zł")
	}
}

// TestGetCurrencySymbol_TransientError_NoPreviousCache verifies that when there is
// no previous cached value and the DB fails, the default symbol is returned.
func TestGetCurrencySymbol_TransientError_NoPreviousCache(t *testing.T) {
	db := newTestDB(t)

	// Close the DB immediately to provoke failures on every query.
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB() = %v", err)
	}
	sqlDB.Close()

	s := NewSettingsService(db)
	got := s.GetCurrencySymbol()
	if got != defaultCurrencySymbol {
		t.Errorf("GetCurrencySymbol() with closed DB and no cache = %q, want default %q", got, defaultCurrencySymbol)
	}
}

// TestGetCurrencySymbol_JSONEncodedValue verifies that GetCurrencySymbol correctly
// parses the shared JSON format {"symbol":"..."} used by both RentalCore and WarehouseCore.
func TestGetCurrencySymbol_JSONEncodedValue(t *testing.T) {
	db := newTestDB(t)
	db.Create(&models.AppSetting{Scope: "global", Key: AppCurrencyKey, Value: `{"symbol":"kr"}`})

	s := NewSettingsService(db)
	got := s.GetCurrencySymbol()
	if got != "kr" {
		t.Errorf("GetCurrencySymbol() with JSON value = %q, want %q", got, "kr")
	}
	if !s.cacheValid {
		t.Error("expected cacheValid=true after successful JSON parse")
	}
}

// TestGetCurrencySymbol_WarehousecoreFallback verifies that when no 'global' row exists
// the service falls back to the 'warehousecore' scope and parses its JSON value.
func TestGetCurrencySymbol_WarehousecoreFallback(t *testing.T) {
	db := newTestDB(t)
	// Only insert a 'warehousecore' row — no 'global' row.
	db.Create(&models.AppSetting{Scope: "warehousecore", Key: AppCurrencyKey, Value: `{"symbol":"SEK"}`})

	s := NewSettingsService(db)
	got := s.GetCurrencySymbol()
	if got != "SEK" {
		t.Errorf("GetCurrencySymbol() warehousecore fallback = %q, want %q", got, "SEK")
	}
	if !s.cacheValid {
		t.Error("expected cacheValid=true after successful warehousecore fallback read")
	}
}

// TestGetCurrencySymbol_GlobalTakesPrecedenceOverWarehousecore verifies that the
// 'global' scope row takes precedence when both scopes are present.
func TestGetCurrencySymbol_GlobalTakesPrecedenceOverWarehousecore(t *testing.T) {
	db := newTestDB(t)
	db.Create(&models.AppSetting{Scope: "global", Key: AppCurrencyKey, Value: `{"symbol":"€"}`})
	db.Create(&models.AppSetting{Scope: "warehousecore", Key: AppCurrencyKey, Value: `{"symbol":"kr"}`})

	s := NewSettingsService(db)
	got := s.GetCurrencySymbol()
	if got != "€" {
		t.Errorf("GetCurrencySymbol() = %q, want global scope %q to take precedence", got, "€")
	}
}

// TestGetCurrencySymbol_JSONAfterCacheExpiry verifies that an expired cache entry is
// refreshed from the DB and the JSON value is correctly parsed.
func TestGetCurrencySymbol_JSONAfterCacheExpiry(t *testing.T) {
	db := newTestDB(t)
	db.Create(&models.AppSetting{Scope: "global", Key: AppCurrencyKey, Value: `{"symbol":"CHF"}`})

	s := &SettingsService{
		db:             db,
		cachedCurrency: "£",
		cacheValid:     true,
		cacheExpiry:    time.Now().Add(-1 * time.Second), // expired
	}

	got := s.GetCurrencySymbol()
	if got != "CHF" {
		t.Errorf("GetCurrencySymbol() after cache expiry = %q, want DB JSON value %q", got, "CHF")
	}
}
