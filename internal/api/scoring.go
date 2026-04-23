package api

import (
	"clonarr/internal/arr"
	"clonarr/internal/core"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// --- Scoring Sandbox ---

func (s *Server) handleTestProwlarr(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req struct {
		URL    string `json:"url"`
		APIKey string `json:"apiKey"`
	}
	// Accept ad-hoc URL/key in body, fall back to saved config
	json.NewDecoder(r.Body).Decode(&req) // ignore error — fields optional
	cfg := s.Core.Config.Get()
	if req.URL == "" {
		req.URL = cfg.Prowlarr.URL
	}
	if req.APIKey == "" || isMasked(req.APIKey) {
		req.APIKey = cfg.Prowlarr.APIKey
	}
	if req.URL == "" || req.APIKey == "" {
		writeJSON(w, map[string]any{"connected": false, "error": "Prowlarr URL and API key are required"})
		return
	}
	client := arr.NewProwlarrClient(req.URL, req.APIKey, s.Core.HTTPClient)
	version, err := client.TestConnection()
	if err != nil {
		writeJSON(w, map[string]any{"connected": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"connected": true, "version": version})
}

func (s *Server) handleScoringProwlarrIndexers(w http.ResponseWriter, r *http.Request) {
	cfg := s.Core.Config.Get()
	if !cfg.Prowlarr.Enabled || cfg.Prowlarr.URL == "" {
		writeJSON(w, []any{})
		return
	}
	client := arr.NewProwlarrClient(cfg.Prowlarr.URL, cfg.Prowlarr.APIKey, s.Core.HTTPClient)
	indexers, err := client.ListIndexers()
	if err != nil {
		writeError(w, 502, "Prowlarr error: "+err.Error())
		return
	}
	writeJSON(w, indexers)
}

func (s *Server) handleScoringProwlarrSearch(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Query      string `json:"query"`
		Categories []int  `json:"categories"`
		IndexerIDs []int  `json:"indexerIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}
	if req.Query == "" {
		writeError(w, 400, "query is required")
		return
	}
	if len(req.Query) > 500 {
		writeError(w, 400, "query too long (max 500 characters)")
		return
	}

	cfg := s.Core.Config.Get()
	if !cfg.Prowlarr.Enabled || cfg.Prowlarr.URL == "" {
		writeError(w, 400, "Prowlarr not configured or disabled")
		return
	}
	client := arr.NewProwlarrClient(cfg.Prowlarr.URL, cfg.Prowlarr.APIKey, s.Core.HTTPClient)
	releases, err := client.Search(req.Query, req.Categories, req.IndexerIDs)
	if err != nil {
		writeError(w, 502, "Prowlarr search failed: "+err.Error())
		return
	}
	writeJSON(w, releases)
}

// ScoringParseResult is the enriched parse response for the scoring sandbox.
type ScoringParseResult struct {
	Title         string             `json:"title"`
	Parsed        ScoringParsedInfo  `json:"parsed"`
	MatchedCFs    []ScoringMatchedCF `json:"matchedCFs"`
	InstanceScore int                `json:"instanceScore"`
}

type ScoringParsedInfo struct {
	Title        string   `json:"title"`
	Year         int      `json:"year,omitempty"`
	Quality      string   `json:"quality"`
	Languages    []string `json:"languages"`
	ReleaseGroup string   `json:"releaseGroup"`
	Edition      string   `json:"edition,omitempty"`
	Season       int      `json:"season,omitempty"`
	Episodes     []int    `json:"episodes,omitempty"`
}

type ScoringMatchedCF struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	TrashID string `json:"trashId,omitempty"`
}

func (s *Server) handleScoringParse(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		InstanceID string `json:"instanceId"`
		Title      string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}
	if req.InstanceID == "" || req.Title == "" {
		writeError(w, 400, "instanceId and title are required")
		return
	}

	inst, ok := s.Core.Config.GetInstance(req.InstanceID)
	if !ok {
		writeError(w, 404, "Instance not found")
		return
	}

	result, err := s.parseSingleRelease(inst, req.Title)
	if err != nil {
		writeError(w, 502, "Parse failed: "+err.Error())
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleScoringParseBatch(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		InstanceID string   `json:"instanceId"`
		Titles     []string `json:"titles"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}
	if req.InstanceID == "" || len(req.Titles) == 0 {
		writeError(w, 400, "instanceId and titles are required")
		return
	}
	if len(req.Titles) > 15 {
		writeError(w, 400, "Maximum 15 titles per batch")
		return
	}

	inst, ok := s.Core.Config.GetInstance(req.InstanceID)
	if !ok {
		writeError(w, 404, "Instance not found")
		return
	}

	results := make([]ScoringParseResult, 0, len(req.Titles))
	for _, title := range req.Titles {
		result, err := s.parseSingleRelease(inst, title)
		if err != nil {
			// Include error as empty result with the title
			results = append(results, ScoringParseResult{Title: title})
			continue
		}
		results = append(results, *result)
	}
	writeJSON(w, results)
}

// isLanguageCF returns true for CFs that depend on language matching — these can't
// be evaluated without movie context (TMDB lookup) so they're stripped from sandbox
// parse results. Matches: "Wrong Language", "Language: Not French", "Language: XXX".
func isLanguageCF(name string) bool {
	lower := strings.ToLower(name)
	return lower == "wrong language" || strings.HasPrefix(lower, "language:")
}

// parseSingleRelease calls the Arr Parse API and enriches CFs with trash_ids.
func (s *Server) parseSingleRelease(inst core.Instance, title string) (*ScoringParseResult, error) {
	client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)
	data, status, err := client.DoRequest("GET", "/parse?title="+url.QueryEscape(title), nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", status, string(data))
	}

	// Parse the raw response — Radarr uses parsedMovieInfo, Sonarr uses parsedEpisodeInfo
	var raw struct {
		Title             string          `json:"title"`
		ParsedMovieInfo   json.RawMessage `json:"parsedMovieInfo"`
		ParsedEpisodeInfo json.RawMessage `json:"parsedEpisodeInfo"`
		CustomFormats     []struct {
			ID                              int    `json:"id"`
			Name                            string `json:"name"`
			IncludeCustomFormatWhenRenaming bool   `json:"includeCustomFormatWhenRenaming"`
		} `json:"customFormats"`
		CustomFormatScore int `json:"customFormatScore"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	// Extract parsed info from the appropriate field
	parsed := ScoringParsedInfo{}
	if len(raw.ParsedMovieInfo) > 0 && string(raw.ParsedMovieInfo) != "null" {
		var movie struct {
			MovieTitles []string `json:"movieTitles"`
			Year        int      `json:"year"`
			Quality     struct {
				Quality struct {
					Name string `json:"name"`
				} `json:"quality"`
			} `json:"quality"`
			Languages []struct {
				Name string `json:"name"`
			} `json:"languages"`
			ReleaseGroup string `json:"releaseGroup"`
			Edition      string `json:"edition"`
		}
		if json.Unmarshal(raw.ParsedMovieInfo, &movie) == nil {
			if len(movie.MovieTitles) > 0 {
				parsed.Title = movie.MovieTitles[0]
			}
			parsed.Year = movie.Year
			parsed.Quality = movie.Quality.Quality.Name
			for _, l := range movie.Languages {
				parsed.Languages = append(parsed.Languages, l.Name)
			}
			parsed.ReleaseGroup = movie.ReleaseGroup
			parsed.Edition = movie.Edition
		}
	} else if len(raw.ParsedEpisodeInfo) > 0 && string(raw.ParsedEpisodeInfo) != "null" {
		var ep struct {
			SeriesTitle    string `json:"seriesTitle"`
			SeasonNumber   int    `json:"seasonNumber"`
			EpisodeNumbers []int  `json:"episodeNumbers"`
			Quality        struct {
				Quality struct {
					Name string `json:"name"`
				} `json:"quality"`
			} `json:"quality"`
			Languages []struct {
				Name string `json:"name"`
			} `json:"languages"`
			ReleaseGroup string `json:"releaseGroup"`
		}
		if json.Unmarshal(raw.ParsedEpisodeInfo, &ep) == nil {
			parsed.Title = ep.SeriesTitle
			parsed.Season = ep.SeasonNumber
			parsed.Episodes = ep.EpisodeNumbers
			parsed.Quality = ep.Quality.Quality.Name
			for _, l := range ep.Languages {
				parsed.Languages = append(parsed.Languages, l.Name)
			}
			parsed.ReleaseGroup = ep.ReleaseGroup
		}
	}

	// Fallback for numeric release groups that Arr's parser drops (e.g. "-126811").
	// Only fires when Arr returned empty — alphanumeric groups stay on Arr's parse.
	if parsed.ReleaseGroup == "" {
		if m := numericReleaseGroupRE.FindStringSubmatch(title); m != nil {
			parsed.ReleaseGroup = m[1]
		}
	}

	// Build CF name → trash_id map from TRaSH data
	ad := s.Core.Trash.GetAppData(inst.Type)
	cfNameToTrashID := make(map[string]string)
	if ad != nil {
		for trashID, cf := range ad.CustomFormats {
			cfNameToTrashID[cf.Name] = trashID
		}
	}
	// Also include custom CFs
	customCFs := s.Core.CustomCFs.List(inst.Type)
	for _, ccf := range customCFs {
		cfNameToTrashID[ccf.Name] = ccf.ID
	}

	// Enrich matched CFs with trash_ids. Language-aware CFs ("Wrong Language",
	// "Language: Not X", etc.) are filtered out: the Parse API runs without movie
	// context so it can't resolve TMDB original language, making language CFs fire
	// incorrectly (returning Language=Unknown → Wrong Language always matches).
	// The language section is omitted from the parsed metadata for the same reason.
	matchedCFs := make([]ScoringMatchedCF, 0, len(raw.CustomFormats))
	for _, cf := range raw.CustomFormats {
		if isLanguageCF(cf.Name) {
			continue
		}
		mcf := ScoringMatchedCF{
			ID:   cf.ID,
			Name: cf.Name,
		}
		if tid, ok := cfNameToTrashID[cf.Name]; ok {
			mcf.TrashID = tid
		}
		matchedCFs = append(matchedCFs, mcf)
	}
	parsed.Languages = nil

	return &ScoringParseResult{
		Title:         title,
		Parsed:        parsed,
		MatchedCFs:    matchedCFs,
		InstanceScore: raw.CustomFormatScore,
	}, nil
}

// handleScoringProfileScores returns the full CF→score map for a profile.
// Supports TRaSH profiles (trash:<id>), imported profiles (imported:<id>),
// and instance profiles (inst:<instanceId>:<profileId>).
func (s *Server) handleScoringProfileScores(w http.ResponseWriter, r *http.Request) {
	profileKey := r.URL.Query().Get("profileKey")
	appType := r.URL.Query().Get("appType")
	if profileKey == "" || appType == "" {
		writeError(w, 400, "profileKey and appType are required")
		return
	}

	type ScoreEntry struct {
		TrashID string `json:"trashId"`
		Name    string `json:"name"`
		Score   int    `json:"score"`
	}

	result := struct {
		Scores   []ScoreEntry `json:"scores"`
		MinScore int          `json:"minScore"`
	}{}

	if strings.HasPrefix(profileKey, "trash:") {
		trashID := strings.TrimPrefix(profileKey, "trash:")
		snap := s.Core.Trash.Snapshot()
		ad := core.SnapshotAppData(snap, appType)
		if ad == nil {
			writeJSON(w, result)
			return
		}

		// Find profile for minFormatScore
		for _, p := range ad.Profiles {
			if p.TrashID == trashID {
				result.MinScore = p.MinFormatScore
				break
			}
		}

		// Get core CFs with scores
		resolvedCFs, scoreCtx := core.ResolveProfileCFs(ad, trashID)
		seen := make(map[string]bool)
		for _, cf := range resolvedCFs {
			result.Scores = append(result.Scores, ScoreEntry{
				TrashID: cf.TrashID,
				Name:    cf.Name,
				Score:   cf.Score,
			})
			seen[cf.TrashID] = true
		}

		// Get optional CFs from categories (use the profile's score context)
		cfCategories := core.ProfileCFCategories(ad, trashID)
		for _, cat := range cfCategories {
			for _, g := range cat.Groups {
				for _, cf := range g.CFs {
					if seen[cf.TrashID] {
						continue
					}
					// Look up score from TRaSH CF data using the profile's score context
					score := 0
					if fullCF, ok := ad.CustomFormats[cf.TrashID]; ok {
						if s, ok := fullCF.TrashScores[scoreCtx]; ok {
							score = s
						} else if s, ok := fullCF.TrashScores["default"]; ok {
							score = s
						}
					}
					result.Scores = append(result.Scores, ScoreEntry{
						TrashID: cf.TrashID,
						Name:    cf.Name,
						Score:   score,
					})
					seen[cf.TrashID] = true
				}
			}
		}

	} else if strings.HasPrefix(profileKey, "imported:") {
		id := strings.TrimPrefix(profileKey, "imported:")
		prof, ok := s.Core.Profiles.Get(id)
		if !ok {
			writeError(w, 404, "Imported profile not found")
			return
		}
		result.MinScore = prof.MinFormatScore

		ad := s.Core.Trash.GetAppData(appType)
		for trashID, score := range prof.FormatItems {
			name := trashID
			if ad != nil {
				if cf, ok := ad.CustomFormats[trashID]; ok {
					name = cf.Name
				}
			}
			result.Scores = append(result.Scores, ScoreEntry{
				TrashID: trashID,
				Name:    name,
				Score:   score,
			})
		}

	} else if strings.HasPrefix(profileKey, "inst:") {
		// inst:<profileId> — needs instanceId query param
		instanceID := r.URL.Query().Get("instanceId")
		profileIDStr := strings.TrimPrefix(profileKey, "inst:")
		profileID, err := strconv.Atoi(profileIDStr)
		if err != nil || instanceID == "" {
			writeError(w, 400, "Invalid instance profile key")
			return
		}
		inst, ok := s.Core.Config.GetInstance(instanceID)
		if !ok {
			writeError(w, 404, "Instance not found")
			return
		}
		client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)
		profiles, err := client.ListProfiles()
		if err != nil {
			writeError(w, 502, "Failed to fetch profiles: "+err.Error())
			return
		}
		for _, p := range profiles {
			if p.ID == profileID {
				result.MinScore = p.MinFormatScore
				for _, fi := range p.FormatItems {
					result.Scores = append(result.Scores, ScoreEntry{
						TrashID: "",
						Name:    fi.Name,
						Score:   fi.Score,
					})
				}
				break
			}
		}
	}

	// Strip language-aware CFs from profile scores so they don't appear in the
	// sandbox's "Unmatched (in profile, not in release)" list either. Same
	// rationale as the parse-side filter: without movie context, language CFs
	// are noise that confuses the total.
	filtered := result.Scores[:0]
	for _, s := range result.Scores {
		if !isLanguageCF(s.Name) {
			filtered = append(filtered, s)
		}
	}
	result.Scores = filtered

	writeJSON(w, result)
}

// handleDebugLog receives frontend log messages.
// Category + Message reach /config/debug.log, which admins download via
// handleDebugDownload after incidents. An authenticated caller who can
// inject control characters (newlines, escape sequences) could forge
// `[TIMESTAMP] [CATEGORY] …` lines to pollute the forensic trail. We
// whitelist Category and sanitize Message before logging.
func (s *Server) handleDebugLog(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Category string `json:"category"`
		Message  string `json:"message"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(400)
		return
	}
	if !isValidLogCategory(req.Category) {
		req.Category = "UI"
	}
	s.Core.DebugLog.Log(req.Category, sanitizeLogField(req.Message))
	w.WriteHeader(204)
}

// isValidLogCategory rejects Category values not in the app's known set.
func isValidLogCategory(c string) bool {
	switch c {
	case core.LogSync, core.LogCompare, core.LogAutoSync, core.LogTrash, core.LogError, core.LogUI, core.LogConfig:
		return true
	}
	return false
}

// sanitizeLogField strips control characters (CR, LF, NUL, other < 0x20)
// and caps length to 1024 bytes for debug-log context. Prevents log
// forgery: an authenticated caller submitting Message with embedded \n
// must not be able to inject fake `[TIMESTAMP] [CATEGORY] …` lines.
func sanitizeLogField(st string) string {
	const maxLen = 1024
	if len(st) > maxLen {
		st = st[:maxLen]
	}
	b := make([]byte, 0, len(st))
	for i := 0; i < len(st); i++ {
		c := st[i]
		if c < 0x20 || c == 0x7f {
			b = append(b, ' ')
			continue
		}
		b = append(b, c)
	}
	return string(b)
}

// handleDebugDownload serves the debug log file for download.
func (s *Server) handleDebugDownload(w http.ResponseWriter, r *http.Request) {
	path := s.Core.DebugLog.FilePath()
	f, err := os.Open(path)
	if err != nil {
		writeError(w, 404, "No debug log file found")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		writeError(w, 500, "Failed to read log file")
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=\"clonarr-debug.log\"")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fi.Size()))
	io.Copy(w, f)
}
