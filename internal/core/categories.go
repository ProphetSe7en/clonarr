package core

// CategoryMeta is one entry in the UI manifest's CFCategories or ProfileGroups
// list. The frontend uses ID to compose CSS class names (cat-anime, grp-sqp,
// …) and writes Color into a matching CSS custom property so style rules in
// features/cf-detail.css and features/profiles.css resolve at render time.
//
// Aliases lets multiple human-readable category names map to the same ID.
// TRaSH's category list has near-duplicates ("French Audio Version" and
// "French HQ Source Groups" both map to the French color), and the alias
// table keeps that mapping declarative rather than scattering switch cases
// across the JS frontend.
type CategoryMeta struct {
	ID      string   `json:"id"`                // CSS-class-safe identifier, e.g. "anime"
	Label   string   `json:"label"`             // human-readable title
	Color   string   `json:"color"`             // hex value, e.g. "#f778ba"
	Aliases []string `json:"aliases,omitempty"` // alternate display names that resolve to this id
}

// CFCategories is the catalog of CF category groupings. The Color values
// match the original :root --cat-* tokens in tokens.css; the manifest
// pushes them into CSS custom properties at runtime so any future change
// here propagates to both Go (validation, sort order) and CSS without a
// rebuild.
//
// IDs match the CSS class suffixes in features/cf-detail.css.
var CFCategories = []CategoryMeta{
	{ID: "golden-rule", Label: "Golden Rule", Color: "#d2a8ff"},
	{ID: "audio", Label: "Audio", Color: "#d29922"},
	{ID: "hdr", Label: "HDR Formats", Color: "#a371f7"},
	{ID: "hq-release-groups", Label: "HQ Release Groups", Color: "#e3b341", Aliases: []string{"Release Groups"}},
	{ID: "resolution", Label: "Resolution", Color: "#79c0ff"},
	{ID: "streaming", Label: "Streaming Services", Color: "#39d353"},
	{ID: "miscellaneous", Label: "Miscellaneous", Color: "#8b949e"},
	{ID: "optional", Label: "Optional", Color: "#58a6ff", Aliases: []string{"Movie Versions"}},
	{ID: "unwanted", Label: "Unwanted", Color: "#f85149"},
	{ID: "anime", Label: "Anime", Color: "#f778ba"},
	{ID: "french", Label: "French", Color: "#39d2e0", Aliases: []string{"French Audio Version", "French HQ Source Groups"}},
	{ID: "german", Label: "German", Color: "#f0883e", Aliases: []string{"German Source Groups", "German Miscellaneous"}},
	{ID: "language", Label: "Language Profiles", Color: "#3fb950"},
	{ID: "other", Label: "Other", Color: "#8b949e"},
	{ID: "required-core", Label: "Required Core", Color: "#8b949e"},
}

// ProfileGroups is the catalog of profile-group prefix tags. IDs match the
// .grp-* class suffixes in features/profiles.css.
var ProfileGroups = []CategoryMeta{
	{ID: "sqp", Label: "SQP", Color: "#58a6ff"},
	{ID: "standard", Label: "Standard", Color: "#3fb950"},
	{ID: "anime", Label: "Anime", Color: "#d2a8ff"},
	{ID: "french", Label: "French", Color: "#d29922"},
	{ID: "german", Label: "German", Color: "#f0883e"},
	{ID: "imported", Label: "Imported", Color: "#8b949e"},
	{ID: "other", Label: "Other", Color: "#8b949e"},
}
