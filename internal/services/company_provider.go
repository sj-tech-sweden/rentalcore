package services

import (
	"strings"
	"sync"
	"time"

	"go-barcode-webapp/internal/models"

	"gorm.io/gorm"
)

const defaultCompanyName = "RentalCore"

// CompanyProvider caches and exposes company branding details.
type CompanyProvider struct {
	db        *gorm.DB
	mu        sync.RWMutex
	name      string
	lastFetch time.Time
	ttl       time.Duration
}

// NewCompanyProvider returns a provider with a five minute cache window.
func NewCompanyProvider(db *gorm.DB) *CompanyProvider {
	return &CompanyProvider{
		db:  db,
		ttl: 5 * time.Minute,
	}
}

// CompanyName returns the cached company name or refreshes it from the database.
func (p *CompanyProvider) CompanyName() string {
	p.mu.RLock()
	name := p.name
	fresh := time.Since(p.lastFetch) < p.ttl && name != ""
	p.mu.RUnlock()
	if fresh {
		return name
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after acquiring write lock.
	if time.Since(p.lastFetch) < p.ttl && p.name != "" {
		return p.name
	}

	var settings models.CompanySettings
	if err := p.db.Order("id DESC").First(&settings).Error; err == nil {
		p.name = sanitizeCompanyName(settings.CompanyName)
	} else if p.name == "" {
		p.name = defaultCompanyName
	}
	p.lastFetch = time.Now()
	return p.name
}

// UpdateName overrides the cached company name, usually after a settings change.
func (p *CompanyProvider) UpdateName(name string) {
	p.mu.Lock()
	p.name = sanitizeCompanyName(name)
	p.lastFetch = time.Now()
	p.mu.Unlock()
}

// Invalidate clears the cache forcing the next read to hit the database.
func (p *CompanyProvider) Invalidate() {
	p.mu.Lock()
	p.lastFetch = time.Time{}
	p.mu.Unlock()
}

func sanitizeCompanyName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return defaultCompanyName
	}
	return trimmed
}
