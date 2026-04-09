package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// ArrClient talks to a Radarr or Sonarr instance's API v3.
type ArrClient struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewArrClient(url, apiKey string, client *http.Client) *ArrClient {
	url = strings.TrimRight(url, "/")
	if !strings.HasPrefix(url, "http") {
		url = "http://" + url
	}
	return &ArrClient{
		baseURL: url,
		apiKey:  apiKey,
		client:  client,
	}
}

// doRequest performs an HTTP request with the API key header.
func (c *ArrClient) doRequest(method, path string, body any) ([]byte, int, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	url := c.baseURL + "/api/v3" + path
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-Api-Key", c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 50<<20)) // 50 MiB max
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	return data, resp.StatusCode, nil
}

// --- System ---

// ArrSystemStatus is the response from /api/v3/system/status.
type ArrSystemStatus struct {
	AppName string `json:"appName"`
	Version string `json:"version"`
}

// TestConnection verifies connectivity and returns the app version.
func (c *ArrClient) TestConnection() (*ArrSystemStatus, error) {
	data, status, err := c.doRequest("GET", "/system/status", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", status, truncate(string(data), 200))
	}
	var result ArrSystemStatus
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &result, nil
}

// --- Custom Formats ---

// ArrCF represents a Custom Format as returned by Radarr/Sonarr API.
type ArrCF struct {
	ID                           int                `json:"id,omitempty"`
	Name                         string             `json:"name"`
	IncludeCustomFormatWhenRenaming bool            `json:"includeCustomFormatWhenRenaming"`
	Specifications               []ArrSpecification `json:"specifications"`
}

// ArrSpecification is a spec within an Arr Custom Format.
type ArrSpecification struct {
	Name           string          `json:"name"`
	Implementation string          `json:"implementation"`
	Negate         bool            `json:"negate"`
	Required       bool            `json:"required"`
	Fields         json.RawMessage `json:"fields"`
}

// ListCustomFormats fetches all CFs from the instance.
func (c *ArrClient) ListCustomFormats() ([]ArrCF, error) {
	data, status, err := c.doRequest("GET", "/customformat", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", status, truncate(string(data), 200))
	}
	var cfs []ArrCF
	if err := json.Unmarshal(data, &cfs); err != nil {
		return nil, fmt.Errorf("parse CFs: %w", err)
	}
	return cfs, nil
}

// CreateCustomFormat creates a new CF.
func (c *ArrClient) CreateCustomFormat(cf *ArrCF) (*ArrCF, error) {
	data, status, err := c.doRequest("POST", "/customformat", cf)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", status, truncate(string(data), 200))
	}
	var result ArrCF
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &result, nil
}

// UpdateCustomFormat updates an existing CF.
func (c *ArrClient) UpdateCustomFormat(id int, cf *ArrCF) (*ArrCF, error) {
	cf.ID = id
	data, status, err := c.doRequest("PUT", fmt.Sprintf("/customformat/%d", id), cf)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", status, truncate(string(data), 200))
	}
	var result ArrCF
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &result, nil
}

// DeleteCustomFormat deletes a CF by ID.
func (c *ArrClient) DeleteCustomFormat(id int) error {
	data, status, err := c.doRequest("DELETE", fmt.Sprintf("/customformat/%d", id), nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("HTTP %d: %s", status, truncate(string(data), 200))
	}
	return nil
}

// --- Quality Profiles ---

// ArrQualityProfile represents a quality profile from the Arr API.
type ArrQualityProfile struct {
	ID                    int                    `json:"id"`
	Name                  string                 `json:"name"`
	UpgradeAllowed        bool                   `json:"upgradeAllowed"`
	Cutoff                int                    `json:"cutoff"`
	MinFormatScore        int                    `json:"minFormatScore"`
	CutoffFormatScore     int                    `json:"cutoffFormatScore"`
	MinUpgradeFormatScore int                    `json:"minUpgradeFormatScore"`
	FormatItems           []ArrProfileFormatItem `json:"formatItems"`
	Items                 []ArrQualityItem       `json:"items"`
	Language              *ArrLanguage           `json:"language,omitempty"`
}

// ArrQualityItem represents a quality level or group within a quality profile.
type ArrQualityItem struct {
	ID      int              `json:"id,omitempty"`
	Name    string           `json:"name,omitempty"`
	Quality *ArrQualityRef   `json:"quality"`
	Items   []ArrQualityItem `json:"items"`
	Allowed bool             `json:"allowed"`
}

// ArrLanguage represents a language from the Arr API.
type ArrLanguage struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// ArrProfileFormatItem is a CF score assignment within a profile.
type ArrProfileFormatItem struct {
	Format int    `json:"format"` // Arr CF ID
	Name   string `json:"name"`
	Score  int    `json:"score"`
}

// ListProfiles fetches all quality profiles.
func (c *ArrClient) ListProfiles() ([]ArrQualityProfile, error) {
	data, status, err := c.doRequest("GET", "/qualityprofile", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", status, truncate(string(data), 200))
	}
	var profiles []ArrQualityProfile
	if err := json.Unmarshal(data, &profiles); err != nil {
		return nil, fmt.Errorf("parse profiles: %w", err)
	}
	return profiles, nil
}

// UpdateProfile updates a quality profile (primarily for CF scores).
func (c *ArrClient) UpdateProfile(profile *ArrQualityProfile) error {
	data, status, err := c.doRequest("PUT", fmt.Sprintf("/qualityprofile/%d", profile.ID), profile)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("HTTP %d: %s", status, truncate(string(data), 200))
	}
	return nil
}

// --- Quality Definitions ---

// ArrQualityDefinition represents a quality size definition.
// Note: Radarr omits maxSize/preferredSize from API responses when they
// haven't been explicitly set. Go unmarshals these as 0. The frontend
// treats 0 as "not explicitly set" and marks them as needing sync.
type ArrQualityDefinition struct {
	ID            int              `json:"id"`
	Quality       ArrQualityRef    `json:"quality"`
	Title         string           `json:"title"`
	MinSize       float64          `json:"minSize"`
	MaxSize       float64          `json:"maxSize"`
	PreferredSize float64          `json:"preferredSize"`
}

type ArrQualityRef struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Source     string `json:"source,omitempty"`
	Resolution int    `json:"resolution,omitempty"`
	Modifier   string `json:"modifier,omitempty"`
}

// ListQualityDefinitions fetches all quality size definitions.
func (c *ArrClient) ListQualityDefinitions() ([]ArrQualityDefinition, error) {
	data, status, err := c.doRequest("GET", "/qualitydefinition", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", status, truncate(string(data), 200))
	}
	var defs []ArrQualityDefinition
	if err := json.Unmarshal(data, &defs); err != nil {
		return nil, fmt.Errorf("parse quality definitions: %w", err)
	}
	return defs, nil
}

// UpdateQualityDefinitions bulk-updates quality size definitions.
func (c *ArrClient) UpdateQualityDefinitions(defs []ArrQualityDefinition) error {
	for _, def := range defs {
		data, status, err := c.doRequest("PUT", fmt.Sprintf("/qualitydefinition/%d", def.ID), &def)
		if err != nil {
			return fmt.Errorf("update %s: %w", def.Quality.Name, err)
		}
		if status < 200 || status >= 300 {
			return fmt.Errorf("update %s: HTTP %d: %s", def.Quality.Name, status, truncate(string(data), 200))
		}
		time.Sleep(100 * time.Millisecond) // rate limit
	}
	return nil
}

// CreateProfile creates a new quality profile.
func (c *ArrClient) CreateProfile(profile *ArrQualityProfile) (*ArrQualityProfile, error) {
	data, status, err := c.doRequest("POST", "/qualityprofile", profile)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", status, truncate(string(data), 200))
	}
	var result ArrQualityProfile
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &result, nil
}

// ListLanguages fetches available languages (Radarr only).
func (c *ArrClient) ListLanguages() ([]ArrLanguage, error) {
	data, status, err := c.doRequest("GET", "/language", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", status, truncate(string(data), 200))
	}
	var languages []ArrLanguage
	if err := json.Unmarshal(data, &languages); err != nil {
		return nil, fmt.Errorf("parse languages: %w", err)
	}
	return languages, nil
}

// ArrNamingConfig represents the naming configuration from Radarr/Sonarr.
type ArrNamingConfig map[string]any

// GetNaming fetches the current naming config from the instance.
func (c *ArrClient) GetNaming() (ArrNamingConfig, error) {
	data, status, err := c.doRequest("GET", "/config/naming", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", status, truncate(string(data), 200))
	}
	var naming ArrNamingConfig
	if err := json.Unmarshal(data, &naming); err != nil {
		return nil, fmt.Errorf("parse naming: %w", err)
	}
	return naming, nil
}

// UpdateNaming applies a naming config to the instance via PUT.
func (c *ArrClient) UpdateNaming(naming ArrNamingConfig) (ArrNamingConfig, error) {
	data, status, err := c.doRequest("PUT", "/config/naming", naming)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", status, truncate(string(data), 200))
	}
	var result ArrNamingConfig
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse naming response: %w", err)
	}
	return result, nil
}

// truncate limits a string to maxLen runes (M14: safe for UTF-8).
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// trashCFToArr converts a TRaSH CF definition to Arr API format.
func trashCFToArr(cf *TrashCF) *ArrCF {
	arr := &ArrCF{
		Name:                            cf.Name,
		IncludeCustomFormatWhenRenaming: cf.IncludeInRename,
	}

	for _, spec := range cf.Specifications {
		arrSpec := ArrSpecification{
			Name:           spec.Name,
			Implementation: spec.Implementation,
			Negate:         spec.Negate,
			Required:       spec.Required,
			Fields:         convertFieldsToArr(spec.Fields),
		}
		arr.Specifications = append(arr.Specifications, arrSpec)
	}

	return arr
}

// convertFieldsToArr converts TRaSH fields format {"value": X} to Arr format [{"name":"value","value":X}].
func convertFieldsToArr(raw json.RawMessage) json.RawMessage {
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

// extractFieldValue extracts the primary "value" from either TRaSH or Arr fields format.
func extractFieldValue(raw json.RawMessage) any {
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
