// Package config provides SQLite database configuration for RentalCore
// Diese Datei enthält die SQLite-spezifische Konfiguration
package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// SQLiteDatabaseConfig enthält SQLite-spezifische Konfiguration
type SQLiteDatabaseConfig struct {
	// Pfad zur Datenbankdatei
	DatabasePath string `json:"database_path"`

	// SQLite Pragmas
	JournalMode string `json:"journal_mode"` // WAL, DELETE, TRUNCATE, PERSIST, MEMORY, OFF
	Synchronous string `json:"synchronous"`  // OFF (0), NORMAL (1), FULL (2), EXTRA (3)
	CacheSize   int    `json:"cache_size"`   // Negative Werte = KB, Positive = Pages
	BusyTimeout int    `json:"busy_timeout"` // Millisekunden

	// Connection Pool (SQLite-angepasst)
	MaxOpenConns int `json:"max_open_conns"` // Empfohlen: 1 für Writes

	// GORM Einstellungen
	LogLevel                                 logger.LogLevel `json:"-"`
	SlowQueryThreshold                       time.Duration   `json:"slow_query_threshold"`
	EnableQueryLogging                       bool            `json:"enable_query_logging"`
	PrepareStmt                              bool            `json:"prepare_stmt"`
	DisableForeignKeyConstraintWhenMigrating bool            `json:"disable_fk_when_migrating"`
}

// GetSQLiteDatabaseConfig lädt die SQLite-Konfiguration aus Environment-Variablen
func GetSQLiteDatabaseConfig() *SQLiteDatabaseConfig {
	config := &SQLiteDatabaseConfig{
		DatabasePath:       getEnv("DB_PATH", "./data/rentalcore.db"),
		JournalMode:        getEnv("DB_JOURNAL_MODE", "WAL"),
		Synchronous:        getEnv("DB_SYNCHRONOUS", "NORMAL"),
		CacheSize:          getEnvAsInt("DB_CACHE_SIZE", -64000), // 64MB
		BusyTimeout:        getEnvAsInt("DB_BUSY_TIMEOUT", 5000), // 5 Sekunden
		MaxOpenConns:       getEnvAsInt("DB_MAX_OPEN_CONNS", 1),  // SQLite-Limit!
		SlowQueryThreshold: getEnvAsDuration("DB_SLOW_QUERY_THRESHOLD", 500*time.Millisecond),
		EnableQueryLogging: getEnvAsBool("DB_ENABLE_QUERY_LOGGING", false),
		PrepareStmt:        getEnvAsBool("DB_PREPARE_STMT", true),
		DisableForeignKeyConstraintWhenMigrating: getEnvAsBool("DB_DISABLE_FK_WHEN_MIGRATING", true),
	}

	// Log Level
	if getEnvAsBool("DB_DEBUG", false) {
		config.LogLevel = logger.Info
	} else {
		config.LogLevel = logger.Warn
	}

	return config
}

// GetDefaultSQLiteConfig gibt eine Standard-Konfiguration zurück
func GetDefaultSQLiteConfig() *SQLiteDatabaseConfig {
	return &SQLiteDatabaseConfig{
		DatabasePath:                             "./data/rentalcore.db",
		JournalMode:                              "WAL",
		Synchronous:                              "NORMAL",
		CacheSize:                                -64000, // 64MB
		BusyTimeout:                              5000,   // 5 Sekunden
		MaxOpenConns:                             1,
		LogLevel:                                 logger.Warn,
		SlowQueryThreshold:                       500 * time.Millisecond,
		EnableQueryLogging:                       false,
		PrepareStmt:                              true,
		DisableForeignKeyConstraintWhenMigrating: true,
	}
}

// DSN erstellt den SQLite Connection String
func (c *SQLiteDatabaseConfig) DSN() string {
	if c.DatabasePath == ":memory:" {
		return "file::memory:?cache=shared"
	}

	return fmt.Sprintf("file:%s?_pragma=busy_timeout(%d)&_pragma=foreign_keys(1)",
		c.DatabasePath,
		c.BusyTimeout,
	)
}

// ConnectSQLiteDatabase verbindet zur SQLite-Datenbank
func ConnectSQLiteDatabase(config *SQLiteDatabaseConfig) (*gorm.DB, error) {
	dsn := config.DSN()

	// GORM Konfiguration
	gormConfig := &gorm.Config{
		PrepareStmt:                              config.PrepareStmt,
		DisableForeignKeyConstraintWhenMigrating: config.DisableForeignKeyConstraintWhenMigrating,
		SkipDefaultTransaction:                   true,
		CreateBatchSize:                          100,
		Logger:                                   createSQLiteLogger(config),
	}

	// Verbinde zur Datenbank
	db, err := gorm.Open(sqlite.Open(dsn), gormConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SQLite database: %w", err)
	}

	// Konfiguriere Connection Pool
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	// SQLite-spezifische Pool-Einstellungen
	// WICHTIG: MaxOpenConns = 1 für korrekte Write-Operationen!
	maxConns := config.MaxOpenConns
	if maxConns <= 0 || maxConns > 1 {
		maxConns = 1
	}
	sqlDB.SetMaxOpenConns(maxConns)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(time.Hour)
	sqlDB.SetConnMaxIdleTime(30 * time.Minute)

	// Setze SQLite Pragmas
	if err := configureSQLitePragmas(db, config); err != nil {
		return nil, fmt.Errorf("failed to configure SQLite pragmas: %w", err)
	}

	// Teste die Verbindung
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping SQLite database: %w", err)
	}

	log.Printf("SQLite database connected: %s", config.DatabasePath)

	return db, nil
}

// configureSQLitePragmas setzt Performance-Pragmas
func configureSQLitePragmas(db *gorm.DB, config *SQLiteDatabaseConfig) error {
	pragmas := []struct {
		name  string
		value interface{}
	}{
		{"journal_mode", config.JournalMode},
		{"synchronous", config.Synchronous},
		{"cache_size", config.CacheSize},
		{"temp_store", "MEMORY"},
		{"mmap_size", 268435456}, // 256MB
	}

	for _, p := range pragmas {
		sql := fmt.Sprintf("PRAGMA %s = %v", p.name, p.value)
		if err := db.Exec(sql).Error; err != nil {
			return fmt.Errorf("failed to set pragma %s: %w", p.name, err)
		}
	}

	// Verifiziere und logge
	var journalMode string
	db.Raw("PRAGMA journal_mode").Scan(&journalMode)
	log.Printf("SQLite journal_mode: %s", journalMode)

	var fkEnabled int
	db.Raw("PRAGMA foreign_keys").Scan(&fkEnabled)
	log.Printf("SQLite foreign_keys: %d", fkEnabled)

	return nil
}

// createSQLiteLogger erstellt einen konfigurierten Logger für GORM
func createSQLiteLogger(config *SQLiteDatabaseConfig) logger.Interface {
	logConfig := logger.Config{
		SlowThreshold:             config.SlowQueryThreshold,
		LogLevel:                  config.LogLevel,
		IgnoreRecordNotFoundError: true,
		Colorful:                  true,
	}

	if config.EnableQueryLogging {
		logConfig.LogLevel = logger.Info
	}

	return logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		logConfig,
	)
}

// ============================================================================
// Helper-Funktionen (falls nicht bereits definiert)
// ============================================================================

// getEnv gibt den Wert einer Environment-Variable zurück oder den Default
func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

// getEnvAsInt gibt den Wert einer Environment-Variable als Int zurück
func getEnvAsInt(key string, defaultValue int) int {
	if value, exists := os.LookupEnv(key); exists {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getEnvAsBool gibt den Wert einer Environment-Variable als Bool zurück
func getEnvAsBool(key string, defaultValue bool) bool {
	if value, exists := os.LookupEnv(key); exists {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

// getEnvAsDuration gibt den Wert einer Environment-Variable als Duration zurück
func getEnvAsDuration(key string, defaultValue time.Duration) time.Duration {
	if value, exists := os.LookupEnv(key); exists {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

// GetDatabaseStats returns database connection statistics
func GetDatabaseStats(db *gorm.DB) (map[string]interface{}, error) {
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	stats := sqlDB.Stats()

	return map[string]interface{}{
		"database_type":            "SQLite",
		"max_open_connections":     stats.MaxOpenConnections,
		"open_connections":         stats.OpenConnections,
		"in_use":                   stats.InUse,
		"idle":                     stats.Idle,
		"wait_count":               stats.WaitCount,
		"wait_duration":            stats.WaitDuration.String(),
		"max_idle_closed":          stats.MaxIdleClosed,
		"max_idle_time_closed":     stats.MaxIdleTimeClosed,
		"max_lifetime_closed":      stats.MaxLifetimeClosed,
	}, nil
}

// ApplyPerformanceIndexes applies database indexes for performance
func ApplyPerformanceIndexes(db *gorm.DB) error {
	log.Println("Applying SQLite performance indexes...")

	// Helper function to create index with error handling for SQLite
	createIndex := func(indexName, tableName, columns string) {
		// SQLite: CREATE INDEX IF NOT EXISTS
		indexSQL := fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s(%s)", indexName, tableName, columns)
		if err := db.Exec(indexSQL).Error; err != nil {
			log.Printf("Warning: Failed to create index %s: %v", indexName, err)
		}
	}

	// Apply indexes
	createIndex("idx_devices_productid", "devices", "productID")
	createIndex("idx_devices_status", "devices", "status")
	createIndex("idx_devices_search", "devices", "deviceID, serialnumber")
	createIndex("idx_jobdevices_deviceid", "jobdevices", "deviceID")
	createIndex("idx_jobdevices_jobid", "jobdevices", "jobID")
	createIndex("idx_jobdevices_composite", "jobdevices", "deviceID, jobID")
	createIndex("idx_jobs_customerid", "jobs", "customerID")
	createIndex("idx_jobs_statusid", "jobs", "statusID")
	createIndex("idx_customers_search_company", "customers", "companyname")
	createIndex("idx_customers_search_name", "customers", "firstname, lastname")
	createIndex("idx_customers_email", "customers", "email")
	createIndex("idx_products_categoryid", "products", "categoryID")
	createIndex("idx_products_status", "products", "status")
	createIndex("idx_devices_product_status", "devices", "productID, status")

	log.Println("Performance indexes applied successfully")
	return nil
}