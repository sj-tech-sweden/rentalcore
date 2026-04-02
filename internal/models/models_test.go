package models

import (
	"testing"
)

func TestCustomerGetDisplayName(t *testing.T) {
	company := "Acme Corp"
	firstName := "John"
	lastName := "Doe"
	empty := ""

	tests := []struct {
		name     string
		customer Customer
		want     string
	}{
		{
			name:     "company name takes precedence",
			customer: Customer{CompanyName: &company, FirstName: &firstName, LastName: &lastName},
			want:     "Acme Corp",
		},
		{
			name:     "first and last name when no company",
			customer: Customer{FirstName: &firstName, LastName: &lastName},
			want:     "John Doe",
		},
		{
			name:     "last name only when no first name",
			customer: Customer{LastName: &lastName},
			want:     "Doe",
		},
		{
			name:     "first name only when no last name",
			customer: Customer{FirstName: &firstName},
			want:     "John",
		},
		{
			name:     "empty company falls back to full name",
			customer: Customer{CompanyName: &empty, FirstName: &firstName, LastName: &lastName},
			want:     "John Doe",
		},
		{
			name:     "unknown customer when all nil",
			customer: Customer{},
			want:     "Unknown Customer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.customer.GetDisplayName()
			if got != tt.want {
				t.Errorf("GetDisplayName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUserGetDisplayName(t *testing.T) {
	tests := []struct {
		name string
		user User
		want string
	}{
		{
			name: "full name when both first and last are set",
			user: User{FirstName: "Jane", LastName: "Smith", Username: "jsmith"},
			want: "Jane Smith",
		},
		{
			name: "last name only",
			user: User{LastName: "Smith", Username: "jsmith"},
			want: "Smith",
		},
		{
			name: "first name only",
			user: User{FirstName: "Jane", Username: "jsmith"},
			want: "Jane",
		},
		{
			name: "falls back to username when no name set",
			user: User{Username: "jsmith"},
			want: "jsmith",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.user.GetDisplayName()
			if got != tt.want {
				t.Errorf("GetDisplayName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatFieldName(t *testing.T) {
	tests := []struct {
		field string
		want  string
	}{
		{"customerID", "Customer"},
		{"statusID", "Status"},
		{"jobcategoryID", "Job Category"},
		{"description", "Description"},
		{"discount", "Discount"},
		{"discount_type", "Discount Type"},
		{"revenue", "Revenue"},
		{"final_revenue", "Final Revenue"},
		{"startDate", "Start Date"},
		{"endDate", "End Date"},
		{"priority", "Priority"},
		{"internal_notes", "Internal Notes"},
		{"customer_notes", "Customer Notes"},
		{"unknownField", "unknownField"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			got := FormatFieldName(tt.field)
			if got != tt.want {
				t.Errorf("FormatFieldName(%q) = %q, want %q", tt.field, got, tt.want)
			}
		})
	}
}
