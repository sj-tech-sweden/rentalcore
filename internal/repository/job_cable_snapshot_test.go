package repository

import (
	"encoding/json"
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

// TestGetJobCables_SnapshotMode_FallsBackToDBWhenSnapshotMissing verifies that
// when snapshot mode is enabled but a cable has no stored snapshot, GetJobCables
// falls back to the local DB preload path without calling WarehouseCore.
// API fill-in is intentionally disabled on the read path; missing snapshots are
// populated by AssignCable or the backfill tool.
func TestGetJobCables_SnapshotMode_FallsBackToDBWhenSnapshotMissing(t *testing.T) {
	db := newTestJobDB(t)
	seedCableAndJobCable(t, db, nil) // no snapshot stored

	repo := NewJobRepository(db)
	repo.cableSnapshotEnabled = true
	// No warehouse client configured; GetJobCables must not call WarehouseCore.

	cables, err := repo.GetJobCables(1)
	if err != nil {
		t.Fatalf("GetJobCables() error: %v", err)
	}
	if len(cables) != 1 {
		t.Fatalf("GetJobCables() returned %d rows, want 1", len(cables))
	}
	if cables[0].Cable == nil {
		t.Fatal("Cable should be populated via DB preload fallback")
	}
	if cables[0].Cable.CableID != 1 {
		t.Errorf("Cable.CableID = %d, want 1", cables[0].Cable.CableID)
	}
	// Snapshot should NOT be written during a read-only GetJobCables call.
	if len(cables[0].CableSnapshot) != 0 {
		t.Error("CableSnapshot should not be persisted on a read path")
	}
}

// TestGetJobCables_SnapshotMode_FallsBackToDBWhenAPIFails verifies that when a
// WarehouseCore client is configured but a snapshot is absent, GetJobCables uses
// the local DB join and does NOT call the API (GetJobCables is read-only).
func TestGetJobCables_SnapshotMode_FallsBackToDBWhenAPIFails(t *testing.T) {
	db := newTestJobDB(t)
	seedCableAndJobCable(t, db, nil)

	// Wire a warehouse client; GetJobCables should not call it regardless.
	repo := NewJobRepository(db)
	repo.WithWarehouseCoreClient(
		warehousecore.NewClientWithConfig("http://127.0.0.1:0", ""),
		true,
	)

	cables, err := repo.GetJobCables(1)
	if err != nil {
		t.Fatalf("GetJobCables() error: %v", err)
	}
	if len(cables) != 1 {
		t.Fatalf("GetJobCables() returned %d rows, want 1", len(cables))
	}
	if cables[0].Cable == nil {
		t.Error("Cable should be populated from DB fallback")
	}
}

func TestGetJobCables_SnapshotMode_PopulatesLookupRelations(t *testing.T) {
	db := newTestJobDB(t)

	// Seed lookup tables so populateCableLookups can resolve names.
	connector1 := models.CableConnector{CableConnectorsID: 1, Name: "XLR"}
	connector2 := models.CableConnector{CableConnectorsID: 2, Name: "RCA"}
	cableType := models.CableType{CableTypesID: 3, Name: "Audio"}
	if err := db.Create(&connector1).Error; err != nil {
		t.Fatalf("seed connector1: %v", err)
	}
	if err := db.Create(&connector2).Error; err != nil {
		t.Fatalf("seed connector2: %v", err)
	}
	if err := db.Create(&cableType).Error; err != nil {
		t.Fatalf("seed cable type: %v", err)
	}

	raw := json.RawMessage(`{"cableID":1,"connector1":1,"connector2":2,"typ":3,"length":5.0}`)
	seedCableAndJobCable(t, db, raw)

	repo := NewJobRepository(db)
	repo.cableSnapshotEnabled = true

	cables, err := repo.GetJobCables(1)
	if err != nil {
		t.Fatalf("GetJobCables() error: %v", err)
	}
	if len(cables) != 1 {
		t.Fatalf("GetJobCables() returned %d rows, want 1", len(cables))
	}
	c := cables[0].Cable
	if c == nil {
		t.Fatal("Cable should be populated from snapshot")
	}
	if c.TypeInfo == nil {
		t.Error("Cable.TypeInfo should be populated by populateCableLookups")
	} else if c.TypeInfo.Name != "Audio" {
		t.Errorf("Cable.TypeInfo.Name = %q, want %q", c.TypeInfo.Name, "Audio")
	}
	if c.Connector1Info == nil {
		t.Error("Cable.Connector1Info should be populated by populateCableLookups")
	} else if c.Connector1Info.Name != "XLR" {
		t.Errorf("Cable.Connector1Info.Name = %q, want %q", c.Connector1Info.Name, "XLR")
	}
	if c.Connector2Info == nil {
		t.Error("Cable.Connector2Info should be populated by populateCableLookups")
	} else if c.Connector2Info.Name != "RCA" {
		t.Errorf("Cable.Connector2Info.Name = %q, want %q", c.Connector2Info.Name, "RCA")
	}
}
