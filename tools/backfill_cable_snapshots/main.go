// tools/backfill_cable_snapshots/main.go
//
// Backfill script: fetch cable metadata from WarehouseCore and store it as
// JSONB in the job_cables.cable_snapshot column.
//
// Usage:
//
//	go run ./tools/backfill_cable_snapshots [flags]
//
// Required environment variables:
//
//	WAREHOUSECORE_BASE_URL                            – e.g. https://wh.example.com
//
// Optional environment variables:
//
//	WAREHOUSECORE_API_KEY                             – admin API key for authenticated WarehouseCore requests
//	DB_HOST, DB_PORT, DB_NAME, DB_USER, DB_PASSWORD,
//	DB_SSLMODE                                        – PostgreSQL connection settings; if unset,
//	                                                   buildDSN() uses the defaults (localhost:5432,
//	                                                   db=rentalcore, user=rentalcore, sslmode=disable)
//
// Optional flags:
//
//	-batch-size int   rows processed per DB round-trip (default 100)
//	-dry-run          print what would be updated without writing to DB
//	-max-retries int  retries on WarehouseCore 5xx errors (default 3)
//
// Rollback:
//
//	Run migrations/042_add_cable_snapshot.down.sql to drop the column.
//	The CABLE_SNAPSHOT_ENABLED feature flag must be set to false (or unset)
//	before applying the down migration so in-flight requests stop reading
//	the column.

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// cableSnapshot mirrors warehousecore.CableSnapshot and is kept local so this
// standalone tool stays self-contained without importing application packages.
type cableSnapshot struct {
	CableID    int      `json:"cableID"`
	Connector1 int      `json:"connector1"`
	Connector2 int      `json:"connector2"`
	Type       int      `json:"typ"`
	Length     float64  `json:"length"`
	MM2        *float64 `json:"mm2,omitempty"`
	Name       *string  `json:"name,omitempty"`
}

type backfillConfig struct {
	dbDSN       string
	whBaseURL   string
	whAPIKey    string
	batchSize   int
	dryRun      bool
	maxRetries  int
	httpTimeout time.Duration
}

func main() {
	var (
		batchSize  = flag.Int("batch-size", 100, "rows per DB batch")
		dryRun     = flag.Bool("dry-run", false, "print plan without writing")
		maxRetries = flag.Int("max-retries", 3, "retries on 5xx")
	)
	flag.Parse()

	cfg := backfillConfig{
		dbDSN:       buildDSN(),
		whBaseURL:   strings.TrimSuffix(mustEnv("WAREHOUSECORE_BASE_URL"), "/"),
		whAPIKey:    os.Getenv("WAREHOUSECORE_API_KEY"),
		batchSize:   *batchSize,
		dryRun:      *dryRun,
		maxRetries:  *maxRetries,
		httpTimeout: 15 * time.Second,
	}

	if err := run(cfg); err != nil {
		log.Fatalf("backfill failed: %v", err)
	}
}

func run(cfg backfillConfig) error {
	db, err := sql.Open("pgx", cfg.dbDSN)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("ping db: %w", err)
	}

	httpClient := &http.Client{Timeout: cfg.httpTimeout}

	var (
		totalProcessed int
		totalUpdated   int
		totalFailed    int
	)

	log.Printf("Starting cable snapshot backfill (dry-run=%v, batch-size=%d)", cfg.dryRun, cfg.batchSize)

	// cursor holds the last-seen (jobID, cableID) pair for stable keyset
	// pagination.  Rows that fail permanently (non-5xx) are added to skipSet
	// so they are not processed again within this run.  The cursor advances
	// over skipped rows so a full page of skip-set entries never causes an
	// early exit.
	var cursorJobID, cursorCableID int
	skipSet := make(map[[2]int]bool)

	for {
		rows, err := fetchBatch(db, cfg.batchSize, cursorJobID, cursorCableID)
		if err != nil {
			return fmt.Errorf("fetch batch: %w", err)
		}
		if len(rows) == 0 {
			break
		}

		for _, row := range rows {
			// Always advance cursor so the next fetchBatch starts after this row,
			// even for rows we skip – this prevents an infinite loop when an
			// entire page of results is in skipSet.
			cursorJobID, cursorCableID = row.jobID, row.cableID

			if skipSet[[2]int{row.jobID, row.cableID}] {
				continue // already failed permanently this run
			}
			totalProcessed++

			snap, err := fetchCableWithRetry(httpClient, cfg, row.cableID, cfg.maxRetries)
			if err != nil {
				log.Printf("ERROR cableID=%d jobid=%d: %v", row.cableID, row.jobID, err)
				totalFailed++
				// Mark as permanently failed to avoid re-selecting this pair.
				skipSet[[2]int{row.jobID, row.cableID}] = true
				continue
			}

			raw, err := json.Marshal(snap)
			if err != nil {
				log.Printf("ERROR marshal cableID=%d: %v", row.cableID, err)
				totalFailed++
				skipSet[[2]int{row.jobID, row.cableID}] = true
				continue
			}

			if cfg.dryRun {
				log.Printf("DRY-RUN would update jobid=%d cableID=%d snapshot=%s",
					row.jobID, row.cableID, string(raw))
				totalUpdated++
				continue
			}

			updated, err := updateSnapshot(db, row.jobID, row.cableID, raw)
			if err != nil {
				log.Printf("ERROR update jobid=%d cableID=%d: %v", row.jobID, row.cableID, err)
				totalFailed++
				skipSet[[2]int{row.jobID, row.cableID}] = true
				continue
			}
			if !updated {
				// Another writer (AssignCable or a parallel backfill) already
				// populated the snapshot between fetchBatch's SELECT and this
				// UPDATE – log as a skip, not a success.
				log.Printf("SKIP already populated jobid=%d cableID=%d", row.jobID, row.cableID)
				continue
			}

			log.Printf("OK jobid=%d cableID=%d", row.jobID, row.cableID)
			totalUpdated++
		}
	}

	log.Printf("Backfill complete: processed=%d updated=%d failed=%d",
		totalProcessed, totalUpdated, totalFailed)

	if totalFailed > 0 {
		return fmt.Errorf("%d rows failed to backfill", totalFailed)
	}
	return nil
}

type jobCableRow struct {
	jobID   int
	cableID int
}

// fetchBatch returns the next batch of job_cables rows where cable_snapshot IS
// NULL, using keyset pagination (ORDER BY jobid, "cableID") for stable and
// efficient iteration.
func fetchBatch(db *sql.DB, limit, afterJobID, afterCableID int) ([]jobCableRow, error) {
	const q = `SELECT jobid, "cableID"
	             FROM job_cables
	            WHERE cable_snapshot IS NULL
	              AND (jobid > $2 OR (jobid = $2 AND "cableID" > $3))
	            ORDER BY jobid, "cableID"
	            LIMIT $1`

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rws, err := db.QueryContext(ctx, q, limit, afterJobID, afterCableID)
	if err != nil {
		return nil, err
	}
	defer rws.Close()

	var result []jobCableRow
	for rws.Next() {
		var r jobCableRow
		if err := rws.Scan(&r.jobID, &r.cableID); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rws.Err()
}

// updateSnapshot persists the JSONB blob to the database only when the row
// does not yet have a snapshot, guarding against concurrent writes from
// AssignCable or a parallel backfill run.
// Returns (true, nil) when the row was updated, (false, nil) when it was
// already populated by another writer (treated as a benign no-op), and
// (false, err) on DB errors.
func updateSnapshot(db *sql.DB, jobID, cableID int, raw json.RawMessage) (bool, error) {
	const q = `UPDATE job_cables
	              SET cable_snapshot = $1
	            WHERE jobid = $2
	              AND "cableID" = $3
	              AND cable_snapshot IS NULL`

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := db.ExecContext(ctx, q, raw, jobID, cableID)
	if err != nil {
		return false, err
	}

	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	// n == 0: another writer already populated cable_snapshot between
	// fetchBatch's SELECT and this UPDATE – treat as a benign no-op.
	return n > 0, nil
}

// fetchCableWithRetry calls GET /admin/cables/{id} with exponential back-off
// on 5xx responses.
func fetchCableWithRetry(client *http.Client, cfg backfillConfig, cableID, maxRetries int) (*cableSnapshot, error) {
	url := fmt.Sprintf("%s/admin/cables/%d", cfg.whBaseURL, cableID)

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			wait := time.Duration(math.Pow(2, float64(attempt-1))) * time.Second
			log.Printf("retry %d/%d for cableID=%d after %s", attempt, maxRetries, cableID, wait)
			time.Sleep(wait)
		}

		snap, err := doFetch(client, url, cfg.whAPIKey, cableID)
		if err == nil {
			return snap, nil
		}

		// Only retry on 5xx; surface all other errors immediately.
		if !isRetryable(err) || attempt == maxRetries {
			return nil, err
		}
		log.Printf("retryable error for cableID=%d: %v", cableID, err)
	}

	return nil, fmt.Errorf("exhausted %d retries for cable %d", maxRetries, cableID)
}

type retryableError struct{ error }

func isRetryable(err error) bool {
	_, ok := err.(retryableError)
	return ok
}

func doFetch(client *http.Client, url, apiKey string, cableID int) (*cableSnapshot, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, retryableError{fmt.Errorf("GET %s: %w", url, err)}
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("cable %d not found (404)", cableID)
	}
	if resp.StatusCode >= 500 {
		return nil, retryableError{fmt.Errorf("5xx (%d) for cable %d", resp.StatusCode, cableID)}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d for cable %d", resp.StatusCode, cableID)
	}

	var snap cableSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		return nil, fmt.Errorf("decode cable %d: %w", cableID, err)
	}
	return &snap, nil
}

// buildDSN constructs a PostgreSQL connection URL from environment variables.
// Values are percent-encoded so passwords and usernames containing special
// characters (spaces, @, /, etc.) are handled correctly.
func buildDSN() string {
	host := getEnvOrDefault("DB_HOST", "localhost")
	port := getEnvOrDefault("DB_PORT", "5432")
	name := getEnvOrDefault("DB_NAME", "rentalcore")
	user := getEnvOrDefault("DB_USER", "rentalcore")
	pass := getEnvOrDefault("DB_PASSWORD", "")
	ssl := getEnvOrDefault("DB_SSLMODE", "disable")

	// Accept numeric port only
	if _, err := strconv.Atoi(port); err != nil {
		port = "5432"
	}

	u := &url.URL{
		Scheme: "postgres",
		Host:   host + ":" + port,
		Path:   "/" + name,
		User:   url.UserPassword(user, pass),
	}
	q := u.Query()
	q.Set("sslmode", ssl)
	u.RawQuery = q.Encode()
	return u.String()
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required environment variable %s is not set", key)
	}
	return v
}

func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
