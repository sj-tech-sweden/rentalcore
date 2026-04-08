package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"go-barcode-webapp/internal/models"

	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// TwentyService – GetConfig / SaveConfig tests
// ---------------------------------------------------------------------------

func TestTwentyService_GetConfig_Defaults(t *testing.T) {
	db := newTestDB(t)
	svc := NewTwentyService(db)

	cfg := svc.GetConfig()
	if cfg.Enabled {
		t.Error("expected Enabled=false for empty config")
	}
	if cfg.APIURL != "" {
		t.Errorf("expected empty APIURL, got %q", cfg.APIURL)
	}
	if cfg.APIKey != "" {
		t.Errorf("expected empty APIKey, got %q", cfg.APIKey)
	}
	if cfg.CurrencyCode != "EUR" {
		t.Errorf("expected default CurrencyCode=EUR, got %q", cfg.CurrencyCode)
	}
}

func TestTwentyService_SaveAndGetConfig(t *testing.T) {
	db := newTestDB(t)
	svc := NewTwentyService(db)

	want := TwentyConfig{
		Enabled:       true,
		APIURL:        "https://crm.example.com",
		APIKey:        "secret-api-key",
		WebhookSecret: "webhook-secret",
		CurrencyCode:  "USD",
	}
	if err := svc.SaveConfig(want); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	got := svc.GetConfig()
	if got.Enabled != want.Enabled {
		t.Errorf("Enabled: got %v, want %v", got.Enabled, want.Enabled)
	}
	if got.APIURL != want.APIURL {
		t.Errorf("APIURL: got %q, want %q", got.APIURL, want.APIURL)
	}
	if got.APIKey != want.APIKey {
		t.Errorf("APIKey: got %q, want %q", got.APIKey, want.APIKey)
	}
	if got.WebhookSecret != want.WebhookSecret {
		t.Errorf("WebhookSecret: got %q, want %q", got.WebhookSecret, want.WebhookSecret)
	}
	if got.CurrencyCode != want.CurrencyCode {
		t.Errorf("CurrencyCode: got %q, want %q", got.CurrencyCode, want.CurrencyCode)
	}
}

func TestTwentyService_SaveConfig_Upsert(t *testing.T) {
	db := newTestDB(t)
	svc := NewTwentyService(db)

	if err := svc.SaveConfig(TwentyConfig{Enabled: true, APIURL: "https://a.example.com", APIKey: "key1", CurrencyCode: "EUR"}); err != nil {
		t.Fatalf("first SaveConfig: %v", err)
	}
	if err := svc.SaveConfig(TwentyConfig{Enabled: false, APIURL: "https://b.example.com", APIKey: "key2", CurrencyCode: "GBP"}); err != nil {
		t.Fatalf("second SaveConfig: %v", err)
	}

	got := svc.GetConfig()
	if got.Enabled {
		t.Error("expected Enabled=false after second save")
	}
	if got.APIURL != "https://b.example.com" {
		t.Errorf("APIURL: got %q, want %q", got.APIURL, "https://b.example.com")
	}
	if got.APIKey != "key2" {
		t.Errorf("APIKey: got %q, want %q", got.APIKey, "key2")
	}
	if got.CurrencyCode != "GBP" {
		t.Errorf("CurrencyCode: got %q, want %q", got.CurrencyCode, "GBP")
	}

	var count int64
	db.Model(&models.AppSetting{}).Where("scope = ? AND key = ?", "global", TwentyEnabledKey).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 row for %q, got %d", TwentyEnabledKey, count)
	}
}

func TestTwentyService_SaveConfig_TrailingSlash(t *testing.T) {
	db := newTestDB(t)
	svc := NewTwentyService(db)

	if err := svc.SaveConfig(TwentyConfig{Enabled: true, APIURL: "https://a.example.com/", APIKey: "k", CurrencyCode: "EUR"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got := svc.GetConfig()
	if got.APIURL != "https://a.example.com" {
		t.Errorf("expected trailing slash stripped, got %q", got.APIURL)
	}
}

// ---------------------------------------------------------------------------
// Webhook inbound / bidirectional sync tests
// ---------------------------------------------------------------------------

// helperEnableWebhook enables the Twenty integration and sets a webhook secret.
func helperEnableWebhook(t *testing.T, db *gorm.DB, secret string) {
	t.Helper()
	db.Create(&models.AppSetting{Scope: "global", Key: TwentyEnabledKey, Value: "true"})
	db.Create(&models.AppSetting{Scope: "global", Key: TwentyAPIURLKey, Value: "https://twenty.example.com"})
	db.Create(&models.AppSetting{Scope: "global", Key: TwentyAPIKeyKey, Value: "test-key"})
	db.Create(&models.AppSetting{Scope: "global", Key: TwentyWebhookSecretKey, Value: secret})
}

func TestTwentyService_ApplyInboundWebhook_InvalidToken(t *testing.T) {
	db := newTestDB(t)
	svc := NewTwentyService(db)
	helperEnableWebhook(t, db, "correct-secret")

	payload := []byte(`{"type":"company.updated","record":{"id":"abc"}}`)
	err := svc.ApplyInboundWebhook(payload, "wrong-token")
	if !errors.Is(err, ErrInvalidWebhookToken) {
		t.Errorf("expected ErrInvalidWebhookToken, got %v", err)
	}
}

func TestTwentyService_ApplyInboundWebhook_DisabledSkipped(t *testing.T) {
	db := newTestDB(t)
	svc := NewTwentyService(db)
	// No config rows -> integration is disabled; should return nil silently.
	payload := []byte(`{"type":"company.updated","record":{"id":"abc"}}`)
	if err := svc.ApplyInboundWebhook(payload, "anything"); err != nil {
		t.Errorf("expected nil when disabled, got %v", err)
	}
}

func TestTwentyService_ApplyInboundWebhook_NoSecretConfigured(t *testing.T) {
	db := newTestDB(t)
	svc := NewTwentyService(db)
	// Enable without a webhook secret.
	db.Create(&models.AppSetting{Scope: "global", Key: TwentyEnabledKey, Value: "true"})
	db.Create(&models.AppSetting{Scope: "global", Key: TwentyAPIURLKey, Value: "https://twenty.example.com"})
	db.Create(&models.AppSetting{Scope: "global", Key: TwentyAPIKeyKey, Value: "test-key"})

	payload := []byte(`{"type":"company.updated","record":{"id":"abc"}}`)
	err := svc.ApplyInboundWebhook(payload, "")
	if !errors.Is(err, ErrWebhookBadRequest) {
		t.Errorf("expected ErrWebhookBadRequest, got %v", err)
	}
}

func TestTwentyService_ApplyInboundWebhook_NoMappingIgnored(t *testing.T) {
	db := newTestDB(t)
	svc := NewTwentyService(db)
	helperEnableWebhook(t, db, "test-secret")

	payload := []byte(`{"type":"company.updated","record":{"id":"unknown-id","name":"Acme"}}`)
	if err := svc.ApplyInboundWebhook(payload, "test-secret"); err != nil {
		t.Errorf("expected no error for unmapped record, got %v", err)
	}
}

func TestTwentyService_ApplyInboundWebhook_UpdatesCompany(t *testing.T) {
	db := newTestDB(t)
	db.AutoMigrate(&models.Customer{})
	svc := NewTwentyService(db)
	helperEnableWebhook(t, db, "test-secret")

	companyName := "Old Name"
	city := "Old City"
	customer := models.Customer{CompanyName: &companyName, City: &city}
	db.Create(&customer)

	twentyID := "twenty-company-001"
	mappingKey := fmt.Sprintf("twenty.company.%d", customer.CustomerID)
	db.Create(&models.AppSetting{Scope: "global", Key: mappingKey, Value: twentyID})

	addr, _ := json.Marshal(map[string]string{
		"addressStreet1": "New St 1",
		"addressCity":    "New City",
		"addressCountry": "SE",
	})
	record := map[string]json.RawMessage{
		"id":      []byte(`"` + twentyID + `"`),
		"name":    []byte(`"New Name"`),
		"address": addr,
	}
	body, _ := json.Marshal(map[string]interface{}{
		"type":   "company.updated",
		"record": record,
	})

	if err := svc.ApplyInboundWebhook(body, "test-secret"); err != nil {
		t.Fatalf("ApplyInboundWebhook: %v", err)
	}

	var updated models.Customer
	db.First(&updated, customer.CustomerID)

	if updated.CompanyName == nil || *updated.CompanyName != "New Name" {
		t.Errorf("CompanyName: got %v, want %q", updated.CompanyName, "New Name")
	}
	if updated.City == nil || *updated.City != "New City" {
		t.Errorf("City: got %v, want %q", updated.City, "New City")
	}
}

func TestTwentyService_ApplyInboundWebhook_EmptyFieldsNotOverwritten(t *testing.T) {
	db := newTestDB(t)
	db.AutoMigrate(&models.Customer{})
	svc := NewTwentyService(db)
	helperEnableWebhook(t, db, "test-secret")

	companyName := "Original Name"
	city := "Original City"
	customer := models.Customer{CompanyName: &companyName, City: &city}
	db.Create(&customer)

	twentyID := "twenty-company-002"
	mappingKey := fmt.Sprintf("twenty.company.%d", customer.CustomerID)
	db.Create(&models.AppSetting{Scope: "global", Key: mappingKey, Value: twentyID})

	// Empty address fields must NOT overwrite existing customer data.
	addr, _ := json.Marshal(map[string]string{
		"addressStreet1": "",
		"addressCity":    "",
	})
	record := map[string]json.RawMessage{
		"id":      []byte(`"` + twentyID + `"`),
		"name":    []byte(`"Updated Name"`),
		"address": addr,
	}
	body, _ := json.Marshal(map[string]interface{}{
		"type":   "company.updated",
		"record": record,
	})

	if err := svc.ApplyInboundWebhook(body, "test-secret"); err != nil {
		t.Fatalf("ApplyInboundWebhook: %v", err)
	}

	var updated models.Customer
	db.First(&updated, customer.CustomerID)

	if updated.CompanyName == nil || *updated.CompanyName != "Updated Name" {
		t.Errorf("CompanyName: got %v, want %q", updated.CompanyName, "Updated Name")
	}
	if updated.City == nil || *updated.City != "Original City" {
		t.Errorf("City should not be overwritten: got %v, want %q", updated.City, "Original City")
	}
}

func TestTwentyService_ApplyInboundWebhook_UpdatesPerson(t *testing.T) {
	db := newTestDB(t)
	db.AutoMigrate(&models.Customer{})
	svc := NewTwentyService(db)
	helperEnableWebhook(t, db, "test-secret")

	firstName := "Alice"
	lastName := "Smith"
	customer := models.Customer{FirstName: &firstName, LastName: &lastName}
	db.Create(&customer)

	twentyID := "twenty-person-001"
	mappingKey := fmt.Sprintf("twenty.person.%d", customer.CustomerID)
	db.Create(&models.AppSetting{Scope: "global", Key: mappingKey, Value: twentyID})

	emailsJSON, _ := json.Marshal(map[string]string{"primaryEmail": "alice@example.com"})
	phonesJSON, _ := json.Marshal(map[string]string{"primaryPhoneNumber": "+46701234567"})
	nameJSON, _ := json.Marshal(map[string]string{"firstName": "Alice", "lastName": "Updated"})
	record := map[string]json.RawMessage{
		"id":     []byte(`"` + twentyID + `"`),
		"name":   nameJSON,
		"emails": emailsJSON,
		"phones": phonesJSON,
	}
	body, _ := json.Marshal(map[string]interface{}{
		"type":   "person.updated",
		"record": record,
	})

	if err := svc.ApplyInboundWebhook(body, "test-secret"); err != nil {
		t.Fatalf("ApplyInboundWebhook: %v", err)
	}

	var updated models.Customer
	db.First(&updated, customer.CustomerID)

	if updated.LastName == nil || *updated.LastName != "Updated" {
		t.Errorf("LastName: got %v, want %q", updated.LastName, "Updated")
	}
	if updated.Email == nil || *updated.Email != "alice@example.com" {
		t.Errorf("Email: got %v, want %q", updated.Email, "alice@example.com")
	}
}

func TestTwentyService_ApplyInboundWebhook_UnknownType(t *testing.T) {
	db := newTestDB(t)
	svc := NewTwentyService(db)
	helperEnableWebhook(t, db, "test-secret")

	payload := []byte(`{"type":"opportunity.updated","record":{"id":"abc"}}`)
	if err := svc.ApplyInboundWebhook(payload, "test-secret"); err != nil {
		t.Errorf("expected no error for unsupported type, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// amountMicros rounding tests
// ---------------------------------------------------------------------------

func TestAmountMicros_Rounding(t *testing.T) {
	cases := []struct {
		amount float64
		want   int64
	}{
		{1.0, 1_000_000},
		{1.234567, 1_234_567},
		{0.999999, 999_999},
		{0.9999995, 1_000_000},
	}
	for _, tc := range cases {
		if got := amountMicros(tc.amount); got != tc.want {
			t.Errorf("amountMicros(%v): got %d, want %d", tc.amount, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// storedID / reverseCustomerIDLookup tests
// ---------------------------------------------------------------------------

func TestStoredID_ErrRecordNotFound(t *testing.T) {
	db := newTestDB(t)
	svc := NewTwentyService(db)

	id, err := svc.storedID("non.existent.key")
	if err != nil {
		t.Errorf("expected nil error for missing key, got %v", err)
	}
	if id != "" {
		t.Errorf("expected empty id for missing key, got %q", id)
	}
}

func TestReverseCustomerIDLookup(t *testing.T) {
	db := newTestDB(t)
	svc := NewTwentyService(db)

	db.Create(&models.AppSetting{Scope: "global", Key: "twenty.company.42", Value: "twenty-abc"})

	id, err := svc.reverseCustomerIDLookup("twenty-abc", "company")
	if err != nil {
		t.Fatalf("reverseCustomerIDLookup: %v", err)
	}
	if id != 42 {
		t.Errorf("expected customerID=42, got %d", id)
	}

	id2, err := svc.reverseCustomerIDLookup("unknown-id", "company")
	if err != nil {
		t.Fatalf("reverseCustomerIDLookup (missing): %v", err)
	}
	if id2 != 0 {
		t.Errorf("expected customerID=0 for unknown, got %d", id2)
	}
}

// ---------------------------------------------------------------------------
// Helper / pure-function tests
// ---------------------------------------------------------------------------

func TestMapJobStatusToStage(t *testing.T) {
	cases := []struct {
		status string
		want   string
	}{
		{"New", "NEW"},
		{"Open", "NEW"},
		{"In Progress", "IN_PROGRESS"},
		{"Active", "IN_PROGRESS"},
		{"Completed", "CLOSED_WON"},
		{"Done", "CLOSED_WON"},
		{"Won", "CLOSED_WON"},
		{"Lost", "CLOSED_LOST"},
		{"Cancelled", "CLOSED_LOST"},
		{"something else", "NEW"},
	}
	for _, tc := range cases {
		if got := mapJobStatusToStage(tc.status); got != tc.want {
			t.Errorf("mapJobStatusToStage(%q): got %q, want %q", tc.status, got, tc.want)
		}
	}
}

func TestExtractID(t *testing.T) {
	raw := map[string]json.RawMessage{
		"createCompany": []byte(`{"id":"abc-123","name":"Acme"}`),
	}
	id := extractID(raw, "createCompany")
	if id != "abc-123" {
		t.Errorf("extractID: got %q, want %q", id, "abc-123")
	}

	id2 := extractID(raw, "notPresent")
	if id2 != "" {
		t.Errorf("extractID missing key: got %q, want empty", id2)
	}
}

func TestBuildAddress(t *testing.T) {
	street := "Main St"
	house := "12"
	city := "Berlin"
	state := "BE"
	zip := "10115"
	country := "Germany"

	c := &models.Customer{
		Street:       &street,
		HouseNumber:  &house,
		City:         &city,
		FederalState: &state,
		ZIP:          &zip,
		Country:      &country,
	}

	addr := buildAddress(c)
	if addr["addressStreet1"] != "Main St 12" {
		t.Errorf("street1: got %q, want %q", addr["addressStreet1"], "Main St 12")
	}
	if addr["addressCity"] != "Berlin" {
		t.Errorf("city: got %q", addr["addressCity"])
	}
	if addr["addressCountry"] != "Germany" {
		t.Errorf("country: got %q", addr["addressCountry"])
	}
}

func TestTwentyService_TestConnection_NoConfig(t *testing.T) {
	db := newTestDB(t)
	svc := NewTwentyService(db)

	err := svc.TestConnection()
	if err == nil {
		t.Error("expected error when no config set")
	}
}
