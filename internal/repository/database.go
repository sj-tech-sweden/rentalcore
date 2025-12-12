// Package repository provides SQLite database connection for RentalCore
// Migration von MySQL zu SQLite mit modernc.org/sqlite (CGO-free)
package repository

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"go-barcode-webapp/internal/config"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// Database wraps the GORM database connection
type Database struct {
	*gorm.DB
}

// NewDatabase erstellt eine neue SQLite-Datenbankverbindung
func NewDatabase(cfg *config.DatabaseConfig) (*Database, error) {
	// Stelle sicher, dass das Verzeichnis existiert
	dbDir := filepath.Dir(cfg.DatabasePath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Baue DSN mit SQLite Pragmas
	dsn := buildSQLiteDSN(cfg)

	// GORM Logger Level bestimmen
	var logLevel logger.LogLevel
	if cfg.EnableQueryLogging {
		logLevel = logger.Info
	} else {
		logLevel = logger.Warn
	}

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger:                 logger.Default.LogMode(logLevel),
		PrepareStmt:            cfg.PrepareStmt,
		SkipDefaultTransaction: true,
		CreateBatchSize:        100, // Reduziert für SQLite
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true,
		},
		// Wichtig für SQLite: Keine FK-Constraints beim Migrieren
		DisableForeignKeyConstraintWhenMigrating: cfg.DisableForeignKeyConstraintWhenMigrating,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	// SQLite-optimierte Connection Pool Einstellungen
	// WICHTIG: SQLite unterstützt nur eine Write-Connection!
	maxConns := cfg.MaxOpenConns
	if maxConns <= 0 || maxConns > 1 {
		maxConns = 1 // SQLite erfordert dies für korrekte Writes
	}
	sqlDB.SetMaxOpenConns(maxConns)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(time.Hour)
	sqlDB.SetConnMaxIdleTime(30 * time.Minute)

	// Setze zusätzliche Pragmas nach der Verbindung
	if err := configureSQLitePragmas(db, cfg); err != nil {
		return nil, fmt.Errorf("failed to configure SQLite pragmas: %w", err)
	}

	log.Printf("SQLite database connected: %s", cfg.DatabasePath)
	return &Database{db}, nil
}

// buildSQLiteDSN erstellt den SQLite Connection String
func buildSQLiteDSN(cfg *config.DatabaseConfig) string {
	// In-Memory Datenbank
	if cfg.DatabasePath == ":memory:" {
		return "file::memory:?cache=shared"
	}

	// File-based Database mit Pragmas
	busyTimeout := cfg.BusyTimeout
	if busyTimeout <= 0 {
		busyTimeout = 5000 // 5 Sekunden default
	}

	return fmt.Sprintf("file:%s?_pragma=busy_timeout(%d)&_pragma=foreign_keys(1)",
		cfg.DatabasePath,
		busyTimeout,
	)
}

// configureSQLitePragmas setzt wichtige SQLite Pragmas für Performance
func configureSQLitePragmas(db *gorm.DB, cfg *config.DatabaseConfig) error {
	// Journal Mode
	journalMode := cfg.JournalMode
	if journalMode == "" {
		journalMode = "WAL" // Default: Write-Ahead Logging
	}

	// Synchronous Mode
	synchronous := cfg.Synchronous
	if synchronous == "" {
		synchronous = "NORMAL"
	}

	// Cache Size
	cacheSize := cfg.CacheSize
	if cacheSize == 0 {
		cacheSize = -64000 // 64MB in KiB (negative = KB)
	}

	pragmas := []struct {
		name  string
		value interface{}
	}{
		{"journal_mode", journalMode},
		{"synchronous", synchronous},
		{"cache_size", cacheSize},
		{"temp_store", "MEMORY"},
		{"mmap_size", 268435456}, // 256MB memory-mapped I/O
	}

	for _, p := range pragmas {
		sql := fmt.Sprintf("PRAGMA %s = %v", p.name, p.value)
		if err := db.Exec(sql).Error; err != nil {
			return fmt.Errorf("failed to set pragma %s: %w", p.name, err)
		}
	}

	// Verifiziere WAL-Mode
	var currentJournalMode string
	db.Raw("PRAGMA journal_mode").Scan(&currentJournalMode)
	log.Printf("SQLite journal_mode: %s", currentJournalMode)

	// Verifiziere Foreign Keys
	var fkEnabled int
	db.Raw("PRAGMA foreign_keys").Scan(&fkEnabled)
	log.Printf("SQLite foreign_keys: %d", fkEnabled)

	return nil
}

// Close schließt die Datenbankverbindung sauber
func (db *Database) Close() error {
	sqlDB, err := db.DB.DB()
	if err != nil {
		return err
	}

	// WAL Checkpoint vor dem Schließen
	if err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
		log.Printf("Warning: WAL checkpoint failed: %v", err)
	}

	return sqlDB.Close()
}

// Ping testet die Datenbankverbindung
func (db *Database) Ping() error {
	sqlDB, err := db.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}

// Checkpoint führt einen WAL-Checkpoint durch
// Sollte periodisch aufgerufen werden um die WAL-Datei klein zu halten
func (db *Database) Checkpoint() error {
	return db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error
}

// Vacuum optimiert die Datenbank und gibt ungenutzten Speicherplatz frei
// ACHTUNG: Kann bei großen Datenbanken lange dauern!
func (db *Database) Vacuum() error {
	return db.Exec("VACUUM").Error
}

// Optimize führt SQLite-Optimierungen durch
// Sollte beim Schließen der Datenbank aufgerufen werden
func (db *Database) Optimize() error {
	return db.Exec("PRAGMA optimize").Error
}

// IntegrityCheck führt eine Integritätsprüfung der Datenbank durch
func (db *Database) IntegrityCheck() ([]string, error) {
	var results []string
	rows, err := db.Raw("PRAGMA integrity_check").Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

// GetDatabaseInfo gibt Informationen über die Datenbank zurück
func (db *Database) GetDatabaseInfo() (*DatabaseInfo, error) {
	info := &DatabaseInfo{}

	// Journal Mode
	db.Raw("PRAGMA journal_mode").Scan(&info.JournalMode)

	// Synchronous
	db.Raw("PRAGMA synchronous").Scan(&info.Synchronous)

	// Cache Size
	db.Raw("PRAGMA cache_size").Scan(&info.CacheSize)

	// Page Size
	db.Raw("PRAGMA page_size").Scan(&info.PageSize)

	// Page Count
	db.Raw("PRAGMA page_count").Scan(&info.PageCount)

	// WAL checkpoint info
	var walPages, totalPages, checkpointed int
	db.Raw("PRAGMA wal_checkpoint").Row().Scan(&walPages, &totalPages, &checkpointed)
	info.WALPages = walPages

	return info, nil
}

// DatabaseInfo enthält Metadaten über die SQLite-Datenbank
type DatabaseInfo struct {
	JournalMode string
	Synchronous int
	CacheSize   int
	PageSize    int
	PageCount   int
	WALPages    int
}

// SizeBytes berechnet die ungefähre Datenbankgröße in Bytes
func (info *DatabaseInfo) SizeBytes() int64 {
	return int64(info.PageSize) * int64(info.PageCount)
}

// SizeMB berechnet die ungefähre Datenbankgröße in Megabytes
func (info *DatabaseInfo) SizeMB() float64 {
	return float64(info.SizeBytes()) / (1024 * 1024)
}
