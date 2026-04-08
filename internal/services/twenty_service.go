package services

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"go-barcode-webapp/internal/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	TwentyEnabledKey       = "twenty.enabled"
	TwentyAPIURLKey        = "twenty.api_url"
	TwentyAPIKeyKey        = "twenty.api_key"
	TwentyWebhookSecretKey = "twenty.webhook_secret"
	TwentyCurrencyCodeKey  = "twenty.currency_code"

	// syncSemSize caps the number of concurrent outbound sync goroutines.
	// 20 was chosen to allow a reasonable level of parallelism for bulk saves
	// while bounding memory growth and outbound HTTP connections to the Twenty API.
	syncSemSize = 20
)

// ErrInvalidWebhookToken is returned when the webhook token in the request
// does not match the configured secret.
var ErrInvalidWebhookToken = errors.New("invalid webhook token")

// ErrWebhookBadRequest is returned when an inbound webhook is rejected due to
// a bad or missing client payload (invalid JSON, missing fields, etc.) or
// because inbound sync is not properly configured on the server side.
// Callers should map this to HTTP 400 with a generic message.
var ErrWebhookBadRequest = errors.New("webhook bad request")

// TwentyService manages synchronisation between RentalCore and a Twenty CRM instance.
// Customer records are pushed to Twenty as Companies (company customers) or People
// (individual customers). Jobs are pushed as Opportunities.
// All remote calls are fire-and-forget (goroutine) so they never block the request path.
type TwentyService struct {
	db         *gorm.DB
	httpClient *http.Client
	syncSem    chan struct{} // bounded semaphore for outbound sync goroutines
	// integrationEnabled is an in-memory fast-path flag. It is initialised from the
	// DB in NewTwentyService and kept up-to-date by SaveConfig, so that
	// SyncCustomerAsync / SyncJobAsync can skip the semaphore + goroutine + DB
	// overhead entirely when the integration is disabled.
	integrationEnabled atomic.Bool
}

// NewTwentyService creates a new TwentyService.
func NewTwentyService(db *gorm.DB) *TwentyService {
	s := &TwentyService{
		db: db,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		syncSem: make(chan struct{}, syncSemSize),
	}
	// Initialise the fast-path flag from the persisted configuration so that the
	// very first calls to SyncCustomerAsync / SyncJobAsync skip unnecessary work
	// even before SaveConfig has been invoked.
	s.integrationEnabled.Store(s.GetConfig().Enabled)
	return s
}

// TwentyConfig holds the runtime configuration for the Twenty integration.
type TwentyConfig struct {
	Enabled       bool
	APIURL        string
	APIKey        string
	WebhookSecret string
	CurrencyCode  string // ISO 4217 currency code, e.g. "EUR"
}

// GetConfig reads the current Twenty integration configuration from app_settings.
func (s *TwentyService) GetConfig() TwentyConfig {
	keys := []string{TwentyEnabledKey, TwentyAPIURLKey, TwentyAPIKeyKey, TwentyWebhookSecretKey, TwentyCurrencyCodeKey}
	var settings []models.AppSetting
	if result := s.db.Where("scope = ? AND key IN ?", "global", keys).Find(&settings); result.Error != nil {
		log.Printf("TwentyService: failed to load configuration from app_settings: %v", result.Error)
	}

	cfg := TwentyConfig{
		CurrencyCode: "EUR", // default
	}
	for _, row := range settings {
		switch row.Key {
		case TwentyEnabledKey:
			cfg.Enabled = row.Value == "true"
		case TwentyAPIURLKey:
			cfg.APIURL = row.Value
		case TwentyAPIKeyKey:
			cfg.APIKey = row.Value
		case TwentyWebhookSecretKey:
			cfg.WebhookSecret = row.Value
		case TwentyCurrencyCodeKey:
			if row.Value != "" {
				cfg.CurrencyCode = row.Value
			}
		}
	}
	return cfg
}

// SaveConfig persists the Twenty integration configuration to app_settings.
func (s *TwentyService) SaveConfig(cfg TwentyConfig) error {
	enabledVal := "false"
	if cfg.Enabled {
		enabledVal = "true"
	}
	currencyCode := cfg.CurrencyCode
	if currencyCode == "" {
		currencyCode = "EUR"
	}
	rows := []models.AppSetting{
		{Scope: "global", Key: TwentyEnabledKey, Value: enabledVal},
		{Scope: "global", Key: TwentyAPIURLKey, Value: strings.TrimRight(cfg.APIURL, "/")},
		{Scope: "global", Key: TwentyAPIKeyKey, Value: cfg.APIKey},
		{Scope: "global", Key: TwentyWebhookSecretKey, Value: cfg.WebhookSecret},
		{Scope: "global", Key: TwentyCurrencyCodeKey, Value: currencyCode},
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		for _, row := range rows {
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "scope"}, {Name: "key"}},
				DoUpdates: clause.Assignments(map[string]interface{}{"value": row.Value, "updated_at": time.Now()}),
			}).Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	// Keep the fast-path flag in sync so that SyncCustomerAsync / SyncJobAsync
	// immediately reflect the new enabled state without waiting for the next
	// GetConfig() DB read.
	s.integrationEnabled.Store(cfg.Enabled)
	return nil
}

// TestConnection verifies connectivity to the Twenty API and returns an error if it fails.
func (s *TwentyService) TestConnection() error {
	cfg := s.GetConfig()
	if cfg.APIURL == "" {
		return errors.New("twenty API URL is not configured")
	}
	if cfg.APIKey == "" {
		return errors.New("twenty API key is not configured")
	}

	payload, err := json.Marshal(gqlRequest{Query: "{ __typename }"})
	if err != nil {
		return fmt.Errorf("failed to encode test query: %w", err)
	}
	resp, err := s.doRequest(cfg, payload)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return errors.New("authentication failed: invalid API key")
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("twenty API returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// SyncCustomerAsync triggers an asynchronous best-effort sync of a customer to Twenty CRM.
// The call is dropped (with a log message) if the semaphore is full.
func (s *TwentyService) SyncCustomerAsync(customer *models.Customer) {
	// Fast-path: skip semaphore + goroutine + DB round-trip when the integration
	// is known to be disabled (updated synchronously by SaveConfig).
	if !s.integrationEnabled.Load() {
		return
	}
	c := *customer // copy to avoid data race
	select {
	case s.syncSem <- struct{}{}:
		go func() {
			defer func() { <-s.syncSem }()
			if err := s.syncCustomer(&c); err != nil {
				log.Printf("TwentyService: SyncCustomer for customer %d failed: %v", c.CustomerID, err)
			}
		}()
	default:
		log.Printf("TwentyService: sync queue full, skipping outbound sync for customer %d", customer.CustomerID)
	}
}

// SyncJobAsync triggers an asynchronous best-effort sync of a job to Twenty CRM.
// The call is dropped (with a log message) if the semaphore is full.
func (s *TwentyService) SyncJobAsync(job *models.Job) {
	// Fast-path: skip semaphore + goroutine + DB round-trip when the integration
	// is known to be disabled (updated synchronously by SaveConfig).
	if !s.integrationEnabled.Load() {
		return
	}
	j := *job // copy to avoid data race
	select {
	case s.syncSem <- struct{}{}:
		go func() {
			defer func() { <-s.syncSem }()
			if err := s.syncJob(&j); err != nil {
				log.Printf("TwentyService: SyncJob for job %d failed: %v", j.JobID, err)
			}
		}()
	default:
		log.Printf("TwentyService: sync queue full, skipping outbound sync for job %d", job.JobID)
	}
}

// syncCustomer pushes a customer to Twenty CRM.
// Company-type customers (or customers with a company name) are synced as Companies;
// individual customers are synced as People.
func (s *TwentyService) syncCustomer(customer *models.Customer) error {
	cfg := s.GetConfig()
	if !cfg.Enabled || cfg.APIURL == "" || cfg.APIKey == "" {
		return nil
	}

	isCompany := customer.CompanyName != nil && *customer.CompanyName != ""
	if isCompany {
		return s.upsertCompany(cfg, customer)
	}
	return s.upsertPerson(cfg, customer)
}

// syncJob pushes a job to Twenty CRM as an Opportunity.
func (s *TwentyService) syncJob(job *models.Job) error {
	cfg := s.GetConfig()
	if !cfg.Enabled || cfg.APIURL == "" || cfg.APIKey == "" {
		return nil
	}
	return s.upsertOpportunity(cfg, job)
}

// ---------- Bidirectional sync: inbound webhook from Twenty ----------

// TwentyWebhookPayload is the incoming webhook event from Twenty CRM.
type TwentyWebhookPayload struct {
	Type   string                     `json:"type"`
	Record map[string]json.RawMessage `json:"record"`
}

// ApplyInboundWebhook processes an incoming Twenty CRM webhook and updates the
// corresponding RentalCore customer record.
// Only records that were originally synced FROM RentalCore are affected.
// The webhook secret must be configured; requests without a valid token are rejected.
func (s *TwentyService) ApplyInboundWebhook(body []byte, webhookToken string) error {
	cfg := s.GetConfig()

	// Reject if the integration is disabled.
	if !cfg.Enabled {
		return nil
	}

	// A webhook secret is required; reject unauthenticated requests.
	if cfg.WebhookSecret == "" {
		return fmt.Errorf("%w: webhook secret is not configured", ErrWebhookBadRequest)
	}
	// Hash both values with SHA-256 before comparing so that
	// subtle.ConstantTimeCompare always operates on equal-length slices,
	// eliminating the length-based early-exit timing signal.
	// TrimSpace on the header guards against accidental surrounding whitespace.
	tokenHash := sha256.Sum256([]byte(strings.TrimSpace(webhookToken)))
	secretHash := sha256.Sum256([]byte(cfg.WebhookSecret))
	if subtle.ConstantTimeCompare(tokenHash[:], secretHash[:]) != 1 {
		return ErrInvalidWebhookToken
	}

	var payload TwentyWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("%w: invalid payload: %v", ErrWebhookBadRequest, err)
	}

	// Determine object type from event type, e.g. "company.updated" → "company".
	parts := strings.SplitN(payload.Type, ".", 2)
	if len(parts) != 2 {
		return nil // unknown type; ignore silently
	}
	objectType := parts[0]
	if objectType != "company" && objectType != "person" {
		return nil // not a type we handle
	}

	// Extract the Twenty record ID.
	idRaw, ok := payload.Record["id"]
	if !ok {
		return fmt.Errorf("%w: record missing id field", ErrWebhookBadRequest)
	}
	var twentyID string
	if err := json.Unmarshal(idRaw, &twentyID); err != nil || twentyID == "" {
		return fmt.Errorf("%w: record id is invalid or empty", ErrWebhookBadRequest)
	}

	// Find the RentalCore customer that maps to this Twenty record.
	customerID, err := s.reverseCustomerIDLookup(twentyID, objectType)
	if err != nil {
		return fmt.Errorf("reverse lookup failed: %w", err)
	}
	if customerID == 0 {
		// Not a record we synced from RentalCore; ignore.
		return nil
	}

	// Load and update the customer.
	var customer models.Customer
	if err := s.db.First(&customer, customerID).Error; err != nil {
		return fmt.Errorf("customer %d not found: %w", customerID, err)
	}

	if objectType == "company" {
		s.applyCompanyWebhook(&customer, payload.Record)
	} else {
		s.applyPersonWebhook(&customer, payload.Record)
	}

	return s.db.Save(&customer).Error
}

// reverseCustomerIDLookup finds the RentalCore customerID that maps to a given
// Twenty object ID and object type ("company" or "person").
// Returns 0 if no mapping exists.
func (s *TwentyService) reverseCustomerIDLookup(twentyID, objectType string) (uint, error) {
	prefix := "twenty." + objectType + "."
	var setting models.AppSetting
	result := s.db.
		Where("scope = ? AND key LIKE ? AND value = ?", "global", prefix+"%", twentyID).
		Limit(1).
		Find(&setting)
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected == 0 {
		return 0, nil
	}
	idStr := strings.TrimPrefix(setting.Key, prefix)
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		return 0, nil
	}
	return uint(id), nil
}

// applyCompanyWebhook maps Twenty company fields onto a RentalCore customer.
// Only non-empty values are applied to avoid wiping existing customer data.
func (s *TwentyService) applyCompanyWebhook(customer *models.Customer, record map[string]json.RawMessage) {
	if raw, ok := record["name"]; ok {
		var name string
		if err := json.Unmarshal(raw, &name); err == nil && name != "" {
			customer.CompanyName = &name
		}
	}
	if raw, ok := record["address"]; ok {
		var addr struct {
			Street1  string `json:"addressStreet1"`
			City     string `json:"addressCity"`
			State    string `json:"addressState"`
			Postcode string `json:"addressPostcode"`
			Country  string `json:"addressCountry"`
		}
		if err := json.Unmarshal(raw, &addr); err == nil {
			if strings.TrimSpace(addr.Street1) != "" {
				customer.Street = &addr.Street1
			}
			if strings.TrimSpace(addr.City) != "" {
				customer.City = &addr.City
			}
			if strings.TrimSpace(addr.State) != "" {
				customer.FederalState = &addr.State
			}
			if strings.TrimSpace(addr.Postcode) != "" {
				customer.ZIP = &addr.Postcode
			}
			if strings.TrimSpace(addr.Country) != "" {
				customer.Country = &addr.Country
			}
		}
	}
}

// applyPersonWebhook maps Twenty person fields onto a RentalCore customer.
// Only non-empty values are applied to avoid wiping existing customer data.
func (s *TwentyService) applyPersonWebhook(customer *models.Customer, record map[string]json.RawMessage) {
	if raw, ok := record["name"]; ok {
		var name struct {
			FirstName string `json:"firstName"`
			LastName  string `json:"lastName"`
		}
		if err := json.Unmarshal(raw, &name); err == nil {
			if strings.TrimSpace(name.FirstName) != "" {
				customer.FirstName = &name.FirstName
			}
			if strings.TrimSpace(name.LastName) != "" {
				customer.LastName = &name.LastName
			}
		}
	}
	if raw, ok := record["emails"]; ok {
		var emails struct {
			PrimaryEmail string `json:"primaryEmail"`
		}
		if err := json.Unmarshal(raw, &emails); err == nil && strings.TrimSpace(emails.PrimaryEmail) != "" {
			customer.Email = &emails.PrimaryEmail
		}
	}
	if raw, ok := record["phones"]; ok {
		var phones struct {
			PrimaryPhoneNumber string `json:"primaryPhoneNumber"`
		}
		if err := json.Unmarshal(raw, &phones); err == nil && strings.TrimSpace(phones.PrimaryPhoneNumber) != "" {
			customer.PhoneNumber = &phones.PrimaryPhoneNumber
		}
	}
}

// ---------- Twenty GraphQL helpers ----------

type gqlRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

type gqlResponse struct {
	Data   map[string]json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (s *TwentyService) doRequest(cfg TwentyConfig, body []byte) (*http.Response, error) {
	apiURL := cfg.APIURL + "/api"
	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	return s.httpClient.Do(req)
}

func (s *TwentyService) execGQL(cfg TwentyConfig, query string, variables map[string]interface{}) (map[string]json.RawMessage, error) {
	payload, err := json.Marshal(gqlRequest{Query: query, Variables: variables})
	if err != nil {
		return nil, err
	}

	resp, err := s.doRequest(cfg, payload)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Check HTTP status before attempting to decode as JSON.
	if resp.StatusCode >= 400 {
		snippet, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
		if readErr != nil {
			return nil, fmt.Errorf("Twenty API HTTP %d (could not read body: %v)", resp.StatusCode, readErr)
		}
		return nil, fmt.Errorf("Twenty API HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var gqlResp gqlResponse
	if err := json.Unmarshal(raw, &gqlResp); err != nil {
		return nil, fmt.Errorf("failed to decode Twenty response: %w", err)
	}
	if len(gqlResp.Errors) > 0 {
		msgs := make([]string, len(gqlResp.Errors))
		for i, e := range gqlResp.Errors {
			msgs[i] = e.Message
		}
		return nil, fmt.Errorf("Twenty API errors: %s", strings.Join(msgs, "; "))
	}
	return gqlResp.Data, nil
}

// storedID returns the Twenty object ID previously stored for a given RentalCore object.
// It returns ("", nil) when no mapping exists yet, and ("", err) on unexpected DB errors.
func (s *TwentyService) storedID(settingsKey string) (string, error) {
	var setting models.AppSetting
	if err := s.db.Where("scope = ? AND key = ?", "global", settingsKey).First(&setting).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("storedID(%q): %w", settingsKey, err)
	}
	return setting.Value, nil
}

// storeID persists a Twenty object ID mapping.
func (s *TwentyService) storeID(settingsKey, twentyID string) {
	row := models.AppSetting{Scope: "global", Key: settingsKey, Value: twentyID}
	if err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "scope"}, {Name: "key"}},
		DoUpdates: clause.Assignments(map[string]interface{}{"value": twentyID, "updated_at": time.Now()}),
	}).Create(&row).Error; err != nil {
		log.Printf("TwentyService: failed to store ID mapping %q: %v", settingsKey, err)
	}
}

// amountMicros converts a float64 amount to micros (millionths) using rounding.
func amountMicros(amount float64) int64 {
	return int64(math.Round(amount * 1_000_000))
}

// ---------- Company sync ----------

func (s *TwentyService) upsertCompany(cfg TwentyConfig, c *models.Customer) error {
	mappingKey := fmt.Sprintf("twenty.company.%d", c.CustomerID)
	existingID, err := s.storedID(mappingKey)
	if err != nil {
		return err
	}

	name := ""
	if c.CompanyName != nil {
		name = *c.CompanyName
	}
	if name == "" {
		name = c.GetDisplayName()
	}

	address := buildAddress(c)

	if existingID == "" {
		// Create
		const q = `
mutation CreateOneCompany($data: CompanyCreateInput!) {
  createCompany(data: $data) { id }
}`
		data := map[string]interface{}{
			"name":       name,
			"domainName": map[string]interface{}{"primaryLinkUrl": "", "primaryLinkLabel": ""},
			"address":    address,
		}
		vars := map[string]interface{}{"data": data}
		resp, err := s.execGQL(cfg, q, vars)
		if err != nil {
			return err
		}
		id := extractID(resp, "createCompany")
		if id != "" {
			s.storeID(mappingKey, id)
		}
		return nil
	}

	// Update
	const q = `
mutation UpdateOneCompany($id: ID!, $data: CompanyUpdateInput!) {
  updateCompany(id: $id, data: $data) { id }
}`
	data := map[string]interface{}{
		"name":    name,
		"address": address,
	}
	vars := map[string]interface{}{"id": existingID, "data": data}
	_, err = s.execGQL(cfg, q, vars)
	return err
}

// ---------- Person sync ----------

func (s *TwentyService) upsertPerson(cfg TwentyConfig, c *models.Customer) error {
	mappingKey := fmt.Sprintf("twenty.person.%d", c.CustomerID)
	existingID, err := s.storedID(mappingKey)
	if err != nil {
		return err
	}

	firstName := ""
	if c.FirstName != nil {
		firstName = *c.FirstName
	}
	lastName := ""
	if c.LastName != nil {
		lastName = *c.LastName
	}
	email := ""
	if c.Email != nil {
		email = *c.Email
	}
	phone := ""
	if c.PhoneNumber != nil {
		phone = *c.PhoneNumber
	}

	if existingID == "" {
		const q = `
mutation CreateOnePerson($data: PersonCreateInput!) {
  createPerson(data: $data) { id }
}`
		data := map[string]interface{}{
			"name":   map[string]interface{}{"firstName": firstName, "lastName": lastName},
			"emails": map[string]interface{}{"primaryEmail": email},
			"phones": map[string]interface{}{"primaryPhoneNumber": phone, "primaryPhoneCountryCode": ""},
		}
		vars := map[string]interface{}{"data": data}
		resp, err := s.execGQL(cfg, q, vars)
		if err != nil {
			return err
		}
		id := extractID(resp, "createPerson")
		if id != "" {
			s.storeID(mappingKey, id)
		}
		return nil
	}

	const q = `
mutation UpdateOnePerson($id: ID!, $data: PersonUpdateInput!) {
  updatePerson(id: $id, data: $data) { id }
}`
	data := map[string]interface{}{
		"name":   map[string]interface{}{"firstName": firstName, "lastName": lastName},
		"emails": map[string]interface{}{"primaryEmail": email},
		"phones": map[string]interface{}{"primaryPhoneNumber": phone, "primaryPhoneCountryCode": ""},
	}
	vars := map[string]interface{}{"id": existingID, "data": data}
	_, err = s.execGQL(cfg, q, vars)
	return err
}

// ---------- Opportunity sync ----------

func (s *TwentyService) upsertOpportunity(cfg TwentyConfig, job *models.Job) error {
	mappingKey := fmt.Sprintf("twenty.opportunity.%d", job.JobID)
	existingID, err := s.storedID(mappingKey)
	if err != nil {
		return err
	}

	name := job.JobCode
	if name == "" {
		name = fmt.Sprintf("Job #%d", job.JobID)
	}

	desc := ""
	if job.Description != nil {
		desc = *job.Description
	}

	stage := "NEW"
	if job.Status.Status != "" {
		stage = mapJobStatusToStage(job.Status.Status)
	}

	closeDate := ""
	if job.EndDate != nil {
		closeDate = job.EndDate.Format("2006-01-02")
	}

	amount := job.Revenue
	if job.FinalRevenue != nil {
		amount = *job.FinalRevenue
	}

	amtObj := map[string]interface{}{
		"amountMicros": amountMicros(amount),
		"currencyCode": cfg.CurrencyCode,
	}

	if existingID == "" {
		const q = `
mutation CreateOneOpportunity($data: OpportunityCreateInput!) {
  createOpportunity(data: $data) { id }
}`
		data := map[string]interface{}{
			"name":      name,
			"stage":     stage,
			"amount":    amtObj,
			"closeDate": closeDate,
		}
		if desc != "" {
			data["pointOfContactNote"] = desc
		}
		vars := map[string]interface{}{"data": data}
		resp, err := s.execGQL(cfg, q, vars)
		if err != nil {
			return err
		}
		id := extractID(resp, "createOpportunity")
		if id != "" {
			s.storeID(mappingKey, id)
		}
		return nil
	}

	const q = `
mutation UpdateOneOpportunity($id: ID!, $data: OpportunityUpdateInput!) {
  updateOpportunity(id: $id, data: $data) { id }
}`
	data := map[string]interface{}{
		"name":      name,
		"stage":     stage,
		"amount":    amtObj,
		"closeDate": closeDate,
	}
	if desc != "" {
		data["pointOfContactNote"] = desc
	}
	vars := map[string]interface{}{"id": existingID, "data": data}
	_, err = s.execGQL(cfg, q, vars)
	return err
}

// ---------- Helpers ----------

func buildAddress(c *models.Customer) map[string]interface{} {
	street := ""
	if c.Street != nil {
		street = *c.Street
	}
	if c.HouseNumber != nil && *c.HouseNumber != "" {
		street = strings.TrimSpace(street + " " + *c.HouseNumber)
	}
	city := ""
	if c.City != nil {
		city = *c.City
	}
	state := ""
	if c.FederalState != nil {
		state = *c.FederalState
	}
	zip := ""
	if c.ZIP != nil {
		zip = *c.ZIP
	}
	country := ""
	if c.Country != nil {
		country = *c.Country
	}
	return map[string]interface{}{
		"addressStreet1":  street,
		"addressCity":     city,
		"addressState":    state,
		"addressPostcode": zip,
		"addressCountry":  country,
	}
}

func mapJobStatusToStage(status string) string {
	lower := strings.ToLower(status)
	switch {
	case strings.Contains(lower, "new") || strings.Contains(lower, "open"):
		return "NEW"
	case strings.Contains(lower, "progress") || strings.Contains(lower, "active"):
		return "IN_PROGRESS"
	case strings.Contains(lower, "won") || strings.Contains(lower, "complet") || strings.Contains(lower, "done"):
		return "CLOSED_WON"
	case strings.Contains(lower, "lost") || strings.Contains(lower, "cancel"):
		return "CLOSED_LOST"
	default:
		return "NEW"
	}
}

// extractID pulls the "id" field out of a Twenty mutation response.
func extractID(data map[string]json.RawMessage, operationName string) string {
	raw, ok := data[operationName]
	if !ok {
		return ""
	}
	var obj struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	return obj.ID
}
