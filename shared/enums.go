package api

// Dashboard enums shared by the backend (which validates them) and the frontend
// (which renders the settings modal and accent picker from them). This is the
// single source: adding a model, effort level, or accent here updates both the
// server-side allowlist and the rendered UI at once.

// Option is a selectable value with its display label. When Value == Label the
// UI shows the raw value (models, efforts); when they differ the label is shown
// and the value is carried on the option's data-value (advisor model ids).
type Option struct {
	Value string
	Label string
}

// Models is the allowlist and option list for the main model field.
var Models = []Option{
	{"opus[1m]", "opus[1m]"},
	{"opus", "opus"},
	{"sonnet", "sonnet"},
	{"haiku", "haiku"},
}

// EffortLevels is the allowlist and option list for effortLevel.
var EffortLevels = []Option{
	{"low", "low"},
	{"medium", "medium"},
	{"high", "high"},
	{"xhigh", "xhigh"},
	{"max", "max"},
}

// AdvisorModels is the curated dropdown for advisorModel. It is a convenience
// subset for the picker only: the backend accepts any canonical model id (see
// the advisor validation regex), so this list is NOT a validation allowlist.
var AdvisorModels = []Option{
	{"", "(none)"},
	{"claude-fable-5", "Fable 5"},
	{"claude-opus-4-8", "Opus 4.8"},
	{"claude-sonnet-5", "Sonnet 5"},
}

// Accent is one accent-picker color: a name plus its dark- and light-theme hex.
type Accent struct {
	Name  string `json:"name"`
	Dark  string `json:"dark"`
	Light string `json:"light"`
}

// Accents is the accent-picker palette, rendered client-side by theme.js and
// name-validated server-side.
var Accents = []Accent{
	{"Red", "#ff4d33", "#d22f1a"},
	{"Amber", "#ffb02e", "#c97a00"},
	{"Lime", "#9ae600", "#5d8a00"},
	{"Cyan", "#2ee6d6", "#0a8f86"},
	{"Blue", "#4d8bff", "#1f5fd6"},
	{"Violet", "#b06bff", "#7a3fd6"},
	{"Pink", "#ff5fae", "#d62f86"},
}

// Themes is the binary light/dark toggle allowlist.
var Themes = []string{"light", "dark"}

// SessionKinds is the spawn/resume `kind` allowlist: a PTY-backed terminal or a
// stream-json pipe-backed chat (see SessionKind in types.go).
var SessionKinds = []SessionKind{SessionKindTerminal, SessionKindChat}

// SessionKindValues returns the session-kind allowlist as plain strings for
// validation.
func SessionKindValues() []string {
	out := make([]string, len(SessionKinds))
	for i, k := range SessionKinds {
		out[i] = string(k)
	}
	return out
}

// NewProjectNamePattern restricts a new project folder name to one safe path
// segment. Shared by the backend validator and the client-side pre-check.
const NewProjectNamePattern = `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`

// ModelFamilies lists the Claude model families from most to least capable.
// The backend's capability rank (advisor check) and its canonical-id regex both
// derive from this one list, so a family present in one but not the other can't
// silently misrank or misvalidate — adding a family is a single edit here.
var ModelFamilies = []string{"fable", "opus", "sonnet", "haiku"}

// ModelValues returns the model allowlist as plain strings for validation.
func ModelValues() []string { return optionValues(Models) }

// EffortValues returns the effort-level allowlist as plain strings for validation.
func EffortValues() []string { return optionValues(EffortLevels) }

// AccentNames returns the accent allowlist as plain strings for validation.
func AccentNames() []string {
	out := make([]string, len(Accents))
	for i, a := range Accents {
		out[i] = a.Name
	}
	return out
}

func optionValues(opts []Option) []string {
	out := make([]string, len(opts))
	for i, o := range opts {
		out[i] = o.Value
	}
	return out
}
