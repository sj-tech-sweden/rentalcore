package warehousecore

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetCable_Success(t *testing.T) {
	mm2 := 2.5
	name := "Test Cable"
	snap := CableSnapshot{
		CableID:    42,
		Connector1: 1,
		Connector2: 2,
		Type:       3,
		Length:     10.0,
		MM2:        &mm2,
		Name:       &name,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/cables/42" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-API-Key") != "test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(snap) //nolint:errcheck
	}))
	defer srv.Close()

	c := NewClientWithConfig(srv.URL, "test-key")
	got, err := c.GetCable(42)
	if err != nil {
		t.Fatalf("GetCable() unexpected error: %v", err)
	}
	if got.CableID != snap.CableID {
		t.Errorf("CableID = %d, want %d", got.CableID, snap.CableID)
	}
	if got.Connector1 != snap.Connector1 {
		t.Errorf("Connector1 = %d, want %d", got.Connector1, snap.Connector1)
	}
	if got.Connector2 != snap.Connector2 {
		t.Errorf("Connector2 = %d, want %d", got.Connector2, snap.Connector2)
	}
	if got.Type != snap.Type {
		t.Errorf("Type = %d, want %d", got.Type, snap.Type)
	}
	if got.Length != snap.Length {
		t.Errorf("Length = %f, want %f", got.Length, snap.Length)
	}
	if got.MM2 == nil || *got.MM2 != mm2 {
		t.Errorf("MM2 = %v, want %v", got.MM2, mm2)
	}
	if got.Name == nil || *got.Name != name {
		t.Errorf("Name = %v, want %v", got.Name, name)
	}
}

func TestGetCable_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := NewClientWithConfig(srv.URL, "")
	_, err := c.GetCable(99)
	if err == nil {
		t.Fatal("GetCable() expected error for 404, got nil")
	}
	if !errors.Is(err, ErrCableNotFound) {
		t.Errorf("GetCable() 404 error should wrap ErrCableNotFound, got: %v", err)
	}
}

func TestGetCable_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClientWithConfig(srv.URL, "")
	_, err := c.GetCable(1)
	if err == nil {
		t.Fatal("GetCable() expected error for 500, got nil")
	}
}

func TestGetCable_NoAPIKey(t *testing.T) {
	snap := CableSnapshot{CableID: 7, Length: 5.0}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Server accepts requests without an API key too
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(snap) //nolint:errcheck
	}))
	defer srv.Close()

	c := NewClientWithConfig(srv.URL, "")
	got, err := c.GetCable(7)
	if err != nil {
		t.Fatalf("GetCable() unexpected error: %v", err)
	}
	if got.CableID != snap.CableID {
		t.Errorf("CableID = %d, want %d", got.CableID, snap.CableID)
	}
}

func TestNewClientWithConfig_BaseURL(t *testing.T) {
	c := NewClientWithConfig("https://wh.example.com", "key")
	if c.GetBaseURL() != "https://wh.example.com" {
		t.Errorf("GetBaseURL() = %q, want %q", c.GetBaseURL(), "https://wh.example.com")
	}
}

func TestNewClientWithConfig_TrailingSlash(t *testing.T) {
	c := NewClientWithConfig("https://wh.example.com/", "key")
	if c.GetBaseURL() != "https://wh.example.com" {
		t.Errorf("trailing slash not stripped: %q", c.GetBaseURL())
	}
}
