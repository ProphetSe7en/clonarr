package core

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// --- TRaSH Data Types ---

// TrashCF represents a Custom Format definition from TRaSH Guides.
type TrashCF struct {
	TrashID         string            `json:"trash_id"`
	TrashScores     map[string]int    `json:"trash_scores"`
	TrashRegex      string            `json:"trash_regex,omitempty"`
	Name            string            `json:"name"`
	IncludeInRename bool              `json:"includeCustomFormatWhenRenaming"`
	Specifications  []CFSpecification `json:"specifications"`
	Description     string            `json:"description,omitempty"` // from includes/cf-descriptions/*.md
}

// CFSpecification is a single matching rule within a Custom Format.
type CFSpecification struct {
	Name           string          `json:"name"`
	Implementation string          `json:"implementation"`
	Negate         bool            `json:"negate"`
	Required       bool            `json:"required"`
	Fields         json.RawMessage `json:"fields"`
}

// TrashCFGroup bundles related CFs for profiles.
type TrashCFGroup struct {
	Name             string         `json:"name"`
	TrashID          string         `json:"trash_id"`
	TrashDescription string         `json:"trash_description"`
	Default          string         `json:"default"`
	CustomFormats    []CFGroupEntry `json:"custom_formats"`
	QualityProfiles  struct {
		Include map[string]string `json:"include"`
	} `json:"quality_profiles"`
}

// CFGroupEntry is a CF reference within a group.
type CFGroupEntry struct {
	Name     string `json:"name"`
	TrashID  string `json:"trash_id"`
	Required bool   `json:"required"`
	Default  *bool  `json:"default,omitempty"` // TRaSH recommendation for optional CFs
}

// TrashQualityProfile is a complete quality profile definition.
type TrashQualityProfile struct {
	TrashID               string            `json:"trash_id"`
	Name                  string            `json:"name"`
	TrashScoreSet         string            `json:"trash_score_set,omitempty"`
	TrashDescription      string            `json:"trash_description,omitempty"`
	TrashURL              string            `json:"trash_url,omitempty"`
	Group                 int               `json:"group"`
	UpgradeAllowed        bool              `json:"upgradeAllowed"`
	Cutoff                string            `json:"cutoff"`
	MinFormatScore        int               `json:"minFormatScore"`
	CutoffFormatScore     int               `json:"cutoffFormatScore"`
	MinUpgradeFormatScore int               `json:"minUpgradeFormatScore"`
	Language              string            `json:"language"`
	Items                 []QualityItem     `json:"items"`
	FormatItems           map[string]string `json:"formatItems"`
	FormatItemsOrder      []string          `json:"-"` // original insertion order of FormatItems keys (CF names)
}

// QualityItem is a quality level or group within a profile.
type QualityItem struct {
	Name    string   `json:"name"`
	Allowed bool     `json:"allowed"`
	Items   []string `json:"items,omitempty"`
}

// cloneQualityItems returns a deep copy of a slice of QualityItems.
// Used for snapshot/deep-copy in config snapshots so the live config can be
// safely shared across goroutines.
func cloneQualityItems(src []QualityItem) []QualityItem {
	if len(src) == 0 {
		return nil
	}
	out := make([]QualityItem, len(src))
	for i, it := range src {
		out[i] = QualityItem{Name: it.Name, Allowed: it.Allowed}
		if len(it.Items) > 0 {
			out[i].Items = make([]string, len(it.Items))
			copy(out[i].Items, it.Items)
		}
	}
	return out
}

// ProfileGroup organizes profiles into display categories.
type ProfileGroup struct {
	Name     string            `json:"name"`
	Profiles map[string]string `json:"profiles"` // filename -> trash_id
}

// TrashQualitySize defines bitrate limits per quality level.
type TrashQualitySize struct {
	TrashID   string             `json:"trash_id"`
	Type      string             `json:"type"`
	Qualities []QualitySizeEntry `json:"qualities"`
}

// QualitySizeEntry is a single quality definition's size limits.
type QualitySizeEntry struct {
	Quality   string  `json:"quality"`
	Min       float64 `json:"min"`
	Preferred float64 `json:"preferred"`
	Max       float64 `json:"max"`
}

// TrashNaming holds file/folder naming schemes from TRaSH.
type TrashNaming struct {
	// Radarr fields
	Folder map[string]string `json:"folder,omitempty"`
	File   map[string]string `json:"file,omitempty"`
	// Sonarr fields
	Season   map[string]string            `json:"season,omitempty"`
	Series   map[string]string            `json:"series,omitempty"`
	Episodes map[string]map[string]string `json:"episodes,omitempty"`
}

// --- Aggregated Data ---

// ConflictEntry is a CF reference in a conflict group.
type ConflictEntry struct {
	TrashID string `json:"trash_id"`
	Name    string `json:"name,omitempty"`
}

// conflictsRaw matches the TRaSH conflicts.json schema where trash_ids are object keys:
//
//	{"custom_formats": [{"trash_id": {"name": "...", "desc": "..."}, ...}]}
type conflictsRaw struct {
	CustomFormats []map[string]struct {
		Name string `json:"name"`
		Desc string `json:"desc"`
	} `json:"custom_formats"`
}

// ConflictsData holds mutually exclusive CF groups from conflicts.json.
// Internally converted from the raw format to flat ConflictEntry slices for API/UI use.
type ConflictsData struct {
	CustomFormats [][]ConflictEntry `json:"custom_formats"`
}

// parseConflicts reads TRaSH conflicts.json (map-keyed by trash_id) into ConflictsData.
func parseConflicts(data []byte) (*ConflictsData, error) {
	var raw conflictsRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	result := &ConflictsData{}
	for _, group := range raw.CustomFormats {
		var entries []ConflictEntry
		for trashID, info := range group {
			entries = append(entries, ConflictEntry{TrashID: trashID, Name: info.Name})
		}
		if len(entries) >= 2 {
			result.CustomFormats = append(result.CustomFormats, entries)
		}
	}
	return result, nil
}

// AppData holds all parsed TRaSH data for one app (radarr or sonarr).
type AppData struct {
	CustomFormats map[string]*TrashCF // keyed by trash_id
	CFGroups      []*TrashCFGroup
	Profiles      []*TrashQualityProfile
	ProfileGroups []*ProfileGroup
	QualitySizes  []*TrashQualitySize
	Naming        *TrashNaming
	Conflicts     *ConflictsData // mutually exclusive CFs from conflicts.json (nil if not present)
}

// PullDiff summarizes what changed in the last pull (for GUI display).
type PullDiff struct {
	PrevCommit string `json:"prevCommit"`
	NewCommit  string `json:"newCommit"`
	Summary    string `json:"summary"` // human-readable diff (same format as Discord)
	Time       string `json:"time"`    // ISO 8601 timestamp of when the change was pulled
}

// TrashData holds all parsed TRaSH data + repo metadata.
type TrashData struct {
	LastPull   time.Time
	CommitHash string
	CommitDate string // git commit date (e.g. "2025-06-12 17:04:00 +0200")
	Changelog  []ChangelogSection
	LastDiff   *PullDiff // diff from last pull (nil if no changes)
	Radarr     AppData
	Sonarr     AppData
}

// --- Trash Store ---

// TrashStore manages the TRaSH repo and parsed data.
type TrashStore struct {
	mu                sync.RWMutex // protects data
	pullMu            sync.Mutex   // serializes clone/pull operations (C4)
	data              *TrashData
	dataDir           string                         // path to TRaSH repo clone
	pullError         string                         // last pull error (empty = OK)
	lastChangelogDate string                         // tracks last seen changelog date for new-section detection
	onNewChangelog    func(section ChangelogSection) // called when updates.txt has a new date section
}

// SetOnNewChangelog sets the callback for when updates.txt has a new date section.
func (ts *TrashStore) SetOnNewChangelog(cb func(section ChangelogSection)) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.onNewChangelog = cb
}

func NewTrashStore(dir string) *TrashStore {
	ts := &TrashStore{
		data:    &TrashData{},
		dataDir: filepath.Join(dir, "trash-guides"),
	}
	// Restore last diff from disk (also sets CommitHash so startup pull can detect changes)
	if data, err := os.ReadFile(filepath.Join(dir, "last-pull-diff.json")); err == nil {
		var diff PullDiff
		if json.Unmarshal(data, &diff) == nil {
			ts.data.LastDiff = &diff
			ts.data.CommitHash = diff.NewCommit
		}
	}
	return ts
}

// Snapshot returns an immutable snapshot of current TRaSH data (C1: safe for long-running operations).
func (ts *TrashStore) Snapshot() *TrashData {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.data // immutable: CloneOrPull builds a new *TrashData and swaps the pointer
}

// SetPullError records a pull failure message for the status API.
func (ts *TrashStore) SetPullError(err string) {
	ts.mu.Lock()
	ts.pullError = err
	ts.mu.Unlock()
}

// CurrentCommit returns the current HEAD commit hash (safe for concurrent reads).
func (ts *TrashStore) CurrentCommit() string {
	snap := ts.Snapshot()
	return snap.CommitHash
}

// DataDir returns the path to the TRaSH repo clone directory.
func (ts *TrashStore) DataDir() string {
	return ts.dataDir
}

// DiffChangedFiles returns a human-readable summary of files changed between two commits.
// Groups changes by app type (Radarr/Sonarr) and category (CFs, Profiles, Groups, etc).
// Uses git diff --name-status to show Added/Modified/Deleted per file.
func (ts *TrashStore) DiffChangedFiles(prevCommit, newCommit string) string {
	cmd := exec.Command("git", "-C", ts.dataDir, "diff", "--name-status", prevCommit, newCommit)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		log.Printf("DiffChangedFiles: git diff %s..%s failed: %v (stderr: %s)", prevCommit, newCommit, err, stderr.String())
		return ""
	}

	type change struct {
		name   string
		status string // "Added", "Updated", "Removed"
	}
	type appChanges struct {
		cfs      []change
		profiles []change
		groups   []change
		naming   []change
		other    int
	}
	apps := map[string]*appChanges{}
	descChanges := 0

	statusLabel := func(s string) string {
		switch s {
		case "A":
			return "Added"
		case "D":
			return "Removed"
		default:
			return "Updated"
		}
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		// Format: "M\tdocs/json/radarr/cf/obfuscated.json"
		fields := strings.SplitN(line, "\t", 2)
		if len(fields) != 2 {
			continue
		}
		status := statusLabel(fields[0])
		filePath := fields[1]

		// docs/json/{radarr,sonarr}/{cf,quality-profiles,cf-groups,naming,quality-size}/*.json
		parts := strings.Split(filePath, "/")
		if len(parts) >= 5 && parts[0] == "docs" && parts[1] == "json" {
			appType := parts[2]
			category := parts[3]
			filename := parts[len(parts)-1]

			if apps[appType] == nil {
				apps[appType] = &appChanges{}
			}
			ac := apps[appType]

			// Try to read the "name" field from the JSON file for a readable name
			// (won't work for deleted files, falls back to filename)
			readableName := ts.readJSONName(filepath.Join(ts.dataDir, filePath))
			if readableName == "" {
				readableName = strings.TrimSuffix(filename, ".json")
			}

			c := change{name: readableName, status: status}
			switch category {
			case "cf":
				ac.cfs = append(ac.cfs, c)
			case "quality-profiles":
				ac.profiles = append(ac.profiles, c)
			case "cf-groups":
				ac.groups = append(ac.groups, c)
			case "naming":
				ac.naming = append(ac.naming, c)
			default:
				ac.other++
			}
		} else if len(parts) >= 2 && parts[0] == "includes" && parts[1] == "cf-descriptions" {
			descChanges++
		}
	}

	if len(apps) == 0 && descChanges == 0 {
		return ""
	}

	var sb strings.Builder
	writeChanges := func(label string, changes []change) {
		if len(changes) == 0 {
			return
		}
		sb.WriteString("\n**" + label + ":**")
		for _, c := range changes {
			sb.WriteString(fmt.Sprintf("\n- %s (%s)", c.name, c.status))
		}
	}

	for _, appType := range []string{"radarr", "sonarr"} {
		ac := apps[appType]
		if ac == nil {
			continue
		}
		title := strings.ToUpper(appType[:1]) + appType[1:]
		sb.WriteString("\n**" + title + "**")
		writeChanges("Custom Formats", ac.cfs)
		writeChanges("Profiles", ac.profiles)
		writeChanges("Groups", ac.groups)
		writeChanges("Naming", ac.naming)
		if ac.other > 0 {
			sb.WriteString(fmt.Sprintf("\n**Other:** %d files", ac.other))
		}
	}
	if descChanges > 0 {
		sb.WriteString(fmt.Sprintf("\n**Descriptions:** %d updated", descChanges))
	}

	return sb.String()
}

// readJSONName reads the "name" field from a JSON file, returns empty string on failure.
func (ts *TrashStore) readJSONName(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var obj struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return ""
	}
	return obj.Name
}

// ChangelogEntry represents one change from updates.txt.
type ChangelogEntry struct {
	Type  string `json:"type"`         // "feat", "fix", "refactor", etc.
	Scope string `json:"scope"`        // e.g. "starr-german", "radarr"
	Msg   string `json:"msg"`          // description
	PR    string `json:"pr,omitempty"` // PR number (e.g. "2646")
}

// ChangelogSection groups entries by date.
type ChangelogSection struct {
	Date    string           `json:"date"` // e.g. "2026-03-15"
	Entries []ChangelogEntry `json:"entries"`
}

// Status returns repo status for the API.
type TrashStatus struct {
	LastPull     string             `json:"lastPull"`
	CommitHash   string             `json:"commitHash"`
	CommitDate   string             `json:"commitDate,omitempty"`
	LastDiff     *PullDiff          `json:"lastDiff,omitempty"`  // what changed in last pull
	Changelog    []ChangelogSection `json:"changelog,omitempty"` // recent updates from updates.txt
	RadarrCFs    int                `json:"radarrCFs"`
	SonarrCFs    int                `json:"sonarrCFs"`
	RadarrGroups int                `json:"radarrGroups"`
	SonarrGroups int                `json:"sonarrGroups"`
	RadarrProfs  int                `json:"radarrProfiles"`
	SonarrProfs  int                `json:"sonarrProfiles"`
	Cloned       bool               `json:"cloned"`
	PullError    string             `json:"pullError,omitempty"`
	Pulling      bool               `json:"pulling"`
}

func (ts *TrashStore) Status() TrashStatus {
	// Check pulling status outside the RLock to avoid lock-ordering issues
	pulling := true
	if ts.pullMu.TryLock() {
		pulling = false
		ts.pullMu.Unlock()
	}

	ts.mu.RLock()
	defer ts.mu.RUnlock()

	st := TrashStatus{
		CommitHash:   ts.data.CommitHash,
		CommitDate:   ts.data.CommitDate,
		LastDiff:     ts.data.LastDiff,
		Changelog:    ts.data.Changelog,
		Cloned:       ts.data.CommitHash != "",
		RadarrCFs:    len(ts.data.Radarr.CustomFormats),
		SonarrCFs:    len(ts.data.Sonarr.CustomFormats),
		RadarrGroups: len(ts.data.Radarr.CFGroups),
		SonarrGroups: len(ts.data.Sonarr.CFGroups),
		RadarrProfs:  len(ts.data.Radarr.Profiles),
		SonarrProfs:  len(ts.data.Sonarr.Profiles),
		PullError:    ts.pullError,
		Pulling:      pulling,
	}
	if !ts.data.LastPull.IsZero() {
		st.LastPull = ts.data.LastPull.Format(time.RFC3339)
	}
	return st
}

// changelogEntryRe matches lines like: - [feat(scope): message](url)
var changelogEntryRe = regexp.MustCompile(`^- \[(\w+)\(([^)]+)\):\s*(.+?)\]\(https://github\.com/[^/]+/[^/]+/pull/(\d+)\)`)

// parseChangelog reads updates.txt and returns the most recent maxSections changelog sections.
// Format: "# YYYY-MM-DD HH:MM" headers followed by "- [type(scope): msg](pr-url)" entries.
// Skips chore entries (not useful for end users).
func parseChangelog(path string, maxSections int) []ChangelogSection {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var sections []ChangelogSection
	var current *ChangelogSection

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "# ") {
			// Date header: "# 2026-03-15 02:38"
			dateStr := strings.TrimPrefix(line, "# ")
			if sp := strings.IndexByte(dateStr, ' '); sp > 0 {
				dateStr = dateStr[:sp] // keep only the date part
			}
			if current != nil && len(current.Entries) > 0 {
				sections = append(sections, *current)
				if len(sections) >= maxSections {
					break
				}
			}
			current = &ChangelogSection{Date: dateStr}
			continue
		}

		if current == nil || !strings.HasPrefix(line, "- [") {
			continue
		}

		m := changelogEntryRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		entryType := strings.ToLower(m[1])
		// Skip chore/style/docs — not useful for end users
		if entryType == "chore" || entryType == "style" || entryType == "docs" || entryType == "ci" {
			continue
		}
		current.Entries = append(current.Entries, ChangelogEntry{
			Type:  entryType,
			Scope: m[2],
			Msg:   m[3],
			PR:    m[4],
		})
	}

	// Don't forget the last section
	if current != nil && len(current.Entries) > 0 && len(sections) < maxSections {
		sections = append(sections, *current)
	}

	return sections
}

// CloneOrPull clones or pulls the TRaSH repo, then re-parses all data.
// Serialized via pullMu (C4: prevents concurrent git operations).
func (ts *TrashStore) CloneOrPull(repoURL, branch string) error {
	// C3: Validate inputs to prevent git flag injection
	if strings.HasPrefix(branch, "-") {
		return fmt.Errorf("invalid branch name: %q", branch)
	}
	if !strings.HasPrefix(repoURL, "https://") && !strings.HasPrefix(repoURL, "http://") {
		return fmt.Errorf("invalid repo URL: must start with https:// or http://")
	}

	// C4: Only one pull at a time
	if !ts.pullMu.TryLock() {
		return fmt.Errorf("pull already in progress")
	}
	defer ts.pullMu.Unlock()

	if err := os.MkdirAll(filepath.Dir(ts.dataDir), 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	if _, err := os.Stat(filepath.Join(ts.dataDir, ".git")); err == nil {
		// Disable git auto-gc — it detaches into the background and the orphan
		// gets reparented to PID 1 (clonarr), which doesn't reap unknown children,
		// causing zombie process buildup over time. (v1.8.5 zombie leak fix)
		disableGC := exec.Command("git", "-C", ts.dataDir, "config", "gc.auto", "0")
		if err := disableGC.Run(); err != nil {
			log.Printf("Warning: failed to disable git gc.auto: %v", err)
		}

		// Enable sparse-checkout on existing full clones (migration)
		sparseFile := filepath.Join(ts.dataDir, ".git", "info", "sparse-checkout")
		if _, serr := os.Stat(sparseFile); serr != nil {
			log.Printf("Migrating existing clone to sparse-checkout")
			for _, cmd := range []*exec.Cmd{
				exec.Command("git", "-C", ts.dataDir, "config", "core.sparseCheckout", "true"),
				exec.Command("git", "-C", ts.dataDir, "sparse-checkout", "set", "--no-cone",
					"docs/json/", "docs/updates.txt", "includes/cf-descriptions/"),
			} {
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if err := cmd.Run(); err != nil {
					log.Printf("Warning: sparse-checkout migration: %v", err)
				}
			}
		}

		// Ensure remote URL matches config (user may have changed repo in UI)
		setURL := exec.Command("git", "-C", ts.dataDir, "remote", "set-url", "origin", repoURL)
		setURL.Stdout = os.Stdout
		setURL.Stderr = os.Stderr
		if err := setURL.Run(); err != nil {
			log.Printf("Warning: failed to update remote URL: %v", err)
		}

		// M13: Pull with explicit branch (deepen=1 so previous commit is available for diff)
		log.Printf("Pulling TRaSH repo in %s (branch: %s)", ts.dataDir, branch)
		cmd := exec.Command("git", "-C", ts.dataDir, "fetch", "--deepen=1", "origin", branch)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("git fetch: %w", err)
		}
		cmd = exec.Command("git", "-C", ts.dataDir, "reset", "--hard", "origin/"+branch)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("git reset: %w", err)
		}
	} else {
		// Clone with sparse-checkout — only fetch the files Clonarr needs
		log.Printf("Cloning TRaSH repo %s → %s (sparse-checkout)", repoURL, ts.dataDir)
		cmd := exec.Command("git", "clone", "--depth=1", "--branch", branch,
			"--filter=blob:none", "--sparse", repoURL, ts.dataDir)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("git clone: %w", err)
		}
		cmd = exec.Command("git", "-C", ts.dataDir, "sparse-checkout", "set", "--no-cone",
			"docs/json/", "docs/updates.txt", "includes/cf-descriptions/")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("sparse-checkout set: %w", err)
		}

		// Disable git auto-gc on fresh clone — see migration block above (v1.8.5).
		disableGC := exec.Command("git", "-C", ts.dataDir, "config", "gc.auto", "0")
		if err := disableGC.Run(); err != nil {
			log.Printf("Warning: failed to disable git gc.auto: %v", err)
		}
	}

	// Get commit hash
	hash, err := exec.Command("git", "-C", ts.dataDir, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return fmt.Errorf("get commit hash: %w", err)
	}

	// Parse all data — builds a new *TrashData (C1: old snapshot remains valid)
	data, err := ts.parseAll()
	if err != nil {
		return fmt.Errorf("parse TRaSH data: %w", err)
	}
	data.CommitHash = strings.TrimSpace(string(hash))
	// Get commit date
	commitDate, err := exec.Command("git", "-C", ts.dataDir, "log", "-1", "--format=%ci").Output()
	if err == nil {
		data.CommitDate = strings.TrimSpace(string(commitDate))
	}
	// Parse changelog from updates.txt
	data.Changelog = parseChangelog(filepath.Join(ts.dataDir, "docs", "updates.txt"), 5)
	data.LastPull = time.Now()

	// Generate diff from previous commit (if available)
	ts.mu.RLock()
	prevCommit := ts.data.CommitHash
	prevChangelogDate := ts.lastChangelogDate
	ts.mu.RUnlock()

	if prevCommit != "" && data.CommitHash != prevCommit {
		if diff := ts.DiffChangedFiles(prevCommit, data.CommitHash); diff != "" {
			data.LastDiff = &PullDiff{
				PrevCommit: prevCommit,
				NewCommit:  data.CommitHash,
				Summary:    diff,
				Time:       time.Now().Format(time.RFC3339),
			}
			// Persist to disk so it survives restarts
			if raw, err := json.Marshal(data.LastDiff); err == nil {
				_ = os.WriteFile(filepath.Join(filepath.Dir(ts.dataDir), "last-pull-diff.json"), raw, 0644)
			}
		}
	} else {
		// No changes — preserve previous diff so GUI keeps showing it
		data.LastDiff = ts.data.LastDiff
	}

	// Track changelog date for new-section detection
	newChangelogDate := ""
	if len(data.Changelog) > 0 {
		newChangelogDate = data.Changelog[0].Date
	}

	// Atomic swap
	ts.mu.Lock()
	ts.data = data
	ts.pullError = ""
	ts.lastChangelogDate = newChangelogDate
	ts.mu.Unlock()

	// Notify if changelog has a new section (for separate Discord notification)
	if newChangelogDate != "" && newChangelogDate != prevChangelogDate && prevChangelogDate != "" && ts.onNewChangelog != nil {
		ts.onNewChangelog(data.Changelog[0])
	}

	log.Printf("TRaSH data loaded: Radarr %d CFs / %d profiles, Sonarr %d CFs / %d profiles",
		len(data.Radarr.CustomFormats), len(data.Radarr.Profiles),
		len(data.Sonarr.CustomFormats), len(data.Sonarr.Profiles))

	return nil
}

// parseAll parses all TRaSH JSON data for both radarr and sonarr.
func (ts *TrashStore) parseAll() (*TrashData, error) {
	data := &TrashData{}

	radarr, err := ts.parseAppData("radarr")
	if err != nil {
		return nil, fmt.Errorf("radarr: %w", err)
	}
	data.Radarr = radarr

	sonarr, err := ts.parseAppData("sonarr")
	if err != nil {
		return nil, fmt.Errorf("sonarr: %w", err)
	}
	data.Sonarr = sonarr

	return data, nil
}

// parseAppData parses all JSON for a single app (radarr or sonarr).
func (ts *TrashStore) parseAppData(app string) (AppData, error) {
	base := filepath.Join(ts.dataDir, "docs", "json", app)
	ad := AppData{
		CustomFormats: make(map[string]*TrashCF),
	}

	// Load CF descriptions from includes/cf-descriptions/*.md
	descMap := loadCFDescriptions(ts.dataDir)

	// Parse CFs
	cfDir := filepath.Join(base, "cf")
	if entries, err := os.ReadDir(cfDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			cf, err := parseJSON[TrashCF](filepath.Join(cfDir, e.Name()))
			if err != nil {
				log.Printf("Warning: skip CF %s/%s: %v", app, e.Name(), err)
				continue
			}
			if cf.TrashID != "" {
				// Attach description: try app-specific first (x265-hd-radarr.md), then generic (truehd-atmos.md)
				key := strings.TrimSuffix(e.Name(), ".json")
				if desc, ok := descMap[key+"-"+app]; ok {
					cf.Description = desc
				} else if desc, ok := descMap[key]; ok {
					cf.Description = desc
				}
				ad.CustomFormats[cf.TrashID] = cf
			}
		}
	}

	// Parse CF groups
	groupDir := filepath.Join(base, "cf-groups")
	if entries, err := os.ReadDir(groupDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			g, err := parseJSON[TrashCFGroup](filepath.Join(groupDir, e.Name()))
			if err != nil {
				log.Printf("Warning: skip CF group %s/%s: %v", app, e.Name(), err)
				continue
			}
			ad.CFGroups = append(ad.CFGroups, g)
		}
	}

	// Parse quality profiles
	profDir := filepath.Join(base, "quality-profiles")
	if entries, err := os.ReadDir(profDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			profPath := filepath.Join(profDir, e.Name())
			p, err := parseJSON[TrashQualityProfile](profPath)
			if err != nil {
				log.Printf("Warning: skip profile %s/%s: %v", app, e.Name(), err)
				continue
			}
			// Extract formatItems key order from raw JSON (Go maps lose insertion order)
			if order, err := extractFormatItemsOrder(profPath); err == nil {
				p.FormatItemsOrder = order
			}
			ad.Profiles = append(ad.Profiles, p)
		}
	}

	// Parse profile groups
	groupsFile := filepath.Join(base, "quality-profile-groups", "groups.json")
	if data, err := os.ReadFile(groupsFile); err == nil {
		var groups []*ProfileGroup
		if err := json.Unmarshal(data, &groups); err != nil {
			log.Printf("Warning: skip profile groups %s: %v", app, err)
		} else {
			ad.ProfileGroups = groups
		}
	}

	// Parse quality sizes
	qsDir := filepath.Join(base, "quality-size")
	if entries, err := os.ReadDir(qsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			qs, err := parseJSON[TrashQualitySize](filepath.Join(qsDir, e.Name()))
			if err != nil {
				log.Printf("Warning: skip quality-size %s/%s: %v", app, e.Name(), err)
				continue
			}
			ad.QualitySizes = append(ad.QualitySizes, qs)
		}
	}

	// Parse naming schemes
	namingDir := filepath.Join(base, "naming")
	if entries, err := os.ReadDir(namingDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			n, err := parseJSON[TrashNaming](filepath.Join(namingDir, e.Name()))
			if err != nil {
				log.Printf("Warning: skip naming %s/%s: %v", app, e.Name(), err)
				continue
			}
			ad.Naming = n
			break // one naming file per app
		}
	}

	// Parse conflicts.json (mutually exclusive CFs) — nil if file doesn't exist yet
	conflictsFile := filepath.Join(base, "conflicts.json")
	if data, err := os.ReadFile(conflictsFile); err == nil {
		if conflicts, err := parseConflicts(data); err != nil {
			log.Printf("Warning: skip conflicts %s: %v", app, err)
		} else if len(conflicts.CustomFormats) > 0 {
			ad.Conflicts = conflicts
			log.Printf("Loaded %d conflict groups for %s", len(conflicts.CustomFormats), app)
		}
	}

	return ad, nil
}

// Regex patterns for stripping markdown/HTML from CF descriptions.
var (
	reHTML        = regexp.MustCompile(`<[^>]+>`)
	reMarkdownLn  = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)(\{[^}]*\})?`)
	reComment     = regexp.MustCompile(`<!--[\s\S]*?-->`)
	reTemplate    = regexp.MustCompile(`\{\{[^}]+\}\}`)
	reIncludeMd   = regexp.MustCompile(`\{!\s*include-markdown\s+"[^"]+"\s*!\}`)
	reSnippetIncl = regexp.MustCompile(`--8<--\s+"[^"]+"`)
	reAdmonition  = regexp.MustCompile(`\?\?\?\s+\w+\s+"[^"]*"`)
	reTitleLine   = regexp.MustCompile(`(?m)^\*\*[^*]+\*\*\s*$`)
)

// loadCFDescriptions reads includes/cf-descriptions/*.md and returns a map of
// base filename (without .md) to cleaned description text.
func loadCFDescriptions(repoDir string) map[string]string {
	descDir := filepath.Join(repoDir, "includes", "cf-descriptions")
	entries, err := os.ReadDir(descDir)
	if err != nil {
		return nil
	}

	descs := make(map[string]string, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(descDir, e.Name()))
		if err != nil {
			continue
		}
		text := cleanDescription(string(data))
		if text != "" {
			key := strings.TrimSuffix(e.Name(), ".md")
			descs[key] = text
		}
	}
	return descs
}

// escapeHTML escapes HTML special characters to prevent XSS.
func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

// cleanDescription strips HTML, markdown links, template variables, includes, and comments.
func cleanDescription(raw string) string {
	s := reComment.ReplaceAllString(raw, "")
	s = reHTML.ReplaceAllString(s, "")
	s = reTitleLine.ReplaceAllString(s, "")
	s = reMarkdownLn.ReplaceAllString(s, "$1")
	s = reTemplate.ReplaceAllString(s, "")
	s = reIncludeMd.ReplaceAllString(s, "")
	s = reSnippetIncl.ReplaceAllString(s, "")
	s = reAdmonition.ReplaceAllString(s, "")
	// Strip markdown bold/italic markers
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "__", "")
	// Convert markdown pipe tables to HTML
	lines := strings.Split(s, "\n")
	var cleaned []string
	var tableRows [][]string
	flushTable := func() {
		if len(tableRows) == 0 {
			return
		}
		var buf strings.Builder
		buf.WriteString("<table class='desc-table'>")
		for i, row := range tableRows {
			tag := "td"
			if i == 0 {
				tag = "th"
			}
			buf.WriteString("<tr>")
			for _, cell := range row {
				buf.WriteString("<" + tag + ">" + escapeHTML(strings.TrimSpace(cell)) + "</" + tag + ">")
			}
			buf.WriteString("</tr>")
		}
		buf.WriteString("</table>")
		cleaned = append(cleaned, buf.String())
		tableRows = nil
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			flushTable()
			continue
		}
		if strings.HasPrefix(line, "|") && strings.HasSuffix(line, "|") {
			// Skip separator rows (|---|---|)
			inner := strings.Trim(line, "|")
			if strings.Trim(strings.ReplaceAll(strings.ReplaceAll(inner, "-", ""), "|", ""), " :") == "" {
				continue
			}
			cells := strings.Split(inner, "|")
			tableRows = append(tableRows, cells)
		} else {
			flushTable()
			cleaned = append(cleaned, line)
		}
	}
	flushTable()
	return strings.Join(cleaned, "\n")
}

// --- CF Picker (Profile Builder) ---

// CategorizedCF is a CF with all score contexts for the CF picker.
type CategorizedCF struct {
	TrashID     string         `json:"trashId"`
	Name        string         `json:"name"`
	TrashScores map[string]int `json:"trashScores"`
	Description string         `json:"description,omitempty"`
	IsCustom    bool           `json:"isCustom,omitempty"`
	Required    bool           `json:"required,omitempty"`  // from CF group: required=true means must-include
	CFDefault   *bool          `json:"cfDefault,omitempty"` // from CF group: per-CF default override
}

// CFPickerGroup is a CF group within a picker category, carrying group metadata + all score contexts.
type CFPickerGroup struct {
	Name             string          `json:"name"`
	ShortName        string          `json:"shortName"`
	GroupTrashID     string          `json:"groupTrashId"`
	TrashDescription string          `json:"trashDescription,omitempty"`
	DefaultEnabled   bool            `json:"defaultEnabled"`
	Exclusive        bool            `json:"exclusive"`
	IncludeProfiles  []string        `json:"includeProfiles,omitempty"`
	CFs              []CategorizedCF `json:"cfs"`
}

// CFPickerCategory groups CF groups by category for the profile builder.
type CFPickerCategory struct {
	Category string          `json:"category"`
	Groups   []CFPickerGroup `json:"groups"`
}

// CFPickerData is the full response for the CF picker endpoint.
type CFPickerData struct {
	Categories []CFPickerCategory `json:"categories"`
	ScoreSets  []string           `json:"scoreSets"`
}

// AllCFsCategorized returns all TRaSH CFs organized by Category > Group > CF hierarchy.
// Uses the same grouping logic as ProfileCFCategories but includes ALL groups (not profile-filtered)
// and carries all score contexts per CF (for score set switching in the builder).
// Custom CFs (user-imported) are injected into their assigned categories.
func AllCFsCategorized(ad *AppData, customCFs []CustomCF) *CFPickerData {
	scoreSets := make(map[string]bool)
	groupedCFs := make(map[string]bool) // track CFs that are in at least one group

	// Collect score sets from all CFs
	for _, cf := range ad.CustomFormats {
		for ctx := range cf.TrashScores {
			scoreSets[ctx] = true
		}
	}

	// Build Category > Group structure from CF groups
	type catKey struct{ cat, groupID string }
	catGroupMap := make(map[string][]CFPickerGroup) // category → groups

	for _, group := range ad.CFGroups {
		category, shortName := ParseCategoryPrefix(group.Name)

		// Same defaultEnabled logic as ProfileCFCategories
		defaultEnabled := group.Default == "true"
		if category == "Streaming Services" && shortName != "General" {
			defaultEnabled = false
		}

		// Detect exclusive groups
		exclusive := strings.Contains(strings.ToLower(group.TrashDescription), "only score or enable one") ||
			strings.Contains(strings.ToLower(group.TrashDescription), "only enable one")

		// Extract profile names from include list
		var includeProfiles []string
		for profName := range group.QualityProfiles.Include {
			includeProfiles = append(includeProfiles, profName)
		}
		sort.Strings(includeProfiles)

		pg := CFPickerGroup{
			Name:             group.Name,
			ShortName:        shortName,
			GroupTrashID:     group.TrashID,
			TrashDescription: group.TrashDescription,
			DefaultEnabled:   defaultEnabled,
			Exclusive:        exclusive,
			IncludeProfiles:  includeProfiles,
		}

		for _, cfEntry := range group.CustomFormats {
			cf, ok := ad.CustomFormats[cfEntry.TrashID]
			if !ok {
				continue
			}
			ccf := CategorizedCF{
				TrashID:     cfEntry.TrashID,
				Name:        cfEntry.Name,
				TrashScores: cf.TrashScores,
				Description: cf.Description,
				Required:    cfEntry.Required,
			}
			if cfEntry.Default != nil {
				ccf.CFDefault = cfEntry.Default
			}
			pg.CFs = append(pg.CFs, ccf)
			groupedCFs[cfEntry.TrashID] = true
		}

		if len(pg.CFs) == 0 {
			continue
		}

		catGroupMap[category] = append(catGroupMap[category], pg)
	}

	// Collect ungrouped CFs into synthetic "Other" groups per category
	ungrouped := make(map[string][]CategorizedCF) // category → CFs
	for trashID, cf := range ad.CustomFormats {
		if groupedCFs[trashID] {
			continue
		}
		cat := categorizeCFByName(cf.Name)
		ungrouped[cat] = append(ungrouped[cat], CategorizedCF{
			TrashID:     trashID,
			Name:        cf.Name,
			TrashScores: cf.TrashScores,
			Description: cf.Description,
		})
	}
	for cat, cfs := range ungrouped {
		sort.Slice(cfs, func(i, j int) bool { return cfs[i].Name < cfs[j].Name })
		catGroupMap[cat] = append(catGroupMap[cat], CFPickerGroup{
			Name:      "Other",
			ShortName: "Other",
			CFs:       cfs,
		})
	}

	// Inject custom CFs into a "Custom" group in their assigned category
	customByCat := make(map[string][]CategorizedCF)
	for _, ccf := range customCFs {
		cat := ccf.Category
		if cat == "" {
			cat = "Custom"
		}
		customByCat[cat] = append(customByCat[cat], CategorizedCF{
			TrashID:  ccf.ID,
			Name:     ccf.Name,
			IsCustom: true,
		})
	}
	for cat, cfs := range customByCat {
		sort.Slice(cfs, func(i, j int) bool { return cfs[i].Name < cfs[j].Name })
		catGroupMap[cat] = append(catGroupMap[cat], CFPickerGroup{
			Name:      "Custom",
			ShortName: "Custom",
			CFs:       cfs,
		})
	}

	// Build sorted categories
	var categories []CFPickerCategory
	for cat, groups := range catGroupMap {
		// Sort groups: defaultEnabled first, then alphabetically
		sort.Slice(groups, func(i, j int) bool {
			if groups[i].DefaultEnabled != groups[j].DefaultEnabled {
				return groups[i].DefaultEnabled
			}
			return groups[i].ShortName < groups[j].ShortName
		})
		categories = append(categories, CFPickerCategory{
			Category: cat,
			Groups:   groups,
		})
	}
	sort.Slice(categories, func(i, j int) bool {
		return GetCategoryOrder(categories[i].Category) < GetCategoryOrder(categories[j].Category)
	})

	var sets []string
	for s := range scoreSets {
		sets = append(sets, s)
	}
	sort.Strings(sets)

	return &CFPickerData{Categories: categories, ScoreSets: sets}
}

// QualityPreset is a unique quality configuration extracted from TRaSH profiles.
type QualityPreset struct {
	ID      string        `json:"id"`      // trash_id of representative profile
	Name    string        `json:"name"`    // e.g. "Remux + WEB 2160p"
	Cutoff  string        `json:"cutoff"`  // e.g. "Remux-2160p"
	Items   []QualityItem `json:"items"`   // full quality item list
	Allowed []string      `json:"allowed"` // names of allowed qualities/groups (for display)
}

// QualityPresets extracts unique quality configurations from all TRaSH profiles.
// Profiles with identical quality items are deduplicated; the first match is used as representative.
func QualityPresets(ad *AppData) []QualityPreset {
	seen := make(map[string]bool)
	var presets []QualityPreset

	for _, p := range ad.Profiles {
		if len(p.Items) == 0 {
			continue
		}
		// Build signature from allowed items + cutoff
		var allowedNames []string
		for _, item := range p.Items {
			if item.Allowed {
				allowedNames = append(allowedNames, item.Name)
			}
		}
		key := p.Cutoff + "|" + strings.Join(allowedNames, ",")
		if seen[key] {
			continue
		}
		seen[key] = true

		presets = append(presets, QualityPreset{
			ID:      p.TrashID,
			Name:    p.Name,
			Cutoff:  p.Cutoff,
			Items:   p.Items,
			Allowed: allowedNames,
		})
	}

	sort.Slice(presets, func(i, j int) bool {
		return presets[i].Name < presets[j].Name
	})
	return presets
}

// extractFormatItemsOrder reads a TRaSH profile JSON and returns formatItems keys in original order.
// Uses json.Decoder token-by-token parsing to preserve insertion order.
func extractFormatItemsOrder(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	// Find "formatItems" key
	depth := 0
	found := false
	for {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch v := t.(type) {
		case json.Delim:
			if v == '{' || v == '[' {
				depth++
			} else {
				depth--
			}
		case string:
			if depth == 1 && v == "formatItems" {
				found = true
			}
		}
		if found {
			break
		}
	}
	if !found {
		return nil, nil
	}
	// Read the opening { of formatItems object
	t, err := dec.Token()
	if err != nil || t != json.Delim('{') {
		return nil, nil
	}
	// Read keys in order
	var order []string
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			break
		}
		if key, ok := t.(string); ok {
			order = append(order, key)
			// Skip the value — assumes scalar (string). TRaSH formatItems are always {"name": "trash_id"}.
			dec.Token() //nolint: errcheck
		}
	}
	return order, nil
}

// parseJSON reads a JSON file and unmarshals into T.
func parseJSON[T any](path string) (*T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// GetAppData returns AppData from a stable snapshot (safe for use after call returns).
func (ts *TrashStore) GetAppData(app string) *AppData {
	snap := ts.Snapshot()
	return SnapshotAppData(snap, app)
}

// SnapshotAppData extracts a deep copy of AppData from an existing snapshot (no lock needed).
// Deep-copies maps and slices so callers cannot mutate the snapshot.
func SnapshotAppData(snap *TrashData, app string) *AppData {
	var src *AppData
	switch app {
	case "radarr":
		src = &snap.Radarr
	case "sonarr":
		src = &snap.Sonarr
	default:
		return nil
	}
	cp := *src
	if src.CustomFormats != nil {
		cp.CustomFormats = make(map[string]*TrashCF, len(src.CustomFormats))
		for k, v := range src.CustomFormats {
			cp.CustomFormats[k] = v
		}
	}
	if src.CFGroups != nil {
		cp.CFGroups = make([]*TrashCFGroup, len(src.CFGroups))
		copy(cp.CFGroups, src.CFGroups)
	}
	if src.Profiles != nil {
		cp.Profiles = make([]*TrashQualityProfile, len(src.Profiles))
		copy(cp.Profiles, src.Profiles)
	}
	if src.ProfileGroups != nil {
		cp.ProfileGroups = make([]*ProfileGroup, len(src.ProfileGroups))
		copy(cp.ProfileGroups, src.ProfileGroups)
	}
	if src.QualitySizes != nil {
		cp.QualitySizes = make([]*TrashQualitySize, len(src.QualitySizes))
		copy(cp.QualitySizes, src.QualitySizes)
	}
	if src.Naming != nil {
		n := *src.Naming
		cp.Naming = &n
	}
	return &cp
}

// ResolvedCF is a single CF with resolved score.
type ResolvedCF struct {
	TrashID     string `json:"trashId"`
	Name        string `json:"name"`
	Score       int    `json:"score"`
	HasScore    bool   `json:"hasScore"`
	Description string `json:"description,omitempty"`
}

// ResolvedCFCategory groups resolved CFs by category.
type ResolvedCFCategory struct {
	Category string       `json:"category"`
	CFs      []ResolvedCF `json:"cfs"`
}

// ProfileCFGroupEntry is a CF within a group, with resolved score and default flag.
type ProfileCFGroupEntry struct {
	TrashID     string `json:"trashId"`
	Name        string `json:"name"`
	Score       int    `json:"score"`
	HasScore    bool   `json:"hasScore"`
	Required    bool   `json:"required"`
	Default     bool   `json:"default"`
	Description string `json:"description,omitempty"`
}

// ProfileCFGroup is a CF group linked to a profile via quality_profiles.include.
type ProfileCFGroup struct {
	Name             string                `json:"name"`
	TrashDescription string                `json:"trashDescription"`
	Required         bool                  `json:"required"` // true if all CFs in group have required=true
	CFs              []ProfileCFGroupEntry `json:"cfs"`
}

// ProfileCFGroups returns CF groups that are linked to a profile via quality_profiles.include,
// split into required and optional groups, with resolved scores.
func ProfileCFGroups(ad *AppData, profileTrashID string) (required []ProfileCFGroup, optional []ProfileCFGroup) {
	if ad == nil {
		return nil, nil
	}

	var profile *TrashQualityProfile
	for _, p := range ad.Profiles {
		if p.TrashID == profileTrashID {
			profile = p
			break
		}
	}
	if profile == nil {
		return nil, nil
	}

	scoreCtx := profile.TrashScoreSet
	if scoreCtx == "" {
		scoreCtx = "default"
	}

	// Build set of CFs already in profile's formatItems (core CFs)
	coreCFs := make(map[string]bool)
	for _, cfTrashID := range profile.FormatItems {
		coreCFs[cfTrashID] = true
	}

	for _, group := range ad.CFGroups {
		// Check if this group's quality_profiles.include references our profile
		found := false
		for _, profTrashID := range group.QualityProfiles.Include {
			if profTrashID == profileTrashID {
				found = true
				break
			}
		}
		if !found {
			continue
		}

		pg := ProfileCFGroup{
			Name:             group.Name,
			TrashDescription: group.TrashDescription,
		}

		// Determine required/optional from ALL CFs in the group (not just filtered)
		allRequired := true
		for _, cfEntry := range group.CustomFormats {
			if !cfEntry.Required {
				allRequired = false
				break
			}
		}

		for _, cfEntry := range group.CustomFormats {
			// Skip CFs that are already in core formatItems
			if coreCFs[cfEntry.TrashID] {
				continue
			}

			entry := ProfileCFGroupEntry{
				TrashID:  cfEntry.TrashID,
				Name:     cfEntry.Name,
				Required: cfEntry.Required,
			}
			if cfEntry.Default != nil && *cfEntry.Default {
				entry.Default = true
			}

			// Resolve score + description
			if cf, ok := ad.CustomFormats[cfEntry.TrashID]; ok {
				if s, ok := cf.TrashScores[scoreCtx]; ok {
					entry.Score = s
					entry.HasScore = true
				} else if s, ok := cf.TrashScores["default"]; ok {
					entry.Score = s
					entry.HasScore = true
				}
				entry.Description = cf.Description
			}

			pg.CFs = append(pg.CFs, entry)
		}

		if len(pg.CFs) == 0 {
			continue
		}

		pg.Required = allRequired
		if allRequired {
			required = append(required, pg)
		} else {
			optional = append(optional, pg)
		}
	}

	return required, optional
}

// --- Category-based CF Group Classification ---

// CategoryCFGroup is a CF group with its category parsed from the name prefix.
type CategoryCFGroup struct {
	Name             string                `json:"name"`      // "[Audio] Audio Formats"
	ShortName        string                `json:"shortName"` // "Audio Formats"
	Category         string                `json:"category"`  // "Audio"
	TrashDescription string                `json:"trashDescription"`
	DefaultEnabled   bool                  `json:"defaultEnabled"` // group.Default == "true"
	Exclusive        bool                  `json:"exclusive"`      // only one CF can be active (Golden Rule)
	CFs              []ProfileCFGroupEntry `json:"cfs"`
}

// CFCategory groups CategoryCFGroups under a shared category name.
type CFCategory struct {
	Category string            `json:"category"`
	Groups   []CategoryCFGroup `json:"groups"`
}

// parseCategoryPrefix extracts "[Category] Short Name" from a group name.
// Returns (category, shortName). If no prefix found, returns ("Other", fullName).
func ParseCategoryPrefix(name string) (string, string) {
	if !strings.HasPrefix(name, "[") {
		return "Other", name
	}
	idx := strings.Index(name, "]")
	if idx < 0 {
		return "Other", name
	}
	category := strings.TrimSpace(name[1:idx])
	shortName := strings.TrimSpace(name[idx+1:])
	if category == "" {
		return "Other", name
	}
	if shortName == "" {
		shortName = category
	}

	// Remap categories for better UI organization
	switch category {
	case "Required":
		category = "Golden Rule"
	case "SQP":
		// SQP-specific CFs (e.g. "Disable if one Radarr") belong in Miscellaneous
		category = "Miscellaneous"
	}

	return category, shortName
}

// getCategoryOrder returns the display order for a CF category.
func GetCategoryOrder(cat string) int {
	switch cat {
	case "Golden Rule":
		return 0
	case "Audio":
		return 1
	case "HDR Formats":
		return 2
	case "HQ Release Groups":
		return 3
	case "Resolution":
		return 4
	case "Streaming Services":
		return 5
	case "Miscellaneous":
		return 6
	case "Optional":
		return 7
	case "Release Groups":
		return 8
	case "Unwanted":
		return 9
	case "Movie Versions":
		return 10
	case "Anime":
		return 11
	case "French Audio Version":
		return 12
	case "French HQ Source Groups":
		return 13
	case "German Source Groups":
		return 14
	case "German Miscellaneous":
		return 15
	case "Language Profiles":
		return 16
	case "SQP":
		return 17
	case "Custom":
		return 18
	default:
		return 99
	}
}

// ProfileCFCategories returns CF groups linked to a profile, organized by category.
// Uses the [Prefix] in group names to classify. Does not modify ProfileCFGroups().
func ProfileCFCategories(ad *AppData, profileTrashID string) []CFCategory {
	if ad == nil {
		return nil
	}

	var profile *TrashQualityProfile
	for _, p := range ad.Profiles {
		if p.TrashID == profileTrashID {
			profile = p
			break
		}
	}
	if profile == nil {
		return nil
	}

	scoreCtx := profile.TrashScoreSet
	if scoreCtx == "" {
		scoreCtx = "default"
	}

	// Build set of CFs already in profile's formatItems (core CFs)
	coreCFs := make(map[string]bool)
	for _, cfTrashID := range profile.FormatItems {
		coreCFs[cfTrashID] = true
	}

	// Build category → groups map
	catMap := make(map[string][]CategoryCFGroup)

	for _, group := range ad.CFGroups {
		// Check if this group's quality_profiles.include references our profile
		found := false
		for _, profTrashID := range group.QualityProfiles.Include {
			if profTrashID == profileTrashID {
				found = true
				break
			}
		}
		if !found {
			continue
		}

		category, shortName := ParseCategoryPrefix(group.Name)

		// For Streaming Services, only "General" is enabled by default
		defaultEnabled := group.Default == "true"
		if category == "Streaming Services" && shortName != "General" {
			defaultEnabled = false
		}

		// Detect exclusive groups (Golden Rule: "only score or enable one")
		exclusive := strings.Contains(strings.ToLower(group.TrashDescription), "only score or enable one") ||
			strings.Contains(strings.ToLower(group.TrashDescription), "only enable one")

		cg := CategoryCFGroup{
			Name:             group.Name,
			ShortName:        shortName,
			Category:         category,
			TrashDescription: group.TrashDescription,
			DefaultEnabled:   defaultEnabled,
			Exclusive:        exclusive,
		}

		for _, cfEntry := range group.CustomFormats {
			// Skip CFs that are already in core formatItems
			if coreCFs[cfEntry.TrashID] {
				continue
			}

			entry := ProfileCFGroupEntry{
				TrashID:  cfEntry.TrashID,
				Name:     cfEntry.Name,
				Required: cfEntry.Required,
			}
			if cfEntry.Default != nil && *cfEntry.Default {
				entry.Default = true
			}

			// Resolve score + description
			if cf, ok := ad.CustomFormats[cfEntry.TrashID]; ok {
				if s, ok := cf.TrashScores[scoreCtx]; ok {
					entry.Score = s
					entry.HasScore = true
				} else if s, ok := cf.TrashScores["default"]; ok {
					entry.Score = s
					entry.HasScore = true
				}
				entry.Description = cf.Description
			}

			cg.CFs = append(cg.CFs, entry)
		}

		if len(cg.CFs) == 0 {
			continue
		}

		catMap[category] = append(catMap[category], cg)
	}

	// Build sorted result
	var categories []CFCategory
	for cat, groups := range catMap {
		// Sort groups: defaultEnabled first, then alphabetically by shortName
		sort.Slice(groups, func(i, j int) bool {
			if groups[i].DefaultEnabled != groups[j].DefaultEnabled {
				return groups[i].DefaultEnabled
			}
			return groups[i].ShortName < groups[j].ShortName
		})
		categories = append(categories, CFCategory{
			Category: cat,
			Groups:   groups,
		})
	}

	sort.Slice(categories, func(i, j int) bool {
		return GetCategoryOrder(categories[i].Category) < GetCategoryOrder(categories[j].Category)
	})

	return categories
}

// ProfileDetailResult holds all data needed for the sync/detail view.
type ProfileDetailResult struct {
	// FormatItemNames are CFs in formatItems that do NOT belong to any TRaSH CF group.
	// Displayed as compact multi-column "Profile" section (just names, no toggles).
	FormatItemNames []ResolvedCF `json:"formatItemNames"`
	// Groups are all TRaSH CF groups linked to this profile, as a flat list.
	// Includes both required and optional groups — NOT filtered by formatItems.
	Groups []CategoryCFGroup `json:"groups"`
}

// ProfileDetailData builds the sync/detail view data using TRaSH CF groups.
// formatItems CFs that also appear in a group are shown in the group (not in Profile section).
// formatItems CFs that don't belong to any group are returned in FormatItemNames.
func ProfileDetailData(ad *AppData, profileTrashID string) *ProfileDetailResult {
	if ad == nil {
		return nil
	}

	var profile *TrashQualityProfile
	for _, p := range ad.Profiles {
		if p.TrashID == profileTrashID {
			profile = p
			break
		}
	}
	if profile == nil {
		return nil
	}

	scoreCtx := profile.TrashScoreSet
	if scoreCtx == "" {
		scoreCtx = "default"
	}

	// Build set of formatItems CFs
	formatItemSet := make(map[string]bool)
	for _, cfTrashID := range profile.FormatItems {
		formatItemSet[cfTrashID] = true
	}

	// Track which formatItems CFs appear in at least one group
	formatItemInGroup := make(map[string]bool)

	// Build flat list of TRaSH groups (do NOT skip formatItems CFs)
	groups := make([]CategoryCFGroup, 0)
	for _, group := range ad.CFGroups {
		// Check if this group's quality_profiles.include references our profile
		found := false
		for _, profTrashID := range group.QualityProfiles.Include {
			if profTrashID == profileTrashID {
				found = true
				break
			}
		}
		if !found {
			continue
		}

		category, shortName := ParseCategoryPrefix(group.Name)

		defaultEnabled := group.Default == "true"
		if category == "Streaming Services" && shortName != "General" {
			defaultEnabled = false
		}

		exclusive := strings.Contains(strings.ToLower(group.TrashDescription), "only score or enable one") ||
			strings.Contains(strings.ToLower(group.TrashDescription), "only enable one")

		cg := CategoryCFGroup{
			Name:             group.Name,
			ShortName:        shortName,
			Category:         category,
			TrashDescription: group.TrashDescription,
			DefaultEnabled:   defaultEnabled,
			Exclusive:        exclusive,
		}

		for _, cfEntry := range group.CustomFormats {
			entry := ProfileCFGroupEntry{
				TrashID:  cfEntry.TrashID,
				Name:     cfEntry.Name,
				Required: cfEntry.Required,
			}
			if cfEntry.Default != nil && *cfEntry.Default {
				entry.Default = true
			}

			if cf, ok := ad.CustomFormats[cfEntry.TrashID]; ok {
				if s, ok := cf.TrashScores[scoreCtx]; ok {
					entry.Score = s
					entry.HasScore = true
				} else if s, ok := cf.TrashScores["default"]; ok {
					entry.Score = s
					entry.HasScore = true
				}
				entry.Description = cf.Description
			}

			cg.CFs = append(cg.CFs, entry)

			// Track that this formatItem CF is covered by a group
			if formatItemSet[cfEntry.TrashID] {
				formatItemInGroup[cfEntry.TrashID] = true
			}
		}

		if len(cg.CFs) == 0 {
			continue
		}

		groups = append(groups, cg)
	}

	// Sort groups by category order, then defaultEnabled first, then name
	sort.Slice(groups, func(i, j int) bool {
		oi := GetCategoryOrder(groups[i].Category)
		oj := GetCategoryOrder(groups[j].Category)
		if oi != oj {
			return oi < oj
		}
		if groups[i].DefaultEnabled != groups[j].DefaultEnabled {
			return groups[i].DefaultEnabled
		}
		return groups[i].Name < groups[j].Name
	})

	// formatItems CFs NOT in any group → "Profile" section
	formatItemNames := make([]ResolvedCF, 0)
	for cfName, cfTrashID := range profile.FormatItems {
		if formatItemInGroup[cfTrashID] {
			continue
		}
		rc := ResolvedCF{
			TrashID: cfTrashID,
			Name:    cfName,
		}
		if cf, ok := ad.CustomFormats[cfTrashID]; ok {
			rc.Name = cf.Name
			if s, ok := cf.TrashScores[scoreCtx]; ok {
				rc.Score = s
				rc.HasScore = true
			} else if s, ok := cf.TrashScores["default"]; ok {
				rc.Score = s
				rc.HasScore = true
			}
		}
		formatItemNames = append(formatItemNames, rc)
	}
	// Sort by score descending, then name
	sort.Slice(formatItemNames, func(i, j int) bool {
		if formatItemNames[i].Score != formatItemNames[j].Score {
			return formatItemNames[i].Score > formatItemNames[j].Score
		}
		return formatItemNames[i].Name < formatItemNames[j].Name
	})

	return &ProfileDetailResult{
		FormatItemNames: formatItemNames,
		Groups:          groups,
	}
}

// ImportedProfileDetailData builds the same ProfileDetailResult as ProfileDetailData,
// but for imported profiles that don't have a trashProfileId. Instead of using
// QualityProfiles.Include to filter groups, it includes any group that has at least
// one CF present in the imported profile's FormatItems.
func ImportedProfileDetailData(ad *AppData, imported *ImportedProfile) *ProfileDetailResult {
	if ad == nil || imported == nil {
		return nil
	}

	formatItems := imported.FormatItems
	if formatItems == nil {
		formatItems = make(map[string]int)
	}

	scoreCtx := imported.ScoreSet
	if scoreCtx == "" {
		scoreCtx = "default"
	}

	// Build set of imported CF trash_ids for fast lookup
	importedSet := make(map[string]bool, len(formatItems))
	for tid := range formatItems {
		importedSet[tid] = true
	}

	// If profile has a goldenRuleVariant, find the matching TRaSH profile
	// and use QualityProfiles.Include to filter groups (same as TRaSH Sync).
	var includeProfileID string
	if imported.TrashProfileID != "" {
		includeProfileID = imported.TrashProfileID
	} else if imported.VariantGoldenRule != "" {
		// Match based on golden rule variant + qualities
		for _, tp := range ad.Profiles {
			nameL := strings.ToLower(tp.Name)
			variant := strings.ToLower(imported.VariantGoldenRule)
			if variant == "uhd" && (strings.Contains(nameL, "uhd") || strings.Contains(nameL, "2160p")) &&
				!strings.Contains(nameL, "anime") && !strings.Contains(nameL, "french") && !strings.Contains(nameL, "german") {
				includeProfileID = tp.TrashID
				break
			}
			if variant == "hd" && !strings.Contains(nameL, "uhd") && !strings.Contains(nameL, "2160p") &&
				!strings.Contains(nameL, "anime") && !strings.Contains(nameL, "french") && !strings.Contains(nameL, "german") {
				includeProfileID = tp.TrashID
				break
			}
		}
	}

	// Track which CFs appear in at least one group
	cfInGroup := make(map[string]bool)

	groups := make([]CategoryCFGroup, 0)
	for _, group := range ad.CFGroups {
		// Filter groups: if we have a profile match, use QualityProfiles.Include
		if includeProfileID != "" {
			found := false
			for _, profTrashID := range group.QualityProfiles.Include {
				if profTrashID == includeProfileID {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		} else {
			// No profile match — include group if ANY of its CFs exist in the imported profile
			hasMatch := false
			for _, cfEntry := range group.CustomFormats {
				if importedSet[cfEntry.TrashID] {
					hasMatch = true
					break
				}
			}
			if !hasMatch {
				continue
			}
		}

		category, shortName := ParseCategoryPrefix(group.Name)

		exclusive := strings.Contains(strings.ToLower(group.TrashDescription), "only score or enable one") ||
			strings.Contains(strings.ToLower(group.TrashDescription), "only enable one")

		cg := CategoryCFGroup{
			Name:             group.Name,
			ShortName:        shortName,
			Category:         category,
			TrashDescription: group.TrashDescription,
			DefaultEnabled:   true,
			Exclusive:        exclusive,
		}

		for _, cfEntry := range group.CustomFormats {
			entry := ProfileCFGroupEntry{
				TrashID:  cfEntry.TrashID,
				Name:     cfEntry.Name,
				Required: cfEntry.Required,
			}
			if cfEntry.Default != nil && *cfEntry.Default {
				entry.Default = true
			}

			// Score: use imported profile's score if CF is in profile, else TRaSH default
			if score, ok := formatItems[cfEntry.TrashID]; ok {
				entry.Score = score
				entry.HasScore = true
			} else if cf, ok := ad.CustomFormats[cfEntry.TrashID]; ok {
				if s, ok := cf.TrashScores[scoreCtx]; ok {
					entry.Score = s
					entry.HasScore = true
				} else if s, ok := cf.TrashScores["default"]; ok {
					entry.Score = s
					entry.HasScore = true
				}
			}

			if cf, ok := ad.CustomFormats[cfEntry.TrashID]; ok {
				entry.Description = cf.Description
			}

			cg.CFs = append(cg.CFs, entry)
			cfInGroup[cfEntry.TrashID] = true
		}

		if len(cg.CFs) == 0 {
			continue
		}

		groups = append(groups, cg)
	}

	// Sort groups by category order, then name
	sort.Slice(groups, func(i, j int) bool {
		oi := GetCategoryOrder(groups[i].Category)
		oj := GetCategoryOrder(groups[j].Category)
		if oi != oj {
			return oi < oj
		}
		if groups[i].DefaultEnabled != groups[j].DefaultEnabled {
			return groups[i].DefaultEnabled
		}
		return groups[i].Name < groups[j].Name
	})

	// CFs in imported profile but NOT in any group → formatItemNames
	formatItemNames := make([]ResolvedCF, 0)
	for tid, score := range formatItems {
		if cfInGroup[tid] {
			continue
		}
		name := tid
		if len(tid) > 8 {
			name = tid[:8] + "..."
		}
		if imported.FormatComments != nil {
			if n, ok := imported.FormatComments[tid]; ok && n != "" {
				name = n
			}
		}
		if cf, ok := ad.CustomFormats[tid]; ok {
			name = cf.Name
		}
		formatItemNames = append(formatItemNames, ResolvedCF{
			TrashID:  tid,
			Name:     name,
			Score:    score,
			HasScore: true,
		})
	}
	sort.Slice(formatItemNames, func(i, j int) bool {
		if formatItemNames[i].Score != formatItemNames[j].Score {
			return formatItemNames[i].Score > formatItemNames[j].Score
		}
		return formatItemNames[i].Name < formatItemNames[j].Name
	})

	return &ProfileDetailResult{
		FormatItemNames: formatItemNames,
		Groups:          groups,
	}
}

func ResolveProfileCFs(ad *AppData, profileTrashID string) ([]ResolvedCF, string) {
	if ad == nil {
		return nil, ""
	}

	// Find the profile
	var profile *TrashQualityProfile
	for _, p := range ad.Profiles {
		if p.TrashID == profileTrashID {
			profile = p
			break
		}
	}
	if profile == nil {
		return nil, ""
	}

	scoreCtx := profile.TrashScoreSet
	if scoreCtx == "" {
		scoreCtx = "default"
	}

	var resolved []ResolvedCF
	for cfName, cfTrashID := range profile.FormatItems {
		cf, ok := ad.CustomFormats[cfTrashID]
		if !ok {
			resolved = append(resolved, ResolvedCF{
				TrashID: cfTrashID,
				Name:    cfName,
			})
			continue
		}

		rc := ResolvedCF{
			TrashID:     cfTrashID,
			Name:        cf.Name,
			Description: cf.Description,
		}
		if score, ok := cf.TrashScores[scoreCtx]; ok {
			rc.Score = score
			rc.HasScore = true
		} else if score, ok := cf.TrashScores["default"]; ok {
			rc.Score = score
			rc.HasScore = true
		}
		resolved = append(resolved, rc)
	}

	return resolved, scoreCtx
}

// categorizeCFByName returns a category for a CF based on its name,
// matching TRaSH's collection-of-custom-formats page structure.
func categorizeCFByName(name string) string {
	n := strings.ToLower(name)

	// French HQ Source Groups (before generic HQ Release Groups — "fr remux tier" starts with "fr")
	if strings.HasPrefix(n, "fr ") {
		return "French HQ Source Groups"
	}

	// German Source Groups & German Miscellaneous (before generic checks)
	if n == "german" || strings.HasPrefix(n, "german ") || strings.HasPrefix(n, "not german") {
		for _, m := range []string{"german lq", "german lq (release title)", "german microsized",
			"german 1080p booster", "german 2160p booster"} {
			if n == m {
				return "German Miscellaneous"
			}
		}
		return "German Source Groups"
	}

	// HQ Release Groups
	for _, prefix := range []string{"remux tier", "uhd bluray tier", "hd bluray tier", "web tier"} {
		if strings.HasPrefix(n, prefix) {
			return "HQ Release Groups"
		}
	}

	// Anime (includes v0-v4, 10bit, uncensored, dubs only — per TRaSH structure)
	if strings.HasPrefix(n, "anime ") {
		return "Anime"
	}
	for _, a := range []string{"v0", "v1", "v2", "v3", "v4", "10bit", "uncensored", "dubs only", "anime dual audio"} {
		if n == a {
			return "Anime"
		}
	}

	// French Audio Version
	for _, f := range []string{"vff", "vof", "vfi", "vf2", "vfq", "voq", "vq", "vfb", "vostfr", "fansub", "fastsub"} {
		if n == f {
			return "French Audio Version"
		}
	}

	// Language Profiles
	if strings.HasPrefix(n, "language:") || n == "wrong language" {
		return "Language Profiles"
	}

	// Streaming Services — individual services not in CF groups
	for _, s := range []string{"vrv", "abema", "adn", "b-global", "bilibili", "cr", "funi", "hidive", "wkn"} {
		if n == s {
			return "Streaming Services"
		}
	}

	// Series/Movie Versions
	for _, v := range []string{"hybrid", "remaster"} {
		if n == v {
			return "Movie Versions"
		}
	}

	// Unwanted
	for _, u := range []string{"br-disk", "br-disk (btn)", "lq", "lq (release title)", "3d", "upscaled", "extras",
		"generated dynamic hdr", "sing-along versions", "av1", "x265 (hd)"} {
		if n == u {
			return "Unwanted"
		}
	}

	// Audio (formats + channels)
	for _, a := range []string{"1.0 mono", "2.0 stereo", "3.0 sound", "4.0 sound",
		"5.1 surround", "6.1 surround", "7.1 surround", "mp3", "opus"} {
		if n == a {
			return "Audio"
		}
	}

	// Miscellaneous
	for _, m := range []string{"repack/proper", "repack2", "repack3", "x264", "x265", "x266",
		"x265 (no hdr/dv)", "no-rlsgroup", "obfuscated", "retags", "scene", "bad dual groups",
		"black and white editions", "freeleech", "internal", "hfr", "multi",
		"vc-1", "vp9", "mpeg2", "line/mic dubbed", "with ad", "with.ad", "dutch groups", "webdl boost",
		"uhd streaming boost", "hd streaming boost",
		"single episode", "multi-episode", "web scene", "season pack"} {
		if n == m {
			return "Miscellaneous"
		}
	}

	// Resolution
	for _, r := range []string{"720p", "1080p", "2160p"} {
		if n == r {
			return "Resolution"
		}
	}

	return "Other"
}
