package api

// MaxJSONBody caps request bodies decoded as JSON across the API. Every such
// route carries a small object (a cwd, a name, a prefs subset), so a generous
// cap still rejects a body sent to exhaust memory. Shared by the backend (its
// authoritative enforcement) and the frontend proxy (its pre-forward buffering
// bound) so the two limits are one value and can't drift.
const MaxJSONBody = 64 << 10
