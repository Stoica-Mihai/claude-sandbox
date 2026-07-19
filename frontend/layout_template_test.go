package main

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	api "claude-sandbox-api"
)

// renderLayout parses the embedded templates the same way NewServer does and
// renders layout.html with the given sessions.
func renderLayout(t *testing.T, sessions []api.DisplaySession) string {
	t.Helper()
	tmpl, err := parseTemplates()
	if err != nil {
		t.Fatalf("parsing templates: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "layout.html", DashboardData{Sessions: sessions}); err != nil {
		t.Fatalf("rendering layout.html: %v", err)
	}
	return buf.String()
}

// renderFragment renders the sessions fragment with the given sessions.
func renderFragment(t *testing.T, sessions []api.DisplaySession) string {
	t.Helper()
	tmpl, err := parseTemplates()
	if err != nil {
		t.Fatalf("parsing templates: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "sessions", DashboardData{Sessions: sessions}); err != nil {
		t.Fatalf("rendering sessions fragment: %v", err)
	}
	return buf.String()
}

// sessionDataRe extracts the #session-data JSON payload from the fragment.
var sessionDataRe = regexp.MustCompile(`(?s)<script type="application/json" id="session-data">(.*?)</script>`)

// TestSessionsFragmentEmbedsJSONPayload pins the client store's data source:
// the fragment carries the session list as parseable JSON.
func TestSessionsFragmentEmbedsJSONPayload(t *testing.T) {
	sessions := []api.DisplaySession{{
		Name:        "claude-abc12345",
		CWD:         "/workspace/proj",
		DirName:     "proj",
		CreatedAt:   time.Unix(1700000000, 0),
		Alive:       true,
		DisplayName: `my "quoted" <proj>`,
	}}
	out := renderFragment(t, sessions)

	m := sessionDataRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatal("fragment is missing the #session-data JSON script block")
	}
	var got []api.DisplaySession
	if err := json.Unmarshal([]byte(m[1]), &got); err != nil {
		t.Fatalf("embedded payload is not valid JSON: %v\npayload: %s", err, m[1])
	}
	if len(got) != 1 || got[0].Name != "claude-abc12345" || got[0].DisplayName != sessions[0].DisplayName {
		t.Fatalf("payload round-trip mismatch: %+v", got)
	}
	// The payload must be HTML-safe: no raw </script> or unescaped angle brackets.
	if strings.Contains(m[1], "<") {
		t.Fatalf("payload contains an unescaped '<': %s", m[1])
	}
}

// TestSessionsFragmentEmptyPayload pins the nil-session shape: an empty JSON
// array, not "null" (the client store JSON.parses it directly).
func TestSessionsFragmentEmptyPayload(t *testing.T) {
	out := renderFragment(t, nil)
	m := sessionDataRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatal("fragment is missing the #session-data JSON script block")
	}
	if strings.TrimSpace(m[1]) != "[]" {
		t.Fatalf("empty payload = %q, want []", m[1])
	}
}

// TestLayoutKeyhintKbdUsesKeycapClasses asserts the welcome-screen keyhint <kbd>
// elements carry the migrated keycap classes (markup migration target).
func TestLayoutKeyhintKbdUsesKeycapClasses(t *testing.T) {
	out := renderLayout(t, nil)

	labels := []string{"NEW", "CLICK", "ALT+N", "ALT+1-9"}
	for _, label := range labels {
		want := `<kbd class="keycap keycap--hint">` + label + `</kbd>`
		if !strings.Contains(out, want) {
			t.Errorf("missing migrated keyhint kbd for %q; want %q", label, want)
		}
	}

	// Every kbd in the layout must be a keycap--hint; no bare kbd survives.
	kbdRe := regexp.MustCompile(`(?s)<kbd[^>]*>`)
	for _, tag := range kbdRe.FindAllString(out, -1) {
		if !strings.Contains(tag, `class="keycap keycap--hint"`) {
			t.Errorf("found kbd without keycap--hint classes: %q", tag)
		}
	}
}

// TestLayoutMobileKeysUseKeycapClasses asserts every mobile control-bar button
// carries the migrated keycap classes and none use the old mobile-key class.
func TestLayoutMobileKeysUseKeycapClasses(t *testing.T) {
	out := renderLayout(t, nil)

	// The mobile input bar has 8 control buttons (Esc, ^C, backspace, 4 arrows, Select).
	const wantClass = `class="keycap keycap--mobile"`
	if got := strings.Count(out, wantClass); got != 8 {
		t.Errorf("keycap--mobile button count = %d, want 8", got)
	}

	for _, label := range []string{"Esc", "^C", "Select"} {
		btn := `<button ` + wantClass + ` data-action="`
		if !strings.Contains(out, btn) {
			t.Errorf("no keycap--mobile button found near label %q", label)
		}
	}
}

// TestLayoutHasNoInlineOnclick pins the delegated-dispatch migration: the
// template wires actions via data-action, never inline onclick. The
// data-action <-> register() coverage lives in TestActionRegistrationMatchesTemplates.
func TestLayoutHasNoInlineOnclick(t *testing.T) {
	if strings.Contains(renderLayout(t, nil), "onclick=") {
		t.Error("rendered layout contains an inline onclick handler")
	}
}

// TestLayoutDropsLegacyKeyClasses guards against regression to the pre-migration
// class names anywhere in the rendered layout.
func TestLayoutDropsLegacyKeyClasses(t *testing.T) {
	out := renderLayout(t, nil)

	for _, legacy := range []string{`class="mobile-key"`, `"mobile-key"`} {
		if strings.Contains(out, legacy) {
			t.Errorf("rendered layout still contains legacy class %q", legacy)
		}
	}

	// A bare <kbd> with no class is the pre-migration shape; it must be gone.
	bareKbd := regexp.MustCompile(`<kbd>`)
	if bareKbd.MatchString(out) {
		t.Error("rendered layout still contains a bare <kbd> (pre-migration markup)")
	}
}

// TestLayoutDesktopControlsUseKeycap asserts the desktop control bar keeps its
// keycap buttons (Esc/Ctrl+C/Ctrl+D) alongside the migrated hint/mobile keycaps.
func TestLayoutDesktopControlsUseKeycap(t *testing.T) {
	out := renderLayout(t, nil)

	for _, label := range []string{">Esc<", ">Ctrl+C<", ">Ctrl+D<"} {
		if !strings.Contains(out, label) {
			t.Errorf("desktop control label %q missing from layout", label)
		}
	}
	// Plain desktop keycaps use only the base class (no modifier).
	if !strings.Contains(out, `<button class="keycap" data-action="send-key" data-key="escape"`) {
		t.Error("desktop Esc keycap button not rendered with base keycap class")
	}
}
