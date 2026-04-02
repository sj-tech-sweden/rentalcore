package pdf

import (
	"testing"

	"go-barcode-webapp/internal/models"
)

func TestNormalizeProductText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"lowercase conversion", "CAMERA PRO", "camera pro"},
		{"removes special chars", "Camera-Pro 200!", "camera pro 200"},
		{"trims whitespace", "  camera  pro  ", "camera pro"},
		{"keeps digits", "Lens 50mm", "lens 50mm"},
		{"empty string", "", ""},
		{"only special chars", "!!!---", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeProductText(tt.input)
			if got != tt.want {
				t.Errorf("normalizeProductText(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCalculateSimilarity(t *testing.T) {
	tests := []struct {
		name    string
		s1, s2  string
		wantMin float64
		wantMax float64
	}{
		{"identical strings", "camera", "camera", 100.0, 100.0},
		{"empty strings", "", "", 100.0, 100.0},
		{"one empty", "camera", "", 0.0, 0.0},
		{"completely different", "abc", "xyz", 0.0, 50.0},
		{"one contains the other", "camera", "cam", 50.0, 100.0},
		{"high similarity", "camera", "camara", 50.0, 100.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateSimilarity(tt.s1, tt.s2)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("calculateSimilarity(%q, %q) = %v, want [%v, %v]", tt.s1, tt.s2, got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestNormalizeCustomerText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"lowercase", "Mustermann GmbH", "mustermann"},
		{"strips legal suffix gmbh", "Acme GmbH", "acme"},
		{"strips ltd", "Tech Ltd", "tech"},
		{"strips ag", "Big AG", "big"},
		{"mixed case and special", "Test & Partner KG", "test partner"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeCustomerText(tt.input)
			if got != tt.want {
				t.Errorf("normalizeCustomerText(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestShouldSkipCustomerToken(t *testing.T) {
	skipTokens := []string{"gmbh", "mbh", "ug", "kg", "ag", "ltd", "inc", "co", "und", "&", "eventtechnik", "events", "event", "verleih"}
	keepTokens := []string{"acme", "tech", "solutions", "john", "doe", "123"}

	for _, token := range skipTokens {
		t.Run("skip_"+token, func(t *testing.T) {
			if !shouldSkipCustomerToken(token) {
				t.Errorf("shouldSkipCustomerToken(%q) = false, want true", token)
			}
		})
	}

	for _, token := range keepTokens {
		t.Run("keep_"+token, func(t *testing.T) {
			if shouldSkipCustomerToken(token) {
				t.Errorf("shouldSkipCustomerToken(%q) = true, want false", token)
			}
		})
	}
}

func TestBuildCustomerFullName(t *testing.T) {
	first := "Jane"
	last := "Doe"
	empty := ""

	tests := []struct {
		name     string
		customer *models.Customer
		want     string
	}{
		{
			name:     "first and last",
			customer: &models.Customer{FirstName: &first, LastName: &last},
			want:     "Jane Doe",
		},
		{
			name:     "last name only",
			customer: &models.Customer{LastName: &last},
			want:     "Doe",
		},
		{
			name:     "first name only",
			customer: &models.Customer{FirstName: &first},
			want:     "Jane",
		},
		{
			name:     "empty names",
			customer: &models.Customer{FirstName: &empty, LastName: &empty},
			want:     "",
		},
		{
			name:     "nil customer",
			customer: nil,
			want:     "",
		},
		{
			name:     "no names set",
			customer: &models.Customer{},
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCustomerFullName(tt.customer)
			if got != tt.want {
				t.Errorf("buildCustomerFullName() = %q, want %q", got, tt.want)
			}
		})
	}
}
