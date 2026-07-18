package main

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

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

// TestLayoutKeyhintKbdUsesKeycapClasses asserts the welcome-screen keyhint <kbd>
// elements carry the migrated keycap classes (markup migration target).
func TestLayoutKeyhintKbdUsesKeycapClasses(t *testing.T) {
	out := renderLayout(t, nil)

	labels := []string{"NEW", "CLICK", "ALT+N", "ALT+W"}
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

// TestLayoutControlsCarryDataAction asserts the static controls wired through
// the delegated dispatcher keep their data-action attribute. A control that
// loses its data-action is a dead button the JS unit tests can't catch (they
// build FakeElements, not the rendered template), so pin it here.
func TestLayoutControlsCarryDataAction(t *testing.T) {
	out := renderLayout(t, nil)

	for _, action := range []string{
		"open-settings", "flip-theme", "toggle-sidebar", "new-session",
		"save-settings", "settings-cat", "toggle-thinking",
		"share-go-public", "share-go-private", "share-copy", "share-regen",
	} {
		if !strings.Contains(out, `data-action="`+action+`"`) {
			t.Errorf("rendered layout missing a control with data-action=%q", action)
		}
	}

	// The migration must leave no inline onclick handlers in the template.
	if strings.Contains(out, "onclick=") {
		t.Error("rendered layout still contains an inline onclick handler")
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
