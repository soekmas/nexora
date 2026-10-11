package crm

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		wantErr  bool
	}{
		{"user@example.com", "user@example.com", false},
		{"USER@EXAMPLE.COM", "user@example.com", false},
		{"  john.doe+test@gmail.com  ", "john.doe+test@gmail.com", false},
		{"", "", false},
		{"invalid-email", "", true},
		{"@missing-local.com", "", true},
		{"missing-domain@", "", true},
	}

	for _, tt := range tests {
		got, err := ValidateEmail(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateEmail(%q) err = %v, wantErr = %v", tt.input, err, tt.wantErr)
		}
		if got != tt.expected {
			t.Errorf("ValidateEmail(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestValidatePhone(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		wantErr  bool
	}{
		{"+6281234567890", "+6281234567890", false},
		{"0812-3456-7890", "081234567890", false},
		{" (021) 555-1234 ", "0215551234", false},
		{"", "", false},
		{"123", "", true}, // too short
		{"phone1234567", "", true},
	}

	for _, tt := range tests {
		got, err := ValidatePhone(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidatePhone(%q) err = %v, wantErr = %v", tt.input, err, tt.wantErr)
		}
		if got != tt.expected {
			t.Errorf("ValidatePhone(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestValidateCustomerNumber(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"CUST-001", false},
		{"CUST_VIP_99", false},
		{"12345678", false},
		{"CUST-20261011-AB12CD", false},
		{"ab", true},        // too short (< 3)
		{"cust#123", true},   // invalid char
		{"cust@domain", true},
	}

	for _, tt := range tests {
		err := ValidateCustomerNumber(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateCustomerNumber(%q) err = %v, wantErr = %v", tt.input, err, tt.wantErr)
		}
	}
}

func TestValidateGroupCode(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"RETAIL", false},
		{"WHOLESALE-VIP", false},
		{"VIP_GOLD_01", false},
		{"A", true}, // too short (< 2)
		{"GRP@!", true},
	}

	for _, tt := range tests {
		err := ValidateGroupCode(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateGroupCode(%q) err = %v, wantErr = %v", tt.input, err, tt.wantErr)
		}
	}
}

func TestPIIMasking(t *testing.T) {
	// Email masking
	if got := MaskEmail("john.doe@example.com"); got != "j***@example.com" {
		t.Errorf("MaskEmail() = %q, want j***@example.com", got)
	}
	if got := MaskEmail("a@b.com"); got != "*@b.com" {
		t.Errorf("MaskEmail(short) = %q, want *@b.com", got)
	}
	if got := MaskEmail("invalid"); got != "***" {
		t.Errorf("MaskEmail(invalid) = %q, want ***", got)
	}

	// Phone masking
	if got := MaskPhone("+6281234567890"); got != "+62****890" {
		t.Errorf("MaskPhone() = %q, want +62****890", got)
	}
	if got := MaskPhone("123"); got != "***" {
		t.Errorf("MaskPhone(short) = %q, want ***", got)
	}

	// Tax ID masking
	if got := MaskTaxID("01.234.567.8-999.000"); got != "****************.000" {
		t.Errorf("MaskTaxID() = %q", got)
	}
	if got := MaskTaxID("1234"); got != "****" {
		t.Errorf("MaskTaxID(short) = %q", got)
	}
}

func TestCustomerToResponse(t *testing.T) {
	now := time.Now().UTC()
	c := &Customer{
		ID:         uuid.New(),
		CustomerNo: "CUST-001",
		Name:       "Test Customer",
		Status:     StatusActive,
		Version:    1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	resp := c.ToResponse()
	if resp.ID != c.ID || resp.CustomerNo != c.CustomerNo || resp.Name != c.Name {
		t.Errorf("ToResponse mapping mismatch: %+v", resp)
	}
	if !resp.Active {
		t.Errorf("expected Active = true for StatusActive")
	}

	c.Status = StatusInactive
	respInactive := c.ToResponse()
	if respInactive.Active {
		t.Errorf("expected Active = false for StatusInactive")
	}
}

func TestCustomerAddressToResponse(t *testing.T) {
	a := &CustomerAddress{
		ID:          uuid.New(),
		CustomerID:  uuid.New(),
		AddressType: AddressTypeShipping,
		AddressLine: "123 Main St",
		CountryCode: "ID",
		IsDefault:   true,
	}

	resp := a.ToResponse()
	if resp.ID != a.ID || resp.Label != AddressTypeShipping || resp.AddressLine1 != "123 Main St" || !resp.IsDefault {
		t.Errorf("Address ToResponse mapping mismatch: %+v", resp)
	}
}
