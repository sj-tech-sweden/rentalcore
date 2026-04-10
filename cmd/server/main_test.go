package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestBuildWarehouseProductsURLWithEnv(t *testing.T) {
	const domain = "warehouse.example.com"
	if err := os.Setenv("WAREHOUSECORE_DOMAIN", domain); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("WAREHOUSECORE_DOMAIN")
	})

	req := httptest.NewRequest(http.MethodGet, "https://rental.example.com/products", nil)
	req.Header.Set("X-Forwarded-Proto", "https")

	got := buildWarehouseProductsURL(req)
	want := "https://" + domain + "/admin/products"

	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestBuildWarehouseProductsURLFallback(t *testing.T) {
	_ = os.Unsetenv("WAREHOUSECORE_DOMAIN")

	req := httptest.NewRequest(http.MethodGet, "https://rent.example.com/products", nil)
	req.Header.Set("X-Forwarded-Proto", "https")

	got := buildWarehouseProductsURL(req)
	want := "https://warehouse.example.com/admin/products"

	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestBuildWarehouseDevicesURLWithEnv(t *testing.T) {
	const domain = "warehouse.example.com"
	if err := os.Setenv("WAREHOUSECORE_DOMAIN", domain); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("WAREHOUSECORE_DOMAIN")
	})

	req := httptest.NewRequest(http.MethodGet, "https://rental.example.com/devices", nil)
	req.Header.Set("X-Forwarded-Proto", "https")

	got := buildWarehouseDevicesURL(req)
	want := "https://" + domain + "/admin/devices"

	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestBuildWarehouseDevicesURLFallback(t *testing.T) {
	_ = os.Unsetenv("WAREHOUSECORE_DOMAIN")

	req := httptest.NewRequest(http.MethodGet, "https://rent.example.com/devices", nil)
	req.Header.Set("X-Forwarded-Proto", "https")

	got := buildWarehouseDevicesURL(req)
	want := "https://warehouse.example.com/admin/devices"

	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestBuildWarehouseCablesURLWithEnv(t *testing.T) {
	const domain = "warehouse.example.com"
	if err := os.Setenv("WAREHOUSECORE_DOMAIN", domain); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("WAREHOUSECORE_DOMAIN")
	})

	req := httptest.NewRequest(http.MethodGet, "https://rental.example.com/cables", nil)
	req.Header.Set("X-Forwarded-Proto", "https")

	got := buildWarehouseCablesURL(req)
	want := "https://" + domain + "/admin/cables"

	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestBuildWarehouseCablesURLFallback(t *testing.T) {
	_ = os.Unsetenv("WAREHOUSECORE_DOMAIN")

	req := httptest.NewRequest(http.MethodGet, "https://rent.example.com/cables", nil)
	req.Header.Set("X-Forwarded-Proto", "https")

	got := buildWarehouseCablesURL(req)
	want := "https://warehouse.example.com/admin/cables"

	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestBuildWarehouseCablesURLWithPort(t *testing.T) {
	_ = os.Unsetenv("WAREHOUSECORE_DOMAIN")

	req := httptest.NewRequest(http.MethodGet, "http://localhost:8081/cables", nil)

	got := buildWarehouseCablesURL(req)
	want := "http://localhost:8082/admin/cables"

	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestBuildWarehouseCasesURLWithEnv(t *testing.T) {
	_ = os.Setenv("WAREHOUSECORE_DOMAIN", "warehouse.example.com")
	defer func() { _ = os.Unsetenv("WAREHOUSECORE_DOMAIN") }()

	req := httptest.NewRequest(http.MethodGet, "http://rent.example.com/cases", nil)

	got := buildWarehouseCasesURL(req)
	want := "http://warehouse.example.com/admin/cases"

	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestBuildWarehouseCasesURLFallback(t *testing.T) {
	_ = os.Unsetenv("WAREHOUSECORE_DOMAIN")

	req := httptest.NewRequest(http.MethodGet, "http://rent.example.com:8081/cases", nil)

	got := buildWarehouseCasesURL(req)
	want := "http://warehouse.example.com/admin/cases"

	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestBuildWarehouseCasesURLWithPort(t *testing.T) {
	_ = os.Unsetenv("WAREHOUSECORE_DOMAIN")

	req := httptest.NewRequest(http.MethodGet, "http://localhost:8081/cases", nil)

	got := buildWarehouseCasesURL(req)
	want := "http://localhost:8082/admin/cases"

	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

// buildDocsRouter returns a Gin router configured through the same
// registerDocsRoutes helper used by setupRoutes, with a stub handler for doc
// files, so the test stays aligned with production routing as it evolves.
func buildDocsRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// Stub handler: simulate gin-swagger returning 200 for a valid file request.
	registerDocsRoutes(r, func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	return r
}

func TestDocsRouteRedirects(t *testing.T) {
	r := buildDocsRouter()

	tests := []struct {
		path         string
		wantStatus   int
		wantLocation string
	}{
		// Bare /docs → /docs/index.html
		{"/docs", http.StatusMovedPermanently, "/docs/index.html"},
		// Bare /docs/ → /docs/index.html
		{"/docs/", http.StatusMovedPermanently, "/docs/index.html"},
		// Query string must be preserved
		{"/docs?url=custom.json", http.StatusMovedPermanently, "/docs/index.html?url=custom.json"},
		{"/docs/?url=custom.json", http.StatusMovedPermanently, "/docs/index.html?url=custom.json"},
		// Swagger file → 200
		{"/docs/index.html", http.StatusOK, ""},
		// /swagger backward-compat redirects
		{"/swagger", http.StatusMovedPermanently, "/docs/index.html"},
		{"/swagger/", http.StatusMovedPermanently, "/docs/index.html"},
		{"/swagger/index.html", http.StatusMovedPermanently, "/docs/index.html"},
		{"/swagger?url=custom.json", http.StatusMovedPermanently, "/docs/index.html?url=custom.json"},
	}

	for _, tc := range tests {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		r.ServeHTTP(w, req)

		if w.Code != tc.wantStatus {
			t.Errorf("GET %s: got status %d, want %d", tc.path, w.Code, tc.wantStatus)
			continue
		}
		if tc.wantLocation != "" {
			if loc := w.Header().Get("Location"); loc != tc.wantLocation {
				t.Errorf("GET %s: got Location %q, want %q", tc.path, loc, tc.wantLocation)
			}
		}
	}
}
