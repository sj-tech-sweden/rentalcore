package repository

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-barcode-webapp/internal/models"
	"go-barcode-webapp/internal/services/warehousecore"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newTestJobDB creates an isolated in-memory SQLite database with the tables
// needed for cable-snapshot tests.
func newTestJobDB(t *testing.T) *Database {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	// Minimal schema for the test
	if err := db.AutoMigrate(
		&models.JobCable{},
		&models.Cable{},
		&models.CableConnector{},
		&models.CableType{},
	); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	return &Database{DB: db}
}

// seedCableAndJobCable inserts a Cable and a JobCable row (optionally with a
// snapshot) into the test DB and returns the JobCable.
func seedCableAndJobCable(t *testing.T, db *Database, snap json.RawMessage) models.JobCable {
	t.Helper()

	cable := models.Cable{CableID: 1, Connector1: 1, Connector2: 2, Type: 1, Length: 5.0}
	if err := db.Create(&cable).Error; err != nil {
		t.Fatalf("seed cable: %v", err)
	}

	jc := models.JobCable{JobID: 1, CableID: 1, CableSnapshot: snap}
	if err := db.Create(&jc).Error; err != nil {
		t.Fatalf("seed job_cable: %v", err)
	}

	return jc
}

// ---------------------------------------------------------------------------
// Default mode (cableSnapshotEnabled = false)
// ---------------------------------------------------------------------------

func TestGetJobCables_DefaultMode_NoSnapshot(t *testing.T) {
	db := newTestJobDB(t)
	seedCableAndJobCable(t, db, nil)

	repo := NewJobRepository(db)
	cables, err := repo.GetJobCables(1)
	if err != nil {
		t.Fatalf("GetJobCables() error: %v", err)
	}
	if len(cables) != 1 {
		t.Fatalf("GetJobCables() returned %d rows, want 1", len(cables))
	}
	// In default mode, Cable is populated via DB preload.
	if cables[0].Cable == nil {
		t.Error("Cable should be preloaded in default mode")
	}
}

// ---------------------------------------------------------------------------
// Snapshot mode (cableSnapshotEnabled = true)
// ---------------------------------------------------------------------------

func TestGetJobCables_SnapshotMode_UsesStoredSnapshot(t *testing.T) {
	db := newTestJobDB(t)

	raw := json.RawMessage(`{"cableID":1,"connector1":1,"connector2":2,"typ":1,"length":5.0}`)
	seedCableAndJobCable(t, db, raw)

	repo := NewJobRepository(db)
	repo.cableSnapshotEnabled = true
	// No warehouse client – if code tries to call it we'll get a nil-pointer
	// panic, which would indicate the snapshot path is not being used.

	cables, err := repo.GetJobCables(1)
	if err != nil {
		t.Fatalf("GetJobCables() error: %v", err)
	}
	if len(cables) != 1 {
		t.Fatalf("GetJobCables() returned %d rows, want 1", len(cables))
	}
	if cables[0].Cable == nil {
		t.Fatal("Cable should be populated from snapshot")
	}
	if cables[0].Cable.CableID != 1 {
		t.Errorf("Cable.CableID = %d, want 1", cables[0].Cable.CableID)
	}
}

func TestGetJobCables_SnapshotMode_FetchesFromWarehouseWhenMissing(t *testing.T) {
	db := newTestJobDB(t)
	seedCableAndJobCable(t, db, nil) // no snapshot stored

	// Spin up a fake WarehouseCore server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/cables/1" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"cableID":1,"connector1":1,"connector2":2,"typ":1,"length":5.0}`)) //nolint:errcheck
	}))
	defer srv.Close()

	whClient := warehousecore.NewClientWithConfig(srv.URL, "")

	repo := NewJobRepository(db)
	repo.WithWarehouseCoreClient(whClient, true)

	cables, err := repo.GetJobCables(1)
	if err != nil {
		t.Fatalf("GetJobCables() error: %v", err)
	}
	if len(cables) != 1 {
		t.Fatalf("GetJobCables() returned %d rows, want 1", len(cables))
	}
	if cables[0].Cable == nil {
		t.Fatal("Cable should be populated from WarehouseCore API")
	}
	if cables[0].Cable.CableID != 1 {
		t.Errorf("Cable.CableID = %d, want 1", cables[0].Cable.CableID)
	}
	// Snapshot should now be persisted
	if len(cables[0].CableSnapshot) == 0 {
		t.Error("CableSnapshot should be stored after successful API fetch")
	}
}

func TestGetJobCables_SnapshotMode_FallsBackToDBWhenAPIFails(t *testing.T) {
	db := newTestJobDB(t)
	seedCableAndJobCable(t, db, nil)

	// Fake WarehouseCore that always fails
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	whClient := warehousecore.NewClientWithConfig(srv.URL, "")

	repo := NewJobRepository(db)
	repo.WithWarehouseCoreClient(whClient, true)

	// Should fall back to DB preload without returning an error
	cables, err := repo.GetJobCables(1)
	if err != nil {
		t.Fatalf("GetJobCables() should not error on API failure, got: %v", err)
	}
	if len(cables) != 1 {
		t.Fatalf("GetJobCables() returned %d rows, want 1", len(cables))
	}
	// Cable is populated via DB fallback
	if cables[0].Cable == nil {
		t.Error("Cable should be populated from DB fallback when API fails")
	}
}
