// Package-level enumerations exposed via the UI manifest endpoint.
//
// These are the single source of truth for option lists that the frontend
// renders as <select> dropdowns and that backend validators check against.
// Adding or removing a value here automatically propagates to both sides:
// the validators in internal/api/config.go read from the same slices the
// manifest serves.
//
// Each enum value carries a stable Value (sent to/from the API and persisted
// to disk), a Label (displayed in the UI), and an optional Description
// (shown as helper text below the dropdown for the currently-selected
// value). Descriptions are drawn from existing UI copy where the original
// HTML had inline x-text branches keyed on the value.
package core

// EnumValue is a stable enum value with a UI-displayable label.
// Description is optional helper text. All fields are JSON-serialized so
// the manifest endpoint can return slices of these directly.
type EnumValue struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// IntBounds is an inclusive integer range for numeric inputs.
type IntBounds struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// AppTypes is the list of supported *arr application types.
// Used by Instance.Type and many endpoint paths (/api/trash/{app}/...).
var AppTypes = []EnumValue{
	{Value: "radarr", Label: "Radarr"},
	{Value: "sonarr", Label: "Sonarr"},
}

// SyncBehaviorAddModes controls how missing CFs are added to the target Arr profile.
var SyncBehaviorAddModes = []EnumValue{
	{
		Value:       "add_missing",
		Label:       "Add any missing formats",
		Description: "Creates every CF in this sync set that doesn't exist in Arr yet. Runs on every sync regardless of history.",
	},
	{
		Value:       "add_new",
		Label:       "Respect manual removals — only add new ones",
		Description: "Only creates CFs that weren't part of your previous sync — for example, CFs TRaSH has added upstream since then. CFs you manually deleted from Arr after a previous sync will stay deleted. On your first-ever sync this behaves identically to 'Add any missing formats'.",
	},
	{
		Value:       "do_not_add",
		Label:       "Do not add any formats",
		Description: "No CFs will be created. Existing CFs in Arr still get their scores updated according to the Scores option below. Missing CFs are silently skipped.",
	},
}

// SyncBehaviorRemoveModes controls how scores in Arr are written/preserved.
var SyncBehaviorRemoveModes = []EnumValue{
	{
		Value:       "remove_custom",
		Label:       "Overwrite all scores in Arr",
		Description: "Clonarr pushes its desired scores (TRaSH defaults plus any score overrides you've set in Clonarr) to Arr, replacing any manual edits made directly in Arr's UI.",
	},
	{
		Value:       "allow_custom",
		Label:       "Preserve manual edits in Arr",
		Description: "Skips CFs that have a non-zero score in Arr which differs from Clonarr's desired value — those are treated as manual edits and left alone. CFs currently at 0 still get the desired score written. Your Clonarr score overrides still apply in both modes; they're part of 'the desired value', not a separate layer.",
	},
}

// SyncBehaviorResetModes controls treatment of CFs scored in Arr but no longer in the sync set.
var SyncBehaviorResetModes = []EnumValue{
	{
		Value:       "reset_to_zero",
		Label:       "Zero out orphaned scores",
		Description: "For CFs currently scored non-zero in the target Arr profile that are no longer part of this sync, the score is reset to 0. CFs already at 0 are left alone. Useful for cleaning up leftover scores after removing a CF from the profile.",
	},
	{
		Value:       "do_not_adjust",
		Label:       "Do not adjust existing scores",
		Description: "Any CF in the target Arr profile that isn't part of this sync keeps whatever score it already has — Clonarr leaves it completely alone.",
	},
}

// AuthModes lists supported authentication modes for the web UI.
// Labels match the existing settings page wording.
var AuthModes = []EnumValue{
	{Value: "forms", Label: "Forms (login page)"},
	{Value: "basic", Label: "Basic (browser popup)"},
	{Value: "none", Label: "None (disabled — unsafe)"},
}

// AuthRequiredModes controls when authentication is enforced relative to the request source.
var AuthRequiredModes = []EnumValue{
	{Value: "disabled_for_local_addresses", Label: "Disabled for Trusted Networks"},
	{Value: "enabled", Label: "Enabled (all traffic)"},
}

// PullIntervalPresets lists the values the Settings dropdown offers, in
// display order. "0" means scheduled pulls are disabled (manual only).
var PullIntervalPresets = []EnumValue{
	{Value: "0", Label: "Disabled"},
	{Value: "5m", Label: "Every 5 minutes"},
	{Value: "15m", Label: "Every 15 minutes"},
	{Value: "30m", Label: "Every 30 minutes"},
	{Value: "1h", Label: "Every hour"},
	{Value: "6h", Label: "Every 6 hours"},
	{Value: "12h", Label: "Every 12 hours"},
	{Value: "24h", Label: "Every 24 hours"},
}

// SessionTTLBounds is the accepted range for Authentication > Session TTL (days).
var SessionTTLBounds = IntBounds{Min: 1, Max: 365}

// IsValidEnumValue returns true when value matches one of the enum's Value fields.
// Used by request validators to keep enum membership in sync with the manifest.
func IsValidEnumValue(enum []EnumValue, value string) bool {
	for _, v := range enum {
		if v.Value == value {
			return true
		}
	}
	return false
}

// EnumValues returns the bare Value strings for an enum slice.
// Convenient for error messages that list valid options.
func EnumValues(enum []EnumValue) []string {
	out := make([]string, 0, len(enum))
	for _, v := range enum {
		out = append(out, v.Value)
	}
	return out
}
