package services

import (
	"testing"
	"time"

	"go-barcode-webapp/internal/models"
)

// newTestJobHistoryService creates a JobHistoryService without a database connection,
// suitable for testing pure change-detection logic.
func newTestJobHistoryService() *JobHistoryService {
	return &JobHistoryService{db: nil}
}

func TestDetectChangesNoChanges(t *testing.T) {
	s := newTestJobHistoryService()

	job := &models.Job{
		JobID:        1,
		CustomerID:   10,
		StatusID:     2,
		Discount:     0,
		DiscountType: "amount",
	}

	changes := s.detectChanges(job, job)
	if len(changes) != 0 {
		t.Errorf("detectChanges() = %d changes, want 0 for identical jobs", len(changes))
	}
}

func TestDetectChangesCustomerIDChanged(t *testing.T) {
	s := newTestJobHistoryService()

	old := &models.Job{CustomerID: 1, StatusID: 1, DiscountType: "amount"}
	new := &models.Job{CustomerID: 2, StatusID: 1, DiscountType: "amount"}

	changes := s.detectChanges(old, new)
	if len(changes) != 1 {
		t.Fatalf("detectChanges() = %d changes, want 1", len(changes))
	}
	if changes[0].Field != "customerID" {
		t.Errorf("change field = %q, want %q", changes[0].Field, "customerID")
	}
	if changes[0].OldValue != "1" || changes[0].NewValue != "2" {
		t.Errorf("change values = (%q, %q), want (\"1\", \"2\")", changes[0].OldValue, changes[0].NewValue)
	}
}

func TestDetectChangesStatusIDChanged(t *testing.T) {
	s := newTestJobHistoryService()

	old := &models.Job{CustomerID: 1, StatusID: 1, DiscountType: "amount"}
	new := &models.Job{CustomerID: 1, StatusID: 3, DiscountType: "amount"}

	changes := s.detectChanges(old, new)
	if len(changes) != 1 {
		t.Fatalf("detectChanges() = %d changes, want 1", len(changes))
	}
	if changes[0].Field != "statusID" {
		t.Errorf("change field = %q, want %q", changes[0].Field, "statusID")
	}
}

func TestDetectChangesDiscountChanged(t *testing.T) {
	s := newTestJobHistoryService()

	old := &models.Job{CustomerID: 1, StatusID: 1, Discount: 0, DiscountType: "amount"}
	new := &models.Job{CustomerID: 1, StatusID: 1, Discount: 10.5, DiscountType: "amount"}

	changes := s.detectChanges(old, new)

	found := false
	for _, c := range changes {
		if c.Field == "discount" {
			found = true
			if c.OldValue != "0.00" || c.NewValue != "10.50" {
				t.Errorf("discount change values = (%q, %q), want (\"0.00\", \"10.50\")", c.OldValue, c.NewValue)
			}
		}
	}
	if !found {
		t.Error("detectChanges() did not report discount change")
	}
}

func TestDetectChangesDiscountTypeChanged(t *testing.T) {
	s := newTestJobHistoryService()

	old := &models.Job{CustomerID: 1, StatusID: 1, DiscountType: "amount"}
	new := &models.Job{CustomerID: 1, StatusID: 1, DiscountType: "percent"}

	changes := s.detectChanges(old, new)

	found := false
	for _, c := range changes {
		if c.Field == "discount_type" {
			found = true
			if c.OldValue != "amount" || c.NewValue != "percent" {
				t.Errorf("discount_type change = (%q, %q), want (\"amount\", \"percent\")", c.OldValue, c.NewValue)
			}
		}
	}
	if !found {
		t.Error("detectChanges() did not report discount_type change")
	}
}

func TestDetectChangesDescriptionChanged(t *testing.T) {
	s := newTestJobHistoryService()

	desc1 := "Old description"
	desc2 := "New description"

	old := &models.Job{CustomerID: 1, StatusID: 1, DiscountType: "amount", Description: &desc1}
	new := &models.Job{CustomerID: 1, StatusID: 1, DiscountType: "amount", Description: &desc2}

	changes := s.detectChanges(old, new)

	found := false
	for _, c := range changes {
		if c.Field == "description" {
			found = true
			if c.OldValue != desc1 || c.NewValue != desc2 {
				t.Errorf("description change = (%q, %q), want (%q, %q)", c.OldValue, c.NewValue, desc1, desc2)
			}
		}
	}
	if !found {
		t.Error("detectChanges() did not report description change")
	}
}

func TestDetectChangesDateChanged(t *testing.T) {
	s := newTestJobHistoryService()

	date1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	date2 := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)

	old := &models.Job{CustomerID: 1, StatusID: 1, DiscountType: "amount", StartDate: &date1}
	new := &models.Job{CustomerID: 1, StatusID: 1, DiscountType: "amount", StartDate: &date2}

	changes := s.detectChanges(old, new)

	found := false
	for _, c := range changes {
		if c.Field == "startDate" {
			found = true
			if c.OldValue != "2024-01-01" || c.NewValue != "2024-06-15" {
				t.Errorf("startDate change = (%q, %q), want (\"2024-01-01\", \"2024-06-15\")", c.OldValue, c.NewValue)
			}
		}
	}
	if !found {
		t.Error("detectChanges() did not report startDate change")
	}
}

func TestCompareNullableUint(t *testing.T) {
	s := newTestJobHistoryService()

	v1 := uint(1)
	v2 := uint(2)

	tests := []struct {
		name      string
		old, new  *uint
		field     string
		wantCount int
		wantOld   string
		wantNew   string
	}{
		{"both nil no change", nil, nil, "f", 0, "", ""},
		{"old nil new set", nil, &v1, "f", 1, "null", "1"},
		{"old set new nil", &v1, nil, "f", 1, "1", "null"},
		{"both same no change", &v1, &v1, "f", 0, "", ""},
		{"both different", &v1, &v2, "f", 1, "1", "2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changes := s.compareNullableUint(tt.old, tt.new, tt.field)
			if len(changes) != tt.wantCount {
				t.Fatalf("compareNullableUint() = %d changes, want %d", len(changes), tt.wantCount)
			}
			if tt.wantCount > 0 {
				if changes[0].OldValue != tt.wantOld || changes[0].NewValue != tt.wantNew {
					t.Errorf("compareNullableUint() values = (%q, %q), want (%q, %q)",
						changes[0].OldValue, changes[0].NewValue, tt.wantOld, tt.wantNew)
				}
			}
		})
	}
}

func TestCompareNullableString(t *testing.T) {
	s := newTestJobHistoryService()

	hello := "hello"
	world := "world"

	tests := []struct {
		name      string
		old, new  *string
		wantCount int
		wantOld   string
		wantNew   string
	}{
		{"both nil no change", nil, nil, 0, "", ""},
		{"old nil new set", nil, &hello, 1, "null", "hello"},
		{"old set new nil", &hello, nil, 1, "hello", "null"},
		{"both same no change", &hello, &hello, 0, "", ""},
		{"both different", &hello, &world, 1, "hello", "world"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changes := s.compareNullableString(tt.old, tt.new, "field")
			if len(changes) != tt.wantCount {
				t.Fatalf("compareNullableString() = %d changes, want %d", len(changes), tt.wantCount)
			}
			if tt.wantCount > 0 {
				if changes[0].OldValue != tt.wantOld || changes[0].NewValue != tt.wantNew {
					t.Errorf("compareNullableString() values = (%q, %q), want (%q, %q)",
						changes[0].OldValue, changes[0].NewValue, tt.wantOld, tt.wantNew)
				}
			}
		})
	}
}

func TestCompareNullableTime(t *testing.T) {
	s := newTestJobHistoryService()

	t1 := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 3, 20, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		old, new  *time.Time
		wantCount int
		wantOld   string
		wantNew   string
	}{
		{"both nil no change", nil, nil, 0, "", ""},
		{"old nil new set", nil, &t1, 1, "null", "2024-01-15"},
		{"old set new nil", &t1, nil, 1, "2024-01-15", "null"},
		{"both same no change", &t1, &t1, 0, "", ""},
		{"both different", &t1, &t2, 1, "2024-01-15", "2024-03-20"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changes := s.compareNullableTime(tt.old, tt.new, "field")
			if len(changes) != tt.wantCount {
				t.Fatalf("compareNullableTime() = %d changes, want %d", len(changes), tt.wantCount)
			}
			if tt.wantCount > 0 {
				if changes[0].OldValue != tt.wantOld || changes[0].NewValue != tt.wantNew {
					t.Errorf("compareNullableTime() values = (%q, %q), want (%q, %q)",
						changes[0].OldValue, changes[0].NewValue, tt.wantOld, tt.wantNew)
				}
			}
		})
	}
}
