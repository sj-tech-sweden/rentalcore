package warehousecore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// ErrCableNotFound is returned by GetCable when WarehouseCore responds with 404.
var ErrCableNotFound = errors.New("cable not found in WarehouseCore")

// RentalEquipmentItem represents a rental equipment item from WarehouseCore
type RentalEquipmentItem struct {
	EquipmentID   uint    `json:"equipment_id"`
	ProductName   string  `json:"product_name"`
	SupplierName  string  `json:"supplier_name"`
	RentalPrice   float64 `json:"rental_price"`
	CustomerPrice float64 `json:"customer_price"`
	Category      string  `json:"category"`
	Description   string  `json:"description"`
	IsActive      bool    `json:"is_active"`
}

// CableSnapshot represents cable metadata fetched from the WarehouseCore API.
// This is a point-in-time copy stored in job_cables.cable_snapshot (JSONB).
type CableSnapshot struct {
	CableID    int      `json:"cableID"`
	Connector1 int      `json:"connector1"`
	Connector2 int      `json:"connector2"`
	Type       int      `json:"typ"`
	Length     float64  `json:"length"`
	MM2        *float64 `json:"mm2,omitempty"`
	Name       *string  `json:"name,omitempty"`
}

// Client is a client for communicating with WarehouseCore API
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	mu         sync.RWMutex
	cache      []RentalEquipmentItem
	cacheTime  time.Time
	cacheTTL   time.Duration
}

// NewClient creates a new WarehouseCore client using environment variables.
func NewClient() *Client {
	// Get the WarehouseCore domain from environment variable
	domain := os.Getenv("WAREHOUSECORE_DOMAIN")
	if domain == "" {
		// Fallback for development
		domain = "localhost:8082"
	}

	// Determine protocol based on domain
	protocol := "https"
	if strings.Contains(domain, "localhost") || strings.Contains(domain, "127.0.0.1") {
		protocol = "http"
	}

	baseURL := fmt.Sprintf("%s://%s", protocol, strings.TrimSuffix(domain, "/"))

	return &Client{
		baseURL: baseURL,
		apiKey:  os.Getenv("WAREHOUSECORE_API_KEY"),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		cacheTTL: 5 * time.Minute,
	}
}

// NewClientWithURL creates a client with a specific base URL
func NewClientWithURL(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		cacheTTL: 5 * time.Minute,
	}
}

// NewClientWithConfig creates a client with explicit base URL and API key.
func NewClientWithConfig(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		cacheTTL: 5 * time.Minute,
	}
}

// GetBaseURL returns the configured base URL
func (c *Client) GetBaseURL() string {
	return c.baseURL
}

// addAuthHeader adds the X-API-Key header when an API key is configured.
func (c *Client) addAuthHeader(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
}

// GetCable fetches cable metadata from WarehouseCore using GET /admin/cables/{id}.
// Returns ErrCableNotFound (wrapped) when the API responds with 404.
func (c *Client) GetCable(id int) (*CableSnapshot, error) {
	url := fmt.Sprintf("%s/admin/cables/%d", c.baseURL, id)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create cable request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	c.addAuthHeader(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch cable %d: %w", id, err)
	}
	defer func() {
		// Drain any remaining body content to allow keep-alive connection reuse.
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w (id=%d)", ErrCableNotFound, id)
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("WarehouseCore returned %d for cable %d", resp.StatusCode, id)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d fetching cable %d", resp.StatusCode, id)
	}

	var snap CableSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		return nil, fmt.Errorf("decode cable %d: %w", id, err)
	}

	return &snap, nil
}

// GetRentalEquipment fetches rental equipment from WarehouseCore
func (c *Client) GetRentalEquipment() ([]RentalEquipmentItem, error) {
	// Check cache first
	c.mu.RLock()
	if len(c.cache) > 0 && time.Since(c.cacheTime) < c.cacheTTL {
		result := make([]RentalEquipmentItem, len(c.cache))
		copy(result, c.cache)
		c.mu.RUnlock()
		return result, nil
	}
	c.mu.RUnlock()

	// Fetch from WarehouseCore
	url := fmt.Sprintf("%s/api/v1/rental-equipment", c.baseURL)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	c.addAuthHeader(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch rental equipment: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rental equipment API returned status %d", resp.StatusCode)
	}

	var items []RentalEquipmentItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("decode rental equipment: %w", err)
	}

	// Update cache
	c.mu.Lock()
	c.cache = items
	c.cacheTime = time.Now()
	c.mu.Unlock()

	return items, nil
}

// GetActiveRentalEquipment fetches only active rental equipment
func (c *Client) GetActiveRentalEquipment() ([]RentalEquipmentItem, error) {
	items, err := c.GetRentalEquipment()
	if err != nil {
		return nil, err
	}

	// Filter active items
	active := make([]RentalEquipmentItem, 0, len(items))
	for _, item := range items {
		if item.IsActive {
			active = append(active, item)
		}
	}

	return active, nil
}

// GetRentalEquipmentBySupplier returns rental equipment grouped by supplier
func (c *Client) GetRentalEquipmentBySupplier() (map[string][]RentalEquipmentItem, error) {
	items, err := c.GetActiveRentalEquipment()
	if err != nil {
		return nil, err
	}

	// Group by supplier
	bySupplier := make(map[string][]RentalEquipmentItem)
	for _, item := range items {
		supplier := item.SupplierName
		if supplier == "" {
			supplier = "Unknown Supplier"
		}
		bySupplier[supplier] = append(bySupplier[supplier], item)
	}

	return bySupplier, nil
}

// ClearCache clears the cached rental equipment data
func (c *Client) ClearCache() {
	c.mu.Lock()
	c.cache = nil
	c.cacheTime = time.Time{}
	c.mu.Unlock()
}
