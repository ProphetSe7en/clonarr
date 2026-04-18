package arr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ProwlarrClient talks to a Prowlarr instance's API v1.
type ProwlarrClient struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewProwlarrClient(url, apiKey string, client *http.Client) *ProwlarrClient {
	url = strings.TrimRight(url, "/")
	if !strings.HasPrefix(url, "http") {
		url = "http://" + url
	}
	return &ProwlarrClient{
		baseURL: url,
		apiKey:  apiKey,
		client:  client,
	}
}

func (c *ProwlarrClient) doRequest(method, path string, body any) ([]byte, int, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	reqURL := c.baseURL + "/api/v1" + path
	req, err := http.NewRequest(method, reqURL, bodyReader)
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

	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10 MiB max
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	return data, resp.StatusCode, nil
}

// ProwlarrIndexer is a simplified indexer from the Prowlarr API.
type ProwlarrIndexer struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Enable   bool   `json:"enable"`
	Protocol string `json:"protocol"`
}

// ProwlarrRelease is a search result from Prowlarr, stripped of download URLs.
type ProwlarrRelease struct {
	GUID        string              `json:"guid"`
	Title       string              `json:"title"`
	SortTitle   string              `json:"sortTitle"`
	Size        int64               `json:"size"`
	Indexer     string              `json:"indexer"`
	IndexerID   int                 `json:"indexerId"`
	Seeders     int                 `json:"seeders"`
	Leechers    int                 `json:"leechers"`
	Categories  []ProwlarrCategory  `json:"categories"`
	PublishDate string              `json:"publishDate"`
	InfoURL     string              `json:"infoUrl,omitempty"`
}

type ProwlarrCategory struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// TestConnection checks if Prowlarr is reachable.
func (c *ProwlarrClient) TestConnection() (string, error) {
	data, status, err := c.doRequest("GET", "/health", nil)
	if err != nil {
		return "", err
	}
	if status == 401 || status == 403 {
		return "", fmt.Errorf("authentication failed (HTTP %d)", status)
	}
	if status != 200 {
		return "", fmt.Errorf("unexpected status %d: %s", status, truncate(string(data), 200))
	}
	// Get version from system/status
	data, status, err = c.doRequest("GET", "/system/status", nil)
	if err != nil {
		return "connected", nil
	}
	if status == 200 {
		var sys struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(data, &sys) == nil && sys.Version != "" {
			return sys.Version, nil
		}
	}
	return "connected", nil
}

// ListIndexers returns enabled indexers.
func (c *ProwlarrClient) ListIndexers() ([]ProwlarrIndexer, error) {
	data, status, err := c.doRequest("GET", "/indexer", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", status, truncate(string(data), 200))
	}
	var all []ProwlarrIndexer
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, fmt.Errorf("parse indexers: %w", err)
	}
	// Filter to enabled only
	enabled := make([]ProwlarrIndexer, 0, len(all))
	for _, idx := range all {
		if idx.Enable {
			enabled = append(enabled, ProwlarrIndexer{
				ID:   idx.ID,
				Name: idx.Name,
			})
		}
	}
	return enabled, nil
}

// Search queries Prowlarr and returns results without download URLs.
func (c *ProwlarrClient) Search(query string, categories []int, indexerIDs []int) ([]ProwlarrRelease, error) {
	params := url.Values{}
	params.Set("query", query)
	for _, cat := range categories {
		params.Add("categories", fmt.Sprintf("%d", cat))
	}
	for _, id := range indexerIDs {
		params.Add("indexerIds", fmt.Sprintf("%d", id))
	}

	data, status, err := c.doRequest("GET", "/search?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", status, truncate(string(data), 200))
	}

	// Parse full response then strip sensitive fields
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse results: %w", err)
	}

	const maxResults = 200
	releases := make([]ProwlarrRelease, 0, min(len(raw), maxResults))
	for _, r := range raw {
		if len(releases) >= maxResults {
			break
		}
		var full struct {
			GUID        string             `json:"guid"`
			Title       string             `json:"title"`
			SortTitle   string             `json:"sortTitle"`
			Size        int64              `json:"size"`
			Indexer     string             `json:"indexer"`
			IndexerID   int                `json:"indexerId"`
			Seeders     int                `json:"seeders"`
			Leechers    int                `json:"leechers"`
			Categories  []ProwlarrCategory `json:"categories"`
			PublishDate string             `json:"publishDate"`
			InfoURL     string             `json:"infoUrl"`
		}
		if err := json.Unmarshal(r, &full); err != nil {
			continue
		}
		releases = append(releases, ProwlarrRelease{
			GUID:        full.GUID,
			Title:       full.Title,
			SortTitle:   full.SortTitle,
			Size:        full.Size,
			Indexer:     full.Indexer,
			IndexerID:   full.IndexerID,
			Seeders:     full.Seeders,
			Leechers:    full.Leechers,
			Categories:  full.Categories,
			PublishDate: full.PublishDate,
			InfoURL:     full.InfoURL,
		})
	}
	return releases, nil
}
