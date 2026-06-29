package web

import (
	"regexp"
	"strings"
	"testing"
)

var cssComment = regexp.MustCompile(`(?s)/\*.*?\*/`)

// cssText is the embedded stylesheet that actually ships to the browser, with
// /* */ comments stripped so they cannot leak into selector text.
func cssText(t *testing.T) string {
	t.Helper()
	b, err := Static.ReadFile("static/css/style.css")
	if err != nil {
		t.Fatalf("read embedded style.css: %v", err)
	}
	return cssComment.ReplaceAllString(string(b), "")
}

// ruleBody returns the declaration block (text between { and the matching }) for
// the first rule whose selector list exactly equals sel. Selectors that merely
// contain sel as a substring are skipped so ".keycap" does not match ".keycap--mobile".
func ruleBody(t *testing.T, css, sel string) (string, bool) {
	t.Helper()
	for i := 0; i < len(css); {
		open := strings.IndexByte(css[i:], '{')
		if open < 0 {
			return "", false
		}
		open += i
		close := strings.IndexByte(css[open:], '}')
		if close < 0 {
			return "", false
		}
		close += open
		selector := strings.TrimSpace(css[i:open])
		if selector == sel {
			return css[open+1 : close], true
		}
		i = close + 1
	}
	return "", false
}

// assertDecls checks that every "prop:value" pair appears (whitespace-insensitive)
// inside the declaration block of the given selector.
func assertDecls(t *testing.T, css, sel string, decls ...string) {
	t.Helper()
	body, ok := ruleBody(t, css, sel)
	if !ok {
		t.Fatalf("selector %q not found in style.css", sel)
	}
	norm := strings.Join(strings.Fields(body), "")
	for _, d := range decls {
		want := strings.Join(strings.Fields(d), "")
		if !strings.Contains(norm, want) {
			t.Errorf("selector %q missing declaration %q\n  block: %s", sel, d, body)
		}
	}
}

// One-red palette (law 4): no off-brand status token exists, and no
// --terminal-fg default is added to :root (it is set at runtime in terminal.js).
func TestNoStatusGreenTokenOneRed(t *testing.T) {
	css := cssText(t)

	if strings.Contains(css, "--ok") {
		t.Error("--ok token must not exist: the system is one-red (law 4); use --accent")
	}
	if strings.Contains(css, "#3fb950") {
		t.Error("off-brand green #3fb950 must not appear in the stylesheet")
	}

	light, ok := ruleBody(t, css, ":root")
	if !ok {
		t.Fatal(":root block not found")
	}
	if strings.Contains(strings.Join(strings.Fields(light), ""), "--terminal-fg:") {
		t.Error(":root must not declare a --terminal-fg default (set at runtime in terminal.js)")
	}
}

// Task 2.1: the base .keycap atom holds the canonical ctrlbar values, and
// .keycap:hover is the shared hover affordance.
func TestKeycapBaseAtom(t *testing.T) {
	css := cssText(t)
	assertDecls(t, css, ".keycap",
		"border:2px solid var(--line)",
		"background:var(--surf)",
		"color:var(--ink)",
		"font-family:var(--mono)",
		"font-size:10px",
		"font-weight:700",
		"padding:3px 9px",
		"cursor:pointer",
	)
	assertDecls(t, css, ".keycap:hover",
		"background:var(--accent)",
		"color:var(--on-accent)",
		"border-color:var(--accent)",
	)
}

// Task 2.2: .keycap--hint carries only the welcome/keyhint deltas.
func TestKeycapHintModifier(t *testing.T) {
	assertDecls(t, cssText(t), ".keycap--hint",
		"font-size:11px",
		"min-width:54px",
		"text-align:center",
		"cursor:default",
	)
}

// Task 2.3: .keycap--mobile and its :active touch affordance.
func TestKeycapMobileModifier(t *testing.T) {
	css := cssText(t)
	assertDecls(t, css, ".keycap--mobile",
		"font-size:13px",
		"height:36px",
		"min-width:36px",
		"padding:0 10px",
		"flex-shrink:0",
	)
	assertDecls(t, css, ".keycap--mobile:active",
		"background:var(--accent)",
		"color:var(--on-accent)",
		"border-color:var(--accent)",
	)
}

// The mobile Select-active state uses the one-red accent (no off-brand green).
func TestKeycapMobileSelActive(t *testing.T) {
	assertDecls(t, cssText(t), ".keycap--mobile.sel-active",
		"color:var(--accent)",
		"border-color:var(--accent)",
	)
}

// Tasks 2.5/2.6/2.7: the duplicated/dead key-chip rules are deleted outright.
func TestDeletedKeyChipRules(t *testing.T) {
	css := cssText(t)
	for _, sel := range []string{
		".keyhint kbd",
		".mobile-key",
		".mobile-key:active",
		".terminal-controls-bg .kbd",
		".terminal-controls-bg .kbd:hover",
	} {
		if _, ok := ruleBody(t, css, sel); ok {
			t.Errorf("rule %q should have been deleted but is still present", sel)
		}
	}
	// No element-bearing class "kbd" remains anywhere (selector token).
	if regexp.MustCompile(`(^|[\s,])\.kbd(\b|[:.\s,{])`).MatchString(css) {
		t.Error("a .kbd selector still exists; the dead key-chip rules must be removed")
	}
	if strings.Contains(css, ".mobile-key") {
		t.Error(".mobile-key still referenced; should be replaced by .keycap--mobile")
	}
}

// Task 2.5: the .keyhint layout rules are kept (only `.keyhint kbd` was removed).
func TestKeyhintLayoutKept(t *testing.T) {
	css := cssText(t)
	assertDecls(t, css, ".keyhint",
		"display:flex",
		"align-items:center",
	)
	assertDecls(t, css, ".keyhint span",
		"color:var(--muted)",
	)
}

// Task 3.1: the lifted #selectOverlay rule is driven by the terminal tokens and
// uses the mono font stack (D3).
func TestSelectOverlayRule(t *testing.T) {
	assertDecls(t, cssText(t), "#selectOverlay",
		"position:absolute",
		"inset:0",
		"z-index:50",
		"padding:12px",
		"overflow-y:auto",
		"font-size:13px",
		"font-family:var(--mono)",
		"color:var(--terminal-fg)",
		"background:var(--terminal-bg)",
		"user-select:text",
		"white-space:pre-wrap",
		"word-break:break-all",
	)
}

// Task 3.2: the rename-error flash resolves through the accent token.
func TestErrFlashRule(t *testing.T) {
	assertDecls(t, cssText(t), "input.err-flash",
		"outline:3px solid var(--accent)",
	)
}

// Task 3.3: the spawn-failure button state resolves through accent tokens.
func TestSpawnFailRule(t *testing.T) {
	assertDecls(t, cssText(t), ".btn-spawn-fail",
		"background:var(--accent)",
		"color:var(--on-accent)",
	)
}

// The lifted/tokenized rules must not reintroduce the hardcoded theme-able hex
// literals the change was meant to eliminate from these rules.
func TestNoHardcodedThemeHexInChangedRules(t *testing.T) {
	css := cssText(t)
	rules := []string{
		"#selectOverlay",
		"input.err-flash",
		".btn-spawn-fail",
		".keycap--mobile.sel-active",
	}
	banned := []string{"#d22f1a", "#efe9dc", "#3fb950", "#c9d1d9", "#0d1117", "rgba(63,185,80"}
	for _, sel := range rules {
		body, ok := ruleBody(t, css, sel)
		if !ok {
			t.Fatalf("selector %q not found", sel)
		}
		low := strings.ToLower(strings.Join(strings.Fields(body), ""))
		for _, hex := range banned {
			if strings.Contains(low, strings.ToLower(hex)) {
				t.Errorf("rule %q contains hardcoded %q; must use a theme token", sel, hex)
			}
		}
	}
}
