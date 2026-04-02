package pdf

import (
	"testing"
)

func TestFindAmountToken(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantToken string
		wantOk    bool
	}{
		{
			name:      "integer amount",
			line:      "Total 1234",
			wantToken: "1234",
			wantOk:    true,
		},
		{
			name:      "decimal amount European format",
			line:      "Gesamt 1.234,56",
			wantToken: "1.234,56",
			wantOk:    true,
		},
		{
			name:      "decimal amount dot separator",
			line:      "Total 99.99",
			wantToken: "99.99",
			wantOk:    true,
		},
		{
			name:   "percentage is skipped",
			line:   "10%",
			wantOk: false,
		},
		{
			name:   "empty line",
			line:   "",
			wantOk: false,
		},
		{
			name:   "text only no numbers",
			line:   "Hello World",
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := findAmountToken(tt.line)
			if ok != tt.wantOk {
				t.Errorf("findAmountToken(%q) ok = %v, want %v", tt.line, ok, tt.wantOk)
			}
			if ok && got != tt.wantToken {
				t.Errorf("findAmountToken(%q) token = %q, want %q", tt.line, got, tt.wantToken)
			}
		})
	}
}

func TestFindDecimalAmountToken(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantToken string
		wantOk    bool
	}{
		{
			name:      "European decimal",
			line:      "Gesamt 1.234,56",
			wantToken: "1.234,56",
			wantOk:    true,
		},
		{
			name:      "dot decimal",
			line:      "Total 99.99",
			wantToken: "99.99",
			wantOk:    true,
		},
		{
			name:   "integer only — no decimal match",
			line:   "Count 42",
			wantOk: false,
		},
		{
			name:   "empty line",
			line:   "",
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := findDecimalAmountToken(tt.line)
			if ok != tt.wantOk {
				t.Errorf("findDecimalAmountToken(%q) ok = %v, want %v", tt.line, ok, tt.wantOk)
			}
			if ok && got != tt.wantToken {
				t.Errorf("findDecimalAmountToken(%q) token = %q, want %q", tt.line, got, tt.wantToken)
			}
		})
	}
}

func TestFindPercentage(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantPct float64
		wantOk  bool
	}{
		{
			name:    "integer percent",
			line:    "Rabatt 10%",
			wantPct: 10.0,
			wantOk:  true,
		},
		{
			name:    "decimal percent",
			line:    "Discount 5,5%",
			wantPct: 5.5,
			wantOk:  true,
		},
		{
			name:    "decimal percent with dot",
			line:    "Discount 7.25%",
			wantPct: 7.25,
			wantOk:  true,
		},
		{
			name:   "no percentage",
			line:   "Total 100",
			wantOk: false,
		},
		{
			name:   "empty line",
			line:   "",
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := findPercentage(tt.line)
			if ok != tt.wantOk {
				t.Errorf("findPercentage(%q) ok = %v, want %v", tt.line, ok, tt.wantOk)
			}
			if ok && got != tt.wantPct {
				t.Errorf("findPercentage(%q) = %v, want %v", tt.line, got, tt.wantPct)
			}
		})
	}
}
