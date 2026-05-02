package agents

// FieldSpec describes the form layout for one notification provider's
// credentials and options. The frontend reads this from /api/ui/manifest
// and renders the agent modal generically — adding a new provider type
// only requires implementing Provider.FieldSpec, with no HTML edits.
//
// A FieldSpec is an ordered list of Groups. Each group is either a single
// input (Kind "field") or a "priority levels" cluster (Kind "priority")
// that maps three Severity tiers to per-tier enable/value pairs.
type FieldSpec struct {
	Groups []FieldGroup `json:"groups"`
}

// FieldGroup is one row in the agent modal. Exactly one of Field/Priority
// is non-nil based on Kind.
type FieldGroup struct {
	Kind     string         `json:"kind"` // "field" | "priority"
	Field    *Field         `json:"field,omitempty"`
	Priority *PriorityGroup `json:"priority,omitempty"`
}

// Field describes a single input row.
//
// Name MUST match the Config struct's JSON tag (lowerCamelCase, e.g.
// "discordWebhook"). The frontend writes user input to
// agentModal.config[Name].
//
// Kind drives the input element:
//   - "text"        → <input type="text">
//   - "password"    → <input type="password">
//   - "url"         → <input type="text"> with URL placeholder
//   - "stringList"  → <textarea>; value joined/split by newlines
type Field struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Label       string `json:"label"`
	LabelHint   string `json:"labelHint,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	HelpHTML    string `json:"helpHtml,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// PriorityGroup represents the 3-tier priority section used by gotify and
// ntfy. The frontend renders one row per Level with a checkbox bound to
// EnabledKey and a number input bound to ValueKey on agentModal.config.
type PriorityGroup struct {
	Label  string          `json:"label"`
	Min    int             `json:"min"`
	Max    int             `json:"max"`
	Levels []PriorityLevel `json:"levels"`
}

// PriorityLevel is one row inside a PriorityGroup.
type PriorityLevel struct {
	Label      string `json:"label"`
	EnabledKey string `json:"enabledKey"`
	ValueKey   string `json:"valueKey"`
	Note       string `json:"note,omitempty"`
}
