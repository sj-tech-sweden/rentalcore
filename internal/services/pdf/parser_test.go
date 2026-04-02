package pdf

import (
	"testing"
	"time"
)

func TestDetectDocumentType(t *testing.T) {
	p := NewIntelligentParser()

	tests := []struct {
		name string
		text string
		want ParsedDocumentType
	}{
		{"invoice German", "Rechnung Nr. 2024-001", DocTypeInvoice},
		{"invoice English", "Invoice #1234", DocTypeInvoice},
		{"offer German", "Angebot 2024-05", DocTypeOffer},
		{"offer English", "Quotation for services", DocTypeOffer},
		{"order German", "Bestellung vom 01.01.2024", DocTypeOrder},
		{"delivery note", "Lieferschein 99", DocTypeDelivery},
		{"unknown", "Just some text without keywords", DocTypeUnknown},
		{"empty", "", DocTypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.detectDocumentType(tt.text)
			if got != tt.want {
				t.Errorf("detectDocumentType(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestExtractCustomerName(t *testing.T) {
	p := NewIntelligentParser()

	tests := []struct {
		name  string
		lines []string
		want  string
	}{
		{
			name:  "extracts name after 'Kunde:'",
			lines: []string{"Kunde: Mustermann GmbH"},
			want:  "Mustermann GmbH",
		},
		{
			name:  "extracts name after 'Customer:'",
			lines: []string{"Customer: John Doe"},
			want:  "John Doe",
		},
		{
			name:  "extracts company name",
			lines: []string{"Firma: Tech Solutions AG"},
			want:  "Tech Solutions AG",
		},
		{
			name:  "no customer info returns empty",
			lines: []string{"Total: 100.00", "Date: 01.01.2024"},
			want:  "",
		},
		{
			name:  "empty lines returns empty",
			lines: []string{},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.extractCustomerName(tt.lines)
			if got != tt.want {
				t.Errorf("extractCustomerName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractDocumentNumber(t *testing.T) {
	p := NewIntelligentParser()

	tests := []struct {
		name  string
		lines []string
		want  string
	}{
		{
			name:  "invoice number German",
			lines: []string{"Rechnung Nr. 2024-001"},
			want:  "2024-001",
		},
		{
			name:  "invoice number English",
			lines: []string{"Invoice #INV-9999"},
			want:  "INV-9999",
		},
		{
			name:  "number with Nr.",
			lines: []string{"Nr. ABC123"},
			want:  "ABC123",
		},
		{
			name:  "no document number",
			lines: []string{"Some text", "More text"},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.extractDocumentNumber(tt.lines)
			if got != tt.want {
				t.Errorf("extractDocumentNumber() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractDocumentDate(t *testing.T) {
	p := NewIntelligentParser()

	tests := []struct {
		name  string
		lines []string
		want  time.Time
	}{
		{
			name:  "date with label German dot format",
			lines: []string{"Datum: 15.03.2024"},
			want:  time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "date with label English",
			lines: []string{"Date: 25/12/2023"},
			want:  time.Date(2023, 12, 25, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "bare date",
			lines: []string{"01.06.2025"},
			want:  time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "no date returns zero value",
			lines: []string{"Hello world"},
			want:  time.Time{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.extractDocumentDate(tt.lines)
			if !got.Equal(tt.want) {
				t.Errorf("extractDocumentDate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractTotalAmount(t *testing.T) {
	p := NewIntelligentParser()

	tests := []struct {
		name  string
		lines []string
		want  float64
	}{
		{
			name:  "gesamt in German",
			lines: []string{"Gesamt: 1234.56"},
			want:  1234.56,
		},
		{
			name:  "total in English",
			lines: []string{"Total: 99.00"},
			want:  99.00,
		},
		{
			name:  "returns largest amount",
			lines: []string{"Summe: 100.00", "Gesamtbetrag: 500.00"},
			want:  500.00,
		},
		{
			name:  "no total returns zero",
			lines: []string{"No amounts here"},
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.extractTotalAmount(tt.lines)
			if got != tt.want {
				t.Errorf("extractTotalAmount() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseAmount(t *testing.T) {
	p := NewIntelligentParser()

	tests := []struct {
		name  string
		input string
		want  float64
	}{
		{"simple integer", "100", 100.0},
		{"dot decimal", "99.99", 99.99},
		{"European format comma decimal", "1.234,56", 1234.56},
		{"with euro symbol", "€ 50.00", 50.00},
		{"with spaces", "1 000", 1000.0},
		{"negative value", "-25.00", -25.0},
		{"empty returns zero", "", 0.0},
		{"non-numeric returns zero", "abc", 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.parseAmount(tt.input)
			if got != tt.want {
				t.Errorf("parseAmount(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseDocumentDetectsType(t *testing.T) {
	p := NewIntelligentParser()

	doc, err := p.ParseDocument("Rechnung Nr. 2024-001\nKunde: Mustermann GmbH\nGesamt: 500.00\nDatum: 01.01.2024")
	if err != nil {
		t.Fatalf("ParseDocument() unexpected error: %v", err)
	}
	if doc.DocumentType != DocTypeInvoice {
		t.Errorf("ParseDocument() DocumentType = %q, want %q", doc.DocumentType, DocTypeInvoice)
	}
	if doc.DocumentNumber != "2024-001" {
		t.Errorf("ParseDocument() DocumentNumber = %q, want %q", doc.DocumentNumber, "2024-001")
	}
	if doc.TotalAmount != 500.00 {
		t.Errorf("ParseDocument() TotalAmount = %v, want %v", doc.TotalAmount, 500.00)
	}
}

func TestParseDocumentUnknownType(t *testing.T) {
	p := NewIntelligentParser()

	doc, err := p.ParseDocument("Some random text without document keywords")
	if err != nil {
		t.Fatalf("ParseDocument() unexpected error: %v", err)
	}
	if doc.DocumentType != DocTypeUnknown {
		t.Errorf("ParseDocument() DocumentType = %q, want %q", doc.DocumentType, DocTypeUnknown)
	}
}

func TestParseDocumentEmptyInput(t *testing.T) {
	p := NewIntelligentParser()

	doc, err := p.ParseDocument("")
	if err != nil {
		t.Fatalf("ParseDocument() unexpected error: %v", err)
	}
	if doc == nil {
		t.Fatal("ParseDocument() returned nil document")
	}
	if doc.DocumentType != DocTypeUnknown {
		t.Errorf("ParseDocument() DocumentType = %q, want %q", doc.DocumentType, DocTypeUnknown)
	}
}
