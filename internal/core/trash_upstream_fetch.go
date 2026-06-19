package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// upstreamWatchRefPrefix is the side-ref namespace Profile Sync uses to
// stash upstream commits without touching the user's master branch. Keeps
// `git pull` and Reset TRaSH Data working normally — they only care about
// refs/heads/*, never refs/upstream-watch/*.
const upstreamWatchRefPrefix = "refs/upstream-watch/"

// UpstreamWatchRef returns the side-ref name for a branch, for callers outside this
// package (e.g. the api naming apply paths) that read upstream naming via the ref.
func UpstreamWatchRef(branch string) string { return upstreamWatchRefPrefix + branch }

// FetchUpstreamRefspec runs `git fetch` from the configured remote into a
// dedicated side-ref so the detection-only path can walk the commit range
// without mutating any branch tracked by other code paths (CloneOrPull,
// Reset, manual `git pull`).
//
// Fetched commits land in `.git/objects/`; the side-ref names the upstream
// tip. The user's working tree and refs/heads/<branch> are untouched.
//
// Hardened against credential-leak + flag-injection the same way
// gitLsRemoteHead is (see watch.go) — credentials from a custom remote
// URL never reach error messages or container logs.
func (ts *TrashStore) FetchUpstreamRefspec(ctx context.Context, remoteURL, branch string) error {
	ts.repoMu.RLock()
	dataDir := ts.dataDir
	ts.repoMu.RUnlock()
	if dataDir == "" {
		return fmt.Errorf("trash store data dir not set")
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// refspec: pull `branch` into our side-ref so we don't disturb refs/heads/<branch>
	refspec := fmt.Sprintf("+%s:%s%s", branch, upstreamWatchRefPrefix, branch)
	cmd := exec.CommandContext(ctx, "git", "-C", dataDir, "fetch",
		"--no-write-fetch-head", // don't update FETCH_HEAD; we use the side-ref
		"--no-tags",             // tags aren't relevant; skip to save objects
		"--", remoteURL, refspec,
	)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=/bin/true",
		"GIT_SSH_COMMAND=ssh -oBatchMode=yes -oStrictHostKeyChecking=accept-new",
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return redactGitError(remoteURL, branch, stderr.String(), err)
	}
	return nil
}

// ChangedFilesSinceLocal returns the list of file paths changed in commits
// reachable from the upstream side-ref but not from the local branch HEAD.
// One entry per file regardless of how many commits touched it (the
// detection layer doesn't care about per-commit attribution — it cares
// about "what's the union of changes I missed since my last pull").
//
// Caller must have called FetchUpstreamRefspec first to populate the
// side-ref. Returns empty slice when the side-ref doesn't exist (treat
// as "no changes detected" rather than erroring).
func (ts *TrashStore) ChangedFilesSinceLocal(ctx context.Context, branch string) ([]string, error) {
	ts.repoMu.RLock()
	dataDir := ts.dataDir
	ts.repoMu.RUnlock()
	if dataDir == "" {
		return nil, fmt.Errorf("trash store data dir not set")
	}

	sideRef := upstreamWatchRefPrefix + branch
	// localRef is HEAD, not refs/heads/<branch>. Clonarr's CloneOrPull does
	// `git reset --hard origin/<branch>` from whatever ref is currently
	// checked out, so refs/heads/<branch> can be stale at an old commit
	// while HEAD has actually advanced. Using HEAD matches what
	// Trash.CurrentCommit() returns (rev-parse --short HEAD) and what
	// the upstream-ahead gate compares against, so the diff range stays
	// consistent across detection paths.
	localRef := "HEAD"

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// `git log local..sideRef --name-only --pretty=format:` returns each
	// changed file once per commit. Pipe through sort -u-style dedup in
	// Go since we don't need shell.
	cmd := exec.CommandContext(ctx, "git", "-C", dataDir, "log",
		"--name-only", "--pretty=format:",
		"--", // separator; nothing after this means "all paths"
	)
	// Insert the rev-range before the `--` separator. Building the args
	// slice explicitly avoids quoting issues.
	cmd.Args = []string{"git", "-C", dataDir, "log",
		localRef + ".." + sideRef,
		"--name-only", "--pretty=format:",
	}

	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		// Common case: side-ref doesn't exist yet (FetchUpstreamRefspec
		// hasn't been called or failed). Treat as "no changes" rather
		// than surfacing a misleading error to the watcher loop.
		if strings.Contains(stderr.String(), "unknown revision") ||
			strings.Contains(stderr.String(), "bad revision") {
			return nil, nil
		}
		return nil, fmt.Errorf("git log %s..%s: %w (stderr: %s)", localRef, sideRef, err, strings.TrimSpace(stderr.String()))
	}

	seen := make(map[string]bool)
	out_list := make([]string, 0)
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		out_list = append(out_list, line)
	}
	return out_list, nil
}

// ReadTrashIDFromRef reads the JSON file at `path` from the given git `ref`
// and returns the `trash_id` field. Returns empty string + nil error when:
//   - file doesn't exist at that ref (e.g. deleted upstream in this range)
//   - file isn't valid JSON
//   - JSON has no `trash_id` field
//
// Used by the detection path to resolve the real trash_id (a 32-char hash
// stored INSIDE the JSON) from a changed file path. The filename slug
// (e.g. `web-tier-01.json`) is NOT the trash_id and never matches against
// profile.FormatItems, which holds the hash form.
func (ts *TrashStore) ReadTrashIDFromRef(ctx context.Context, ref, path string) (string, error) {
	ts.repoMu.RLock()
	dataDir := ts.dataDir
	ts.repoMu.RUnlock()
	if dataDir == "" {
		return "", fmt.Errorf("trash store data dir not set")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", dataDir, "show", ref+":"+path)
	out, err := cmd.Output()
	if err != nil {
		// Most common case: file doesn't exist at this ref (deleted, or
		// added in upstream so HEAD lookup misses). Caller falls back to
		// the other ref or skips. Don't surface as error.
		return "", nil
	}
	var doc struct {
		TrashID string `json:"trash_id"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return "", nil
	}
	return doc.TrashID, nil
}

// GroupCFMember is one entry from a cf-group's custom_formats array — the
// CF's identity (trash_id + name) plus its in-group flags (default-on /
// required). Used by cf-group change detection to compare old-vs-new
// member lists and figure out what changed.
type GroupCFMember struct {
	TrashID  string `json:"trash_id"`
	Name     string `json:"name"`
	Default  bool   `json:"default"`  // default-on within the group
	Required bool   `json:"required"` // mandatory; user can't opt-out
}

// ReadCFGroupMembersFromRef returns the custom_formats array of the cf-group
// JSON at the given ref and path. Returns nil + nil error when the file
// doesn't exist at that ref (deleted upstream, or about-to-be-added — both
// valid states the diff caller handles by treating absence as empty).
func (ts *TrashStore) ReadCFGroupMembersFromRef(ctx context.Context, ref, path string) ([]GroupCFMember, error) {
	ts.repoMu.RLock()
	dataDir := ts.dataDir
	ts.repoMu.RUnlock()
	if dataDir == "" {
		return nil, fmt.Errorf("trash store data dir not set")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", dataDir, "show", ref+":"+path)
	out, err := cmd.Output()
	if err != nil {
		return nil, nil
	}
	var doc struct {
		CustomFormats []GroupCFMember `json:"custom_formats"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, nil
	}
	return doc.CustomFormats, nil
}

// GroupDiff captures what changed between two versions of a cf-group's
// member list. Each slice contains the NEW-side entry (so AffectedName
// reflects what the rule will see when sync runs). For removed CFs the
// entry comes from the OLD side since they're gone from NEW.
type GroupDiff struct {
	Added       []GroupCFMember // present in new, absent in old
	Removed     []GroupCFMember // present in old, absent in new (entry from old)
	DefaultOn   []GroupCFMember // flag flipped to default:true (was false or missing)
	DefaultOff  []GroupCFMember // flag flipped to default:false (was true)
	RequiredOn  []GroupCFMember // flag flipped to required:true
	RequiredOff []GroupCFMember // flag flipped to required:false
}

// DiffCFGroupMembers compares two member-list snapshots and categorises the
// changes. Caller passes nil for old (group added) or nil for new (group
// removed) to get the natural added-all / removed-all behaviour.
func DiffCFGroupMembers(oldMembers, newMembers []GroupCFMember) GroupDiff {
	oldByID := make(map[string]GroupCFMember, len(oldMembers))
	for _, m := range oldMembers {
		oldByID[m.TrashID] = m
	}
	newByID := make(map[string]GroupCFMember, len(newMembers))
	for _, m := range newMembers {
		newByID[m.TrashID] = m
	}
	var diff GroupDiff
	for _, n := range newMembers {
		o, existed := oldByID[n.TrashID]
		if !existed {
			diff.Added = append(diff.Added, n)
			continue
		}
		if n.Default && !o.Default {
			diff.DefaultOn = append(diff.DefaultOn, n)
		} else if !n.Default && o.Default {
			diff.DefaultOff = append(diff.DefaultOff, n)
		}
		if n.Required && !o.Required {
			diff.RequiredOn = append(diff.RequiredOn, n)
		} else if !n.Required && o.Required {
			diff.RequiredOff = append(diff.RequiredOff, n)
		}
	}
	for _, o := range oldMembers {
		if _, stillThere := newByID[o.TrashID]; !stillThere {
			diff.Removed = append(diff.Removed, o)
		}
	}
	return diff
}

// IsEmpty reports whether nothing changed.
func (d GroupDiff) IsEmpty() bool {
	return len(d.Added) == 0 && len(d.Removed) == 0 &&
		len(d.DefaultOn) == 0 && len(d.DefaultOff) == 0 &&
		len(d.RequiredOn) == 0 && len(d.RequiredOff) == 0
}

// CFSnapshot captures the subset of a TRaSH CF JSON the detection diff
// cares about: identity + per-context scores + a structural fingerprint
// of the specifications array. Specs themselves aren't enumerated —
// just hashed — since the spec list is heterogeneous and we only need
// to know "specs changed" vs "specs unchanged" for now.
type CFSnapshot struct {
	Name            string          `json:"name"`
	TrashScores     map[string]int  `json:"trash_scores"`
	Specifications  json.RawMessage `json:"specifications"`
	IncludeInRename bool            `json:"includeCustomFormatWhenRenaming"`
}

// CFDiff is the per-aspect change set between two CF snapshots.
type CFDiff struct {
	NameChanged  bool
	OldName      string
	NewName      string
	ScoreChanges map[string]ScoreChange // score-context key → old/new
	SpecsChanged bool
	// SpecDiff is the structured spec diff between old and new
	// Specifications, populated when SpecsChanged is true and both
	// sides parsed cleanly. Used by the TRaSH Last Updated /
	// Pending-updates panel to render "+ added X / ~ changed Y"
	// bullets instead of the bare "conditions changed" label. Nil
	// when the bytes differ but neither side parses (rare; e.g. an
	// upstream CF schema change). Reuses the diff engine defined
	// in cf_diff.go so the History row, Pending panel, pre-pull
	// preview, and drift modal can all share one source of truth.
	SpecDiff          *CFSpecDiff
	RenameFlagChanged bool
	RenameFlagNow     bool
}

// IsEmpty reports whether nothing actionable changed.
func (d CFDiff) IsEmpty() bool {
	return !d.NameChanged && !d.SpecsChanged && !d.RenameFlagChanged && len(d.ScoreChanges) == 0
}

// ReadCFFromRef parses a CF JSON at the given ref. Returns nil + nil
// error when the file isn't present (deleted or not yet added).
func (ts *TrashStore) ReadCFFromRef(ctx context.Context, ref, path string) (*CFSnapshot, error) {
	ts.repoMu.RLock()
	dataDir := ts.dataDir
	ts.repoMu.RUnlock()
	if dataDir == "" {
		return nil, fmt.Errorf("trash store data dir not set")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", dataDir, "show", ref+":"+path)
	out, err := cmd.Output()
	if err != nil {
		return nil, nil
	}
	var snap CFSnapshot
	if err := json.Unmarshal(out, &snap); err != nil {
		return nil, nil
	}
	return &snap, nil
}

// ReadNamingFromRef reads an app type's naming-scheme JSON from a git ref (the
// upstream side-ref) via `git show`, with NO working-tree pull — so the naming
// UPDATE check can see guide changes in Notify/Delayed mode the way profile-sync
// already detects CF/profile changes. Returns nil (not an error) when the file is
// absent/unreadable at the ref. The filename isn't assumed (matches the loader,
// which scans the naming dir): find the first .json under the dir at the ref.
func (ts *TrashStore) ReadNamingFromRef(ctx context.Context, ref, appType string) *TrashNaming {
	ts.repoMu.RLock()
	dataDir := ts.dataDir
	ts.repoMu.RUnlock()
	if dataDir == "" {
		return nil
	}
	dir := "docs/json/" + appType + "/naming"
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	lsOut, err := exec.CommandContext(ctx, "git", "-C", dataDir, "ls-tree", "--name-only", ref+":"+dir).Output()
	if err != nil {
		return nil
	}
	file := ""
	for _, line := range strings.Split(strings.TrimSpace(string(lsOut)), "\n") {
		if strings.HasSuffix(line, ".json") {
			file = line
			break
		}
	}
	if file == "" {
		return nil
	}
	out, err := exec.CommandContext(ctx, "git", "-C", dataDir, "show", ref+":"+dir+"/"+file).Output()
	if err != nil {
		return nil
	}
	var n TrashNaming
	if json.Unmarshal(out, &n) != nil {
		return nil
	}
	return &n
}

// DiffCFSnapshots reports per-aspect changes. Nil sides treated as empty
// (CF newly added or removed).
func DiffCFSnapshots(oldCF, newCF *CFSnapshot) CFDiff {
	var diff CFDiff
	if oldCF == nil {
		oldCF = &CFSnapshot{}
	}
	if newCF == nil {
		newCF = &CFSnapshot{}
	}
	if oldCF.Name != newCF.Name {
		diff.NameChanged = true
		diff.OldName = oldCF.Name
		diff.NewName = newCF.Name
	}
	if oldCF.IncludeInRename != newCF.IncludeInRename {
		diff.RenameFlagChanged = true
		diff.RenameFlagNow = newCF.IncludeInRename
	}
	// Score diff per context key. Union of both maps so added / removed
	// contexts surface too.
	keys := make(map[string]bool)
	for k := range oldCF.TrashScores {
		keys[k] = true
	}
	for k := range newCF.TrashScores {
		keys[k] = true
	}
	if len(keys) > 0 {
		diff.ScoreChanges = make(map[string]ScoreChange)
	}
	for k := range keys {
		o, oOk := oldCF.TrashScores[k]
		n, nOk := newCF.TrashScores[k]
		if oOk != nOk || o != n {
			diff.ScoreChanges[k] = ScoreChange{Old: o, New: n}
		}
	}
	// Specs change — byte-equal comparison on the raw JSON. Whitespace-
	// only edits (rare in TRaSH commits) would false-positive but that's
	// acceptable noise; full structural diff would require parsing the
	// heterogeneous spec types.
	if string(oldCF.Specifications) != string(newCF.Specifications) {
		diff.SpecsChanged = true
		// Compute the structured per-condition delta so the Pending /
		// TRaSH Last Updated panels can render concrete bullets
		// instead of the bare "conditions changed" placeholder. Parse
		// both sides into the TrashCF shape the diff engine expects;
		// silently skip on parse failure (caller still has SpecsChanged
		// as the fallback signal).
		if oldT, oldErr := snapshotToTrashCF(oldCF); oldErr == nil {
			if newT, newErr := snapshotToTrashCF(newCF); newErr == nil {
				if sd := DiffCFSpecs(oldT, newT); sd.HasAny() {
					diff.SpecDiff = sd
				}
			}
		}
	}
	return diff
}

// snapshotToTrashCF inflates a CFSnapshot into the TrashCF shape used
// by DiffCFSpecs. Only Specifications is parsed; the other fields
// (Name, IncludeInRename, TrashScores) are copied straight across so
// the diff engine can compare on them too if/when callers want it.
func snapshotToTrashCF(s *CFSnapshot) (*TrashCF, error) {
	if s == nil {
		return &TrashCF{}, nil
	}
	specs := []CFSpecification{}
	if len(s.Specifications) > 0 {
		if err := json.Unmarshal(s.Specifications, &specs); err != nil {
			return nil, err
		}
	}
	return &TrashCF{
		Name:            s.Name,
		IncludeInRename: s.IncludeInRename,
		TrashScores:     s.TrashScores,
		Specifications:  specs,
	}, nil
}

// QualityProfileSnapshot is the subset of a TRaSH quality-profile JSON
// the detection diff cares about. Captures only the fields that change
// the rule's effective Arr state on next sync.
type QualityProfileSnapshot struct {
	Name                  string            `json:"name"`
	Cutoff                string            `json:"cutoff"`
	MinFormatScore        int               `json:"minFormatScore"`
	CutoffFormatScore     int               `json:"cutoffFormatScore"`
	MinUpgradeFormatScore int               `json:"minUpgradeFormatScore"`
	UpgradeAllowed        bool              `json:"upgradeAllowed"`
	Language              string            `json:"language"`
	FormatItems           map[string]string `json:"formatItems"` // CF name → trash_id
	Items                 []QualityItemSnap `json:"items"`
}

// QualityItemSnap is a single quality-item row. For merged-quality
// groups (like "Merged QPs" wrapping Remux-2160p / WEBDL-2160p /
// WEBRip-2160p) the children are a STRING ARRAY of leaf names — not
// nested objects — matching the actual TRaSH profile-JSON schema.
type QualityItemSnap struct {
	Name    string   `json:"name"`
	Allowed bool     `json:"allowed"`
	Items   []string `json:"items"`
}

// ReadQualityProfileFromRef reads + parses the profile JSON at the given
// ref. Returns nil + nil error when the file doesn't exist (deleted or
// not yet added at that ref).
func (ts *TrashStore) ReadQualityProfileFromRef(ctx context.Context, ref, path string) (*QualityProfileSnapshot, error) {
	ts.repoMu.RLock()
	dataDir := ts.dataDir
	ts.repoMu.RUnlock()
	if dataDir == "" {
		return nil, fmt.Errorf("trash store data dir not set")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", dataDir, "show", ref+":"+path)
	out, err := cmd.Output()
	if err != nil {
		return nil, nil
	}
	var snap QualityProfileSnapshot
	if err := json.Unmarshal(out, &snap); err != nil {
		return nil, nil
	}
	return &snap, nil
}

// QualityItemChange is one quality whose `allowed` flag flipped between
// snapshots. The leaf name is what Arr displays in the profile editor.
type QualityItemChange struct {
	Name       string
	NowAllowed bool
}

// FormatItemChange is one CF reference added to or removed from the
// profile's direct formatItems map. TrashID is the CF's trash_id (the
// value side of the formatItems map).
type FormatItemChange struct {
	Name    string // CF name (map key)
	TrashID string // CF trash_id (map value)
}

// ScoreChange captures an old→new int delta for one profile-level score
// threshold (minFormatScore / cutoffFormatScore / minUpgradeFormatScore).
type ScoreChange struct {
	Old int
	New int
}

// ProfileDiff is the full set of changes between two quality-profile
// snapshots. All flag-changes false + slices empty means no meaningful
// diff (e.g. only whitespace tweaked in the file).
type ProfileDiff struct {
	NameChanged                  bool
	OldName                      string
	NewName                      string
	CutoffChanged                bool
	OldCutoff                    string
	NewCutoff                    string
	MinFormatScoreChanged        bool
	MinFormatScore               ScoreChange
	CutoffFormatScoreChanged     bool
	CutoffFormatScore            ScoreChange
	MinUpgradeFormatScoreChanged bool
	MinUpgradeFormatScore        ScoreChange
	UpgradeAllowedChanged        bool
	UpgradeAllowedNow            bool
	LanguageChanged              bool
	OldLanguage                  string
	NewLanguage                  string
	ItemsChanged                 []QualityItemChange // leaf qualities whose allowed flipped
	ItemsAdded                   []QualityItemChange // qualities present in new only
	ItemsRemoved                 []QualityItemChange // qualities present in old only
	FIAdded                      []FormatItemChange  // CFs added to formatItems
	FIRemoved                    []FormatItemChange  // CFs removed from formatItems
}

// IsEmpty reports whether there's nothing actionable in the diff.
func (d ProfileDiff) IsEmpty() bool {
	return !d.NameChanged && !d.CutoffChanged &&
		!d.MinFormatScoreChanged && !d.CutoffFormatScoreChanged && !d.MinUpgradeFormatScoreChanged &&
		!d.UpgradeAllowedChanged && !d.LanguageChanged &&
		len(d.ItemsChanged) == 0 && len(d.ItemsAdded) == 0 && len(d.ItemsRemoved) == 0 &&
		len(d.FIAdded) == 0 && len(d.FIRemoved) == 0
}

// flattenItemsAllowed reduces a (possibly nested) items list into a
// leaf-name → allowed-flag map. Merged-quality wrappers (an item with
// nested items as string names) push their `allowed` value down to each
// child leaf, matching what the Arr API actually applies.
func flattenItemsAllowed(items []QualityItemSnap) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, it := range items {
		if len(it.Items) > 0 {
			for _, leafName := range it.Items {
				if leafName != "" {
					out[leafName] = it.Allowed
				}
			}
			continue
		}
		if it.Name != "" {
			out[it.Name] = it.Allowed
		}
	}
	return out
}

// DiffQualityProfile compares two snapshots and categorises changes.
// Nil on either side is treated as empty (profile added or removed).
func DiffQualityProfile(oldP, newP *QualityProfileSnapshot) ProfileDiff {
	var diff ProfileDiff
	if oldP == nil {
		oldP = &QualityProfileSnapshot{}
	}
	if newP == nil {
		newP = &QualityProfileSnapshot{}
	}
	if oldP.Name != newP.Name {
		diff.NameChanged = true
		diff.OldName = oldP.Name
		diff.NewName = newP.Name
	}
	if oldP.Cutoff != newP.Cutoff {
		diff.CutoffChanged = true
		diff.OldCutoff = oldP.Cutoff
		diff.NewCutoff = newP.Cutoff
	}
	if oldP.MinFormatScore != newP.MinFormatScore {
		diff.MinFormatScoreChanged = true
		diff.MinFormatScore = ScoreChange{Old: oldP.MinFormatScore, New: newP.MinFormatScore}
	}
	if oldP.CutoffFormatScore != newP.CutoffFormatScore {
		diff.CutoffFormatScoreChanged = true
		diff.CutoffFormatScore = ScoreChange{Old: oldP.CutoffFormatScore, New: newP.CutoffFormatScore}
	}
	if oldP.MinUpgradeFormatScore != newP.MinUpgradeFormatScore {
		diff.MinUpgradeFormatScoreChanged = true
		diff.MinUpgradeFormatScore = ScoreChange{Old: oldP.MinUpgradeFormatScore, New: newP.MinUpgradeFormatScore}
	}
	if oldP.UpgradeAllowed != newP.UpgradeAllowed {
		diff.UpgradeAllowedChanged = true
		diff.UpgradeAllowedNow = newP.UpgradeAllowed
	}
	if oldP.Language != newP.Language {
		diff.LanguageChanged = true
		diff.OldLanguage = oldP.Language
		diff.NewLanguage = newP.Language
	}
	oldLeaves := flattenItemsAllowed(oldP.Items)
	newLeaves := flattenItemsAllowed(newP.Items)
	for name, nowAllowed := range newLeaves {
		oldAllowed, existed := oldLeaves[name]
		if !existed {
			diff.ItemsAdded = append(diff.ItemsAdded, QualityItemChange{Name: name, NowAllowed: nowAllowed})
			continue
		}
		if oldAllowed != nowAllowed {
			diff.ItemsChanged = append(diff.ItemsChanged, QualityItemChange{Name: name, NowAllowed: nowAllowed})
		}
	}
	for name, oldAllowed := range oldLeaves {
		if _, stillThere := newLeaves[name]; !stillThere {
			diff.ItemsRemoved = append(diff.ItemsRemoved, QualityItemChange{Name: name, NowAllowed: oldAllowed})
		}
	}
	// formatItems: map[CF-name]CF-trash_id. Diff by trash_id since CF
	// names can be renamed upstream without changing identity.
	oldFI := make(map[string]string, len(oldP.FormatItems)) // trash_id → name
	for name, tid := range oldP.FormatItems {
		oldFI[tid] = name
	}
	newFI := make(map[string]string, len(newP.FormatItems))
	for name, tid := range newP.FormatItems {
		newFI[tid] = name
	}
	for tid, name := range newFI {
		if _, existed := oldFI[tid]; !existed {
			diff.FIAdded = append(diff.FIAdded, FormatItemChange{Name: name, TrashID: tid})
		}
	}
	for tid, name := range oldFI {
		if _, stillThere := newFI[tid]; !stillThere {
			diff.FIRemoved = append(diff.FIRemoved, FormatItemChange{Name: name, TrashID: tid})
		}
	}
	return diff
}
