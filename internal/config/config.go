package config

import (
	"encoding/json"
	"os"
	"strconv"
	"time"
	
	"gorm.io/gorm/logger"
)

type Config struct {
	Database DatabaseConfig `json:"database"`
	Server   ServerConfig   `json:"server"`
	UI       UIConfig       `json:"ui"`
	Email    EmailConfig    `json:"email"`
	Invoice  InvoiceConfig  `json:"invoice"`
	PDF      PDFConfig      `json:"pdf"`
	Security SecurityConfig `json:"security"`
	Logging  LoggingConfig  `json:"logging"`
	Backup   BackupConfig   `json:"backup"`
	Features FeaturesConfig `json:"features"`
}

type DatabaseConfig struct {
	// SQLite Configuration
	DatabasePath string `json:"database_path"`
	JournalMode  string `json:"journal_mode"`  // WAL, DELETE, TRUNCATE, PERSIST, MEMORY, OFF
	Synchronous  string `json:"synchronous"`   // OFF (0), NORMAL (1), FULL (2), EXTRA (3)
	CacheSize    int    `json:"cache_size"`    // Negative = KB, Positive = Pages
	BusyTimeout  int    `json:"busy_timeout"`  // Milliseconds

	// Connection Pool (SQLite: max 1 for writes)
	MaxOpenConns    int           `json:"max_open_conns"`
	MaxIdleConns    int           `json:"max_idle_conns"`
	ConnMaxLifetime time.Duration `json:"conn_max_lifetime"`
	ConnMaxIdleTime time.Duration `json:"conn_max_idle_time"`

	// Query settings
	SlowQueryThreshold time.Duration   `json:"slow_query_threshold"`
	EnableQueryLogging bool            `json:"enable_query_logging"`
	LogLevel           logger.LogLevel `json:"-"` // Not serializable
	PrepareStmt        bool            `json:"prepare_stmt"`
	DisableForeignKeyConstraintWhenMigrating bool `json:"disable_fk_when_migrating"`

	// Legacy MySQL fields (kept for backwards compatibility, ignored)
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	Database string `json:"database,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	PoolSize int    `json:"pool_size,omitempty"`
}

type ServerConfig struct {
	Port int    `json:"port"`
	Host string `json:"host"`
}

type UIConfig struct {
	ThemeDark      string            `json:"theme_dark"`
	ThemeLight     string            `json:"theme_light"`
	CurrentTheme   string            `json:"current_theme"`
	Colors         map[string]string `json:"colors"`
	AutoSave       bool              `json:"auto_save"`
	AutoSaveInterval int             `json:"auto_save_interval"`
	CacheTimeout   int               `json:"cache_timeout"`
	WindowWidth    int               `json:"window_width"`
	WindowHeight   int               `json:"window_height"`
}

type EmailConfig struct {
	SMTPHost     string `json:"smtp_host"`
	SMTPPort     int    `json:"smtp_port"`
	SMTPUsername string `json:"smtp_username"`
	SMTPPassword string `json:"smtp_password"`
	FromEmail    string `json:"from_email"`
	FromName     string `json:"from_name"`
	UseTLS       bool   `json:"use_tls"`
}

type InvoiceConfig struct {
	DefaultTaxRate          float64 `json:"default_tax_rate"`
	DefaultPaymentTerms     int     `json:"default_payment_terms"`
	AutoCalculateRentalDays bool    `json:"auto_calculate_rental_days"`
	ShowLogoOnInvoice       bool    `json:"show_logo_on_invoice"`
	InvoiceNumberPrefix     string  `json:"invoice_number_prefix"`
	InvoiceNumberFormat     string  `json:"invoice_number_format"`
	CurrencySymbol          string  `json:"currency_symbol"`
	CurrencyCode            string  `json:"currency_code"`
	DateFormat              string  `json:"date_format"`
}

type PDFConfig struct {
	Generator string            `json:"generator"`
	PaperSize string            `json:"paper_size"`
	Margins   map[string]string `json:"margins"`
}

type SecurityConfig struct {
	SessionTimeout    int    `json:"session_timeout"`
	PasswordMinLength int    `json:"password_min_length"`
	MaxLoginAttempts  int    `json:"max_login_attempts"`
	LockoutDuration   int    `json:"lockout_duration"`
	EncryptionKey     string `json:"encryption_key"`
}

type LoggingConfig struct {
	Level      string `json:"level"`
	File       string `json:"file"`
	MaxSize    int    `json:"max_size"`
	MaxBackups int    `json:"max_backups"`
	MaxAge     int    `json:"max_age"`
}

type BackupConfig struct {
	Enabled       bool   `json:"enabled"`
	Interval      int    `json:"interval"`
	RetentionDays int    `json:"retention_days"`
	Path          string `json:"path"`
}

type FeaturesConfig struct {
	// ScannerEnabled field deprecated - scanner functionality removed
	// Kept for backwards compatibility with existing config files
	ScannerEnabled bool `json:"scanner_enabled"`
}

func LoadConfig(path string) (*Config, error) {
	// Start with default config
	config := getDefaultConfig()
	
	// Override with environment variables if they exist
	loadFromEnvironment(config)
	
	// Try to load from file if it exists
	file, err := os.Open(path)
	if err == nil {
		defer file.Close()
		decoder := json.NewDecoder(file)
		if err := decoder.Decode(config); err != nil {
			return nil, err
		}
		// Override again with environment variables to give them priority
		loadFromEnvironment(config)
	}

	return config, nil
}

func (c *Config) Save(path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(c)
}

func getDefaultConfig() *Config {
	return &Config{
		Database: DatabaseConfig{
			// SQLite defaults
			DatabasePath:       "./data/rentalcore.db",
			JournalMode:        "WAL",
			Synchronous:        "NORMAL",
			CacheSize:          -64000, // 64MB
			BusyTimeout:        5000,   // 5 seconds
			MaxOpenConns:       1,      // SQLite limit
			MaxIdleConns:       1,
			ConnMaxLifetime:    time.Hour,
			ConnMaxIdleTime:    30 * time.Minute,
			SlowQueryThreshold: 500 * time.Millisecond,
			EnableQueryLogging: false,
			LogLevel:           logger.Warn,
			PrepareStmt:        true,
			DisableForeignKeyConstraintWhenMigrating: true,
		},
		Server: ServerConfig{
			Port: 8080,
			Host: "localhost",
		},
		UI: UIConfig{
			ThemeDark:        "darkly",
			ThemeLight:       "flatly",
			CurrentTheme:     "dark",
			AutoSave:         true,
			AutoSaveInterval: 300,
			CacheTimeout:     300,
			WindowWidth:      1400,
			WindowHeight:     800,
			Colors: map[string]string{
				"primary":     "#007bff",
				"background":  "#ffffff",
				"text":        "#000000",
				"selection":   "#e9ecef",
				"success":     "#28a745",
				"error":       "#dc3545",
				"warning":     "#ffc107",
				"dark_bg":     "#2b2b2b",
				"dark_text":   "#ffffff",
			},
		},
		Email: EmailConfig{
			SMTPHost:     "localhost",
			SMTPPort:     587,
			SMTPUsername: "",
			SMTPPassword: "",
			FromEmail:    "noreply@rentalcore.com",
			FromName:     "RentalCore",
			UseTLS:       true,
		},
		Invoice: InvoiceConfig{
			DefaultTaxRate:          19.0,
			DefaultPaymentTerms:     30,
			AutoCalculateRentalDays: true,
			ShowLogoOnInvoice:       true,
			InvoiceNumberPrefix:     "INV-",
			InvoiceNumberFormat:     "{prefix}{year}{month}{sequence:4}",
			CurrencySymbol:          "€",
			CurrencyCode:            "EUR",
			DateFormat:              "DD.MM.YYYY",
		},
		PDF: PDFConfig{
			Generator: "auto",
			PaperSize: "A4",
			Margins: map[string]string{
				"top":    "1cm",
				"bottom": "1cm",
				"left":   "1cm",
				"right":  "1cm",
			},
		},
		Security: SecurityConfig{
			SessionTimeout:    3600,
			PasswordMinLength: 8,
			MaxLoginAttempts:  5,
			LockoutDuration:   900,
			EncryptionKey:     "RentalCore-Demo-Key-CHANGE-IN-PRODUCTION-256-BIT",
		},
		Logging: LoggingConfig{
			Level:      "info",
			File:       "logs/app.log",
			MaxSize:    100,
			MaxBackups: 5,
			MaxAge:     30,
		},
		Backup: BackupConfig{
			Enabled:       true,
			Interval:      86400,
			RetentionDays: 30,
			Path:          "backups/",
		},
		Features: FeaturesConfig{
			ScannerEnabled: false, // Scanner functionality removed
		},
	}
}

// loadFromEnvironment loads configuration from environment variables
func loadFromEnvironment(config *Config) {
	// SQLite Database configuration
	if dbPath := os.Getenv("DB_PATH"); dbPath != "" {
		config.Database.DatabasePath = dbPath
	}
	if journalMode := os.Getenv("DB_JOURNAL_MODE"); journalMode != "" {
		config.Database.JournalMode = journalMode
	}
	if synchronous := os.Getenv("DB_SYNCHRONOUS"); synchronous != "" {
		config.Database.Synchronous = synchronous
	}
	if cacheSize := os.Getenv("DB_CACHE_SIZE"); cacheSize != "" {
		if cs, err := strconv.Atoi(cacheSize); err == nil {
			config.Database.CacheSize = cs
		}
	}
	if busyTimeout := os.Getenv("DB_BUSY_TIMEOUT"); busyTimeout != "" {
		if bt, err := strconv.Atoi(busyTimeout); err == nil {
			config.Database.BusyTimeout = bt
		}
	}
	if maxOpenConns := os.Getenv("DB_MAX_OPEN_CONNS"); maxOpenConns != "" {
		if moc, err := strconv.Atoi(maxOpenConns); err == nil {
			config.Database.MaxOpenConns = moc
		}
	}

	// Server configuration
	if host := os.Getenv("SERVER_HOST"); host != "" {
		config.Server.Host = host
	}
	if port := os.Getenv("SERVER_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			config.Server.Port = p
		}
	}

	// Security configuration
	if key := os.Getenv("ENCRYPTION_KEY"); key != "" {
		config.Security.EncryptionKey = key
	}
	if timeout := os.Getenv("SESSION_TIMEOUT"); timeout != "" {
		if t, err := strconv.Atoi(timeout); err == nil {
			config.Security.SessionTimeout = t
		}
	}

	// Email configuration
	if host := os.Getenv("SMTP_HOST"); host != "" {
		config.Email.SMTPHost = host
	}
	if port := os.Getenv("SMTP_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			config.Email.SMTPPort = p
		}
	}
	if username := os.Getenv("SMTP_USERNAME"); username != "" {
		config.Email.SMTPUsername = username
	}
	if password := os.Getenv("SMTP_PASSWORD"); password != "" {
		config.Email.SMTPPassword = password
	}
	if fromEmail := os.Getenv("FROM_EMAIL"); fromEmail != "" {
		config.Email.FromEmail = fromEmail
	}
	if fromName := os.Getenv("FROM_NAME"); fromName != "" {
		config.Email.FromName = fromName
	}
	if useTLS := os.Getenv("USE_TLS"); useTLS != "" {
		config.Email.UseTLS = useTLS == "true"
	}

	// Invoice configuration
	if taxRate := os.Getenv("DEFAULT_TAX_RATE"); taxRate != "" {
		if rate, err := strconv.ParseFloat(taxRate, 64); err == nil {
			config.Invoice.DefaultTaxRate = rate
		}
	}
	if paymentTerms := os.Getenv("DEFAULT_PAYMENT_TERMS"); paymentTerms != "" {
		if terms, err := strconv.Atoi(paymentTerms); err == nil {
			config.Invoice.DefaultPaymentTerms = terms
		}
	}
	if symbol := os.Getenv("CURRENCY_SYMBOL"); symbol != "" {
		config.Invoice.CurrencySymbol = symbol
	}
	if code := os.Getenv("CURRENCY_CODE"); code != "" {
		config.Invoice.CurrencyCode = code
	}

	// Logging configuration
	if level := os.Getenv("LOG_LEVEL"); level != "" {
		config.Logging.Level = level
	}
	if file := os.Getenv("LOG_FILE"); file != "" {
		config.Logging.File = file
	}

	// Backup configuration
	if enabled := os.Getenv("BACKUP_ENABLED"); enabled != "" {
		config.Backup.Enabled = enabled == "true"
	}
	if interval := os.Getenv("BACKUP_INTERVAL"); interval != "" {
		if i, err := strconv.Atoi(interval); err == nil {
			config.Backup.Interval = i
		}
	}
	if retention := os.Getenv("BACKUP_RETENTION_DAYS"); retention != "" {
		if r, err := strconv.Atoi(retention); err == nil {
			config.Backup.RetentionDays = r
		}
	}

	// Features configuration (deprecated)
	// Scanner functionality has been removed, but we keep the config field for backwards compatibility
	if scannerEnabled := os.Getenv("SCANNER_ENABLED"); scannerEnabled != "" {
		config.Features.ScannerEnabled = false // Always false, scanner removed
	}
}