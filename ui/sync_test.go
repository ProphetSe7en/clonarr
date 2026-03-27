package main

import (
	"encoding/json"
	"testing"
)

func TestCustomCFToArr_ConvertsObjectFields(t *testing.T) {
	// TRaSH format: {"value": "regex"}
	cf := &CustomCF{
		Name:    "Test CF",
		AppType: "radarr",
		Specifications: []ArrSpecification{
			{
				Name:           "spec1",
				Implementation: "ReleaseTitleSpecification",
				Fields:         json.RawMessage(`{"value":"test\\.regex"}`),
			},
		},
	}

	arr := customCFToArr(cf)

	if arr.Name != "Test CF" {
		t.Fatalf("Name = %q, want Test CF", arr.Name)
	}
	if len(arr.Specifications) != 1 {
		t.Fatalf("specs = %d, want 1", len(arr.Specifications))
	}

	// Fields should be converted to array format
	var fields []map[string]any
	if err := json.Unmarshal(arr.Specifications[0].Fields, &fields); err != nil {
		t.Fatalf("fields not array format: %v", err)
	}
	if len(fields) != 1 {
		t.Fatalf("fields length = %d, want 1", len(fields))
	}
	if fields[0]["name"] != "value" {
		t.Fatalf("field name = %v, want value", fields[0]["name"])
	}
	if fields[0]["value"] != "test\\.regex" {
		t.Fatalf("field value = %v, want test\\.regex", fields[0]["value"])
	}
}

func TestCustomCFToArr_PassthroughArrayFields(t *testing.T) {
	// Already in Arr format: [{"name":"value","value":"x"}]
	cf := &CustomCF{
		Name:    "Test CF",
		AppType: "radarr",
		Specifications: []ArrSpecification{
			{
				Name:           "spec1",
				Implementation: "ReleaseTitleSpecification",
				Fields:         json.RawMessage(`[{"name":"value","value":"already.arr"}]`),
			},
		},
	}

	arr := customCFToArr(cf)

	var fields []map[string]any
	if err := json.Unmarshal(arr.Specifications[0].Fields, &fields); err != nil {
		t.Fatalf("fields parse failed: %v", err)
	}
	if fields[0]["value"] != "already.arr" {
		t.Fatalf("field value = %v, want already.arr", fields[0]["value"])
	}
}

func TestCustomCFToArr_MultipleSpecs(t *testing.T) {
	cf := &CustomCF{
		Name:    "Multi",
		AppType: "radarr",
		Specifications: []ArrSpecification{
			{Name: "s1", Implementation: "Impl1", Negate: true, Required: false, Fields: json.RawMessage(`{"value":"a"}`)},
			{Name: "s2", Implementation: "Impl2", Negate: false, Required: true, Fields: json.RawMessage(`{"value":"b"}`)},
		},
	}

	arr := customCFToArr(cf)

	if len(arr.Specifications) != 2 {
		t.Fatalf("specs = %d, want 2", len(arr.Specifications))
	}
	if arr.Specifications[0].Name != "s1" || !arr.Specifications[0].Negate {
		t.Fatalf("spec0 = %+v, want s1/negate=true", arr.Specifications[0])
	}
	if arr.Specifications[1].Name != "s2" || !arr.Specifications[1].Required {
		t.Fatalf("spec1 = %+v, want s2/required=true", arr.Specifications[1])
	}
}

func TestCustomCFToArr_EmptySpecs(t *testing.T) {
	cf := &CustomCF{Name: "Empty", AppType: "radarr"}
	arr := customCFToArr(cf)
	if len(arr.Specifications) != 0 {
		t.Fatalf("specs = %d, want 0", len(arr.Specifications))
	}
}

func TestConvertFieldsToArr_ObjectToArray(t *testing.T) {
	input := json.RawMessage(`{"value":"test"}`)
	out := convertFieldsToArr(input)

	var fields []map[string]any
	if err := json.Unmarshal(out, &fields); err != nil {
		t.Fatalf("not array format: %v", err)
	}
	if len(fields) != 1 || fields[0]["name"] != "value" || fields[0]["value"] != "test" {
		t.Fatalf("unexpected: %s", string(out))
	}
}

func TestConvertFieldsToArr_ArrayPassthrough(t *testing.T) {
	input := json.RawMessage(`[{"name":"value","value":"x"}]`)
	out := convertFieldsToArr(input)
	if string(out) != string(input) {
		t.Fatalf("array should pass through: got %s", string(out))
	}
}

func TestConvertFieldsToArr_MultipleKeys(t *testing.T) {
	input := json.RawMessage(`{"exceptLanguage":false,"value":"test"}`)
	out := convertFieldsToArr(input)

	var fields []map[string]any
	if err := json.Unmarshal(out, &fields); err != nil {
		t.Fatalf("not array: %v", err)
	}
	if len(fields) != 2 {
		t.Fatalf("fields = %d, want 2", len(fields))
	}
	// Keys should be sorted alphabetically
	if fields[0]["name"] != "exceptLanguage" {
		t.Fatalf("first key = %v, want exceptLanguage (sorted)", fields[0]["name"])
	}
	if fields[1]["name"] != "value" {
		t.Fatalf("second key = %v, want value (sorted)", fields[1]["name"])
	}
}
