package core

import (
	"clonarr/internal/arr"
	"encoding/json"
	"sort"
)

// TrashCFToArr converts a TRaSH CF definition to Arr API format.
func TrashCFToArr(cf *TrashCF) *arr.ArrCF {
	out := &arr.ArrCF{
		Name:                            cf.Name,
		IncludeCustomFormatWhenRenaming: cf.IncludeInRename,
	}

	for _, spec := range cf.Specifications {
		arrSpec := arr.ArrSpecification{
			Name:           spec.Name,
			Implementation: spec.Implementation,
			Negate:         spec.Negate,
			Required:       spec.Required,
			Fields:         ConvertFieldsToArr(spec.Fields),
		}
		out.Specifications = append(out.Specifications, arrSpec)
	}

	return out
}

// ConvertFieldsToArr converts TRaSH fields format {"value": X} to Arr format [{"name":"value","value":X}].
func ConvertFieldsToArr(raw json.RawMessage) json.RawMessage {
	// Try to parse as object {"value": X}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw // return as-is if we can't parse
	}

	// Check if it's already in array format
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err == nil {
		return raw // already array format
	}

	// M6: Convert object to array format with deterministic ordering
	keys := make([]string, 0, len(obj))
	for key := range obj {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var fields []map[string]any
	for _, key := range keys {
		var v any
		if err := json.Unmarshal(obj[key], &v); err != nil {
			return raw // malformed field value, return original
		}
		fields = append(fields, map[string]any{
			"name":  key,
			"value": v,
		})
	}

	result, err := json.Marshal(fields)
	if err != nil {
		return raw
	}
	return result
}

// ExtractFieldValue extracts the primary "value" from either TRaSH or Arr fields format.
func ExtractFieldValue(raw json.RawMessage) any {
	// Try object format: {"value": X}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil {
		if v, ok := obj["value"]; ok {
			return v
		}
	}

	// Try array format: [{"name":"value","value":X}]
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err == nil {
		for _, f := range arr {
			if f["name"] == "value" {
				return f["value"]
			}
		}
	}

	return nil
}
