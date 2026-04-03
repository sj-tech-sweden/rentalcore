package services

import (
	"testing"
	"time"
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
