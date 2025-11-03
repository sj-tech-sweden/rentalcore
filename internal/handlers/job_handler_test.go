package handlers

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestNormalizeProductSelections(t *testing.T) {
	input := []JobProductSelection{
		{ProductID: 1, Quantity: 2},
		{ProductID: 1, Quantity: 3},
		{ProductID: 0, Quantity: 5}, // invalid product id
		{ProductID: 2, Quantity: 0}, // zero quantity
	}

	got := normalizeProductSelections(input)
	want := []JobProductSelection{
		{ProductID: 1, Quantity: 5},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeProductSelections() = %#v, want %#v", got, want)
	}
}

func TestParseProductSelectionsFromString(t *testing.T) {
	payload := `[{"product_id":1,"quantity":2},{"product_id":1,"quantity":1},{"product_id":3,"quantity":0}]`

	got, err := parseProductSelectionsFromString(payload)
	if err != nil {
		t.Fatalf("parseProductSelectionsFromString() unexpected error: %v", err)
	}

	want := []JobProductSelection{
		{ProductID: 1, Quantity: 3},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseProductSelectionsFromString() = %#v, want %#v", got, want)
	}
}

func TestParseProductSelectionsFromInterfaceSlice(t *testing.T) {
	raw := []interface{}{
		map[string]interface{}{"product_id": 2.0, "quantity": 4.0},
		map[string]interface{}{"product_id": 2.0, "quantity": 1.0},
		map[string]interface{}{"product_id": 3.0, "quantity": 0.0}, // zero quantity should be ignored
	}

	got, err := parseProductSelectionsFromInterface(raw)
	if err != nil {
		t.Fatalf("parseProductSelectionsFromInterface() unexpected error: %v", err)
	}

	want := []JobProductSelection{
		{ProductID: 2, Quantity: 5},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseProductSelectionsFromInterface() = %#v, want %#v", got, want)
	}
}

func TestParseProductSelectionsFromInterfaceInvalid(t *testing.T) {
	_, err := parseProductSelectionsFromInterface(123)
	if err == nil {
		t.Fatal("parseProductSelectionsFromInterface() expected error for unsupported type, got nil")
	}
}

func TestParseProductSelectionsFromInterfaceJSONString(t *testing.T) {
	rawSelections := []JobProductSelection{
		{ProductID: 4, Quantity: 2},
		{ProductID: 4, Quantity: 3},
	}
	encoded, err := json.Marshal(rawSelections)
	if err != nil {
		t.Fatalf("failed to marshal selections: %v", err)
	}

	got, err := parseProductSelectionsFromInterface(string(encoded))
	if err != nil {
		t.Fatalf("parseProductSelectionsFromInterface() unexpected error: %v", err)
	}

	want := []JobProductSelection{
		{ProductID: 4, Quantity: 5},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseProductSelectionsFromInterface() = %#v, want %#v", got, want)
	}
}
