package arr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ArrClient talks to a Radarr or Sonarr instance's API v3.
type ArrClient struct {
	baseURL  string
	apiKey   string
	username string
	password string
	client   *http.Client
}

func NewArrClient(url, apiKey, username, password string, client *http.Client) *ArrClient {
	url = strings.TrimRight(url, "/")
	if !strings.HasPrefix(url, "http") {
		url = "http://" + url
	}
	return &ArrClient{
		baseURL:  url,
		apiKey:   apiKey,
		username: username,
		password: password,
		client:   client,
	}
}

// DoRequest performs an HTTP request with the API key header.
func (c *ArrClient) DoRequest(method, path string, body any) ([]byte, int, error) {
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
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}
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
	data, status, err := c.DoRequest("GET", "/system/status", nil)
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
	data, status, err := c.DoRequest("GET", "/customformat", nil)
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
	data, status, err := c.DoRequest("POST", "/customformat", cf)
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
	data, status, err := c.DoRequest("PUT", fmt.Sprintf("/customformat/%d", id), cf)
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
	data, status, err := c.DoRequest("DELETE", fmt.Sprintf("/customformat/%d", id), nil)
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
	data, status, err := c.DoRequest("GET", "/qualityprofile", nil)
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
	data, status, err := c.DoRequest("PUT", fmt.Sprintf("/qualityprofile/%d", profile.ID), profile)
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
// Sonarr/Radarr return null for maxSize/preferredSize when set to "Unlimited"
// (slider all the way right), and omit the field from the JSON response
// entirely. Using *float64 lets us distinguish null (Unlimited) from 0.0
// (explicit zero). nil is not "unset" — see QualitySizeLimits for why it must
// be compared against the limit rather than against 0.
type ArrQualityDefinition struct {
	ID            int              `json:"id"`
	Quality       ArrQualityRef    `json:"quality"`
	Title         string           `json:"title"`
	MinSize       *float64         `json:"minSize"`
	MaxSize       *float64         `json:"maxSize"`
	PreferredSize *float64         `json:"preferredSize"`
}

// FloatVal safely dereferences a *float64, returning 0 if nil.
func FloatVal(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// FloatPtr returns a pointer to a float64 value.
func FloatPtr(v float64) *float64 {
	return &v
}

// QualitySizeLimits holds the max/preferred sizes an instance treats as
// "Unlimited" — the slider pushed all the way right. Once a value reaches its
// limit, Radarr/Sonarr store it as null and omit the field when serialising,
// so a nil size means "at the limit", never "unset" and never 0.
//
// The values match Radarr >= 5.9.0.9049 and Sonarr >= 4.0.8.2158, which raised
// the old 400/399 (Radarr) and 400/395 (Sonarr) ceilings. Current TRaSH guide
// data targets the raised limits — the movie sizes ship preferred 1999 and
// max 2000 — and Recyclarr resolves them the same way, so Clonarr and
// Recyclarr write identical values to a shared instance.
type QualitySizeLimits struct {
	Max       float64
	Preferred float64
}

// QualitySizeLimitsFor returns the limits for an instance type ("radarr" or
// "sonarr"). Anything else gets the Radarr limits, matching what the quality
// size feature assumed before limits were modelled at all.
func QualitySizeLimitsFor(appType string) QualitySizeLimits {
	if strings.EqualFold(appType, "sonarr") {
		return QualitySizeLimits{Max: 1000, Preferred: 995}
	}
	return QualitySizeLimits{Max: 2000, Preferred: 1999}
}

// SizeOrLimit resolves a nullable size for comparison: nil means "Unlimited",
// which is equivalent to limit. Reaching for FloatVal here instead turns every
// Unlimited quality into 0.0 and makes it look permanently out of sync against
// TRaSH, which is the bug this exists to prevent. MinSize has no Unlimited end,
// so it keeps using FloatVal.
func SizeOrLimit(p *float64, limit float64) float64 {
	if p == nil {
		return limit
	}
	return *p
}

// SizePtr converts a concrete size into the pointer form used in write bodies,
// collapsing a value at or above limit to nil. Writing the number instead is
// accepted by the instance but read back as null, so the quality would show as
// out of sync on every following comparison.
func SizePtr(v, limit float64) *float64 {
	if v >= limit {
		return nil
	}
	return &v
}

// NormalizeSize applies the same collapse to an already-nullable size, leaving
// nil untouched. Used on definitions that arrive from the UI, which sends the
// numeric TRaSH targets rather than null.
func NormalizeSize(p *float64, limit float64) *float64 {
	if p == nil {
		return nil
	}
	return SizePtr(*p, limit)
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
	data, status, err := c.DoRequest("GET", "/qualitydefinition", nil)
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
		data, status, err := c.DoRequest("PUT", fmt.Sprintf("/qualitydefinition/%d", def.ID), &def)
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

// DeleteProfile removes a quality profile. Arr returns HTTP 400 if the
// profile is in use (assigned to any movie/series/import list/collection).
// Caller is responsible for verifying 0 usage first via the Count* helpers.
func (c *ArrClient) DeleteProfile(id int) error {
	data, status, err := c.DoRequest("DELETE", fmt.Sprintf("/qualityprofile/%d", id), nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("HTTP %d: %s", status, truncate(string(data), 200))
	}
	return nil
}

// profileRef is the minimal shape we need from /movie, /series,
// /importlist, /collection — just enough to count how many items
// reference each quality profile.
type profileRef struct {
	QualityProfileID int `json:"qualityProfileId"`
}

// ListMovieProfileIDs returns the quality-profile ID of every movie
// (Radarr only). Lightweight — minimal struct discards every field we
// don't need so a 1000+ movie library deserializes fast.
func (c *ArrClient) ListMovieProfileIDs() ([]int, error) {
	data, status, err := c.DoRequest("GET", "/movie", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", status, truncate(string(data), 200))
	}
	var items []profileRef
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("parse movies: %w", err)
	}
	out := make([]int, 0, len(items))
	for _, m := range items {
		if m.QualityProfileID > 0 {
			out = append(out, m.QualityProfileID)
		}
	}
	return out, nil
}

// ListSeriesProfileIDs returns the quality-profile ID of every series
// (Sonarr only). Symmetric counterpart to ListMovieProfileIDs.
func (c *ArrClient) ListSeriesProfileIDs() ([]int, error) {
	data, status, err := c.DoRequest("GET", "/series", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", status, truncate(string(data), 200))
	}
	var items []profileRef
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("parse series: %w", err)
	}
	out := make([]int, 0, len(items))
	for _, s := range items {
		if s.QualityProfileID > 0 {
			out = append(out, s.QualityProfileID)
		}
	}
	return out, nil
}

// ListImportListProfileIDs returns the quality-profile ID of every
// import list (both Radarr and Sonarr).
func (c *ArrClient) ListImportListProfileIDs() ([]int, error) {
	data, status, err := c.DoRequest("GET", "/importlist", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", status, truncate(string(data), 200))
	}
	var items []profileRef
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("parse import lists: %w", err)
	}
	out := make([]int, 0, len(items))
	for _, l := range items {
		if l.QualityProfileID > 0 {
			out = append(out, l.QualityProfileID)
		}
	}
	return out, nil
}

// ListCollectionProfileIDs returns the quality-profile ID of every
// collection (Radarr only — Sonarr has no equivalent). Each collection
// carries a profile ID used for movies added from the collection.
func (c *ArrClient) ListCollectionProfileIDs() ([]int, error) {
	data, status, err := c.DoRequest("GET", "/collection", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", status, truncate(string(data), 200))
	}
	var items []profileRef
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("parse collections: %w", err)
	}
	out := make([]int, 0, len(items))
	for _, c := range items {
		if c.QualityProfileID > 0 {
			out = append(out, c.QualityProfileID)
		}
	}
	return out, nil
}

// CreateProfile creates a new quality profile.
func (c *ArrClient) CreateProfile(profile *ArrQualityProfile) (*ArrQualityProfile, error) {
	data, status, err := c.DoRequest("POST", "/qualityprofile", profile)
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
	data, status, err := c.DoRequest("GET", "/language", nil)
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
	data, status, err := c.DoRequest("GET", "/config/naming", nil)
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
	data, status, err := c.DoRequest("PUT", "/config/naming", naming)
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


