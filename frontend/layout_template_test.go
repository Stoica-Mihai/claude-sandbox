package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	api "claude-sandbox-api"
)

// renderLayout renders layout.html with the given sessions.
func renderLayout(t *testing.T, sessions []api.DisplaySession) string {
	return renderNamed(t, "layout.html", DashboardData{Sessions: sessions})
}

// renderFragment renders the sessions fragment with the given sessions.
func renderFragment(t *testing.T, sessions []api.DisplaySession) string {
	return renderNamed(t, "sessions", DashboardData{Sessions: sessions})
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

// renderLogs renders layout.html with the logs surface active.
func renderLogs(t *testing.T) string {
	return renderNamed(t, "layout.html", DashboardData{Logs: true})
}

// TestLogsLayoutRendersLogsContext pins the logs surface: sub-label "logs", the
// logs view markup (status strip + log list), the "Logs" sidebar kick, and NO
// session content (no session-data payload, no + NEW, no session cards).
func TestLogsLayoutRendersLogsContext(t *testing.T) {
	out := renderLogs(t)

	for _, want := range []string{
		`<span class="sub">logs</span>`,
		`class="lz-status"`,
		`class="lz-list"`,
		`class="lz-filters"`,
		`<span class="kick">Logs</span>`,
		`class="lz-side-hint"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("logs layout missing %q", want)
		}
	}

	for _, notWant := range []string{
		`id="session-data"`,
		`class="scard session-card"`,
		`data-action="new-session"`,
		`<span class="sub">dashboard</span>`,
	} {
		if strings.Contains(out, notWant) {
			t.Errorf("logs layout should not contain %q (session surface leaked)", notWant)
		}
	}

	// The Logs footer nav entry is the current surface.
	if !strings.Contains(out, `href="/logs" title="Logs" aria-current="page"`) {
		t.Error("logs layout: Logs nav entry not marked aria-current")
	}
}

// TestDashboardLayoutUnchanged pins the dashboard surface: sub-label
// "dashboard", session list present, Dashboard nav marked current, and the logs
// view absent.
func TestDashboardLayoutUnchanged(t *testing.T) {
	out := renderLayout(t, []api.DisplaySession{{Name: "claude-abc12345", CWD: "/workspace/p", DisplayName: "p"}})

	for _, want := range []string{
		`<span class="sub">dashboard</span>`,
		`id="session-list"`,
		`<span class="kick">Sessions</span>`,
		`data-action="new-session"`,
		`href="/" title="Dashboard" aria-current="page"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dashboard layout missing %q", want)
		}
	}
	if strings.Contains(out, `class="lz-list"`) {
		t.Error("dashboard layout should not contain the logs view")
	}
}

// TestLogsSurfaceGatedByLogSharing pins the flow: sharing off ⇒ a tunnel
// visitor gets no Logs menu and a 403 on /logs; the host enabling log sharing
// ⇒ the tunnel gets the Logs menu + /logs. Host is always unaffected.
func TestLogsSurfaceGatedByLogSharing(t *testing.T) {
	if host := renderNamed(t, "layout.html", DashboardData{}); !strings.Contains(host, `href="/logs"`) {
		t.Error("host layout should show the Logs nav item")
	}
	if hidden := renderNamed(t, "layout.html", DashboardData{HideLogs: true}); strings.Contains(hidden, `href="/logs"`) {
		t.Error("HideLogs layout must omit the Logs nav item")
	}

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	}))
	t.Cleanup(backend.Close)
	mux := http.NewServeMux()
	srv, err := NewServer(backend.URL, backend.URL, backend.URL, mux)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	tunnelGet := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		markTunnel(mux).ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		return rec
	}

	// Sharing off (default): tunnel /logs 403, tunnel shell omits the Logs nav.
	if rec := tunnelGet("/logs"); rec.Code != http.StatusForbidden {
		t.Errorf("tunnel /logs (sharing off) = %d, want 403", rec.Code)
	}
	if rec := tunnelGet("/"); strings.Contains(rec.Body.String(), `href="/logs"`) {
		t.Error("tunnel dashboard (sharing off) must omit the Logs nav item")
	}

	// Host enables log sharing: the tunnel now gets the surface.
	srv.shareLogsEnabled.Store(true)
	if rec := tunnelGet("/logs"); rec.Code != http.StatusOK {
		t.Errorf("tunnel /logs (sharing on) = %d, want 200", rec.Code)
	}
	if rec := tunnelGet("/"); !strings.Contains(rec.Body.String(), `href="/logs"`) {
		t.Error("tunnel dashboard (sharing on) should show the Logs nav item")
	}
}

// TestLogsRouteServesLogsContext exercises the real GET /logs handler end to
// end (through the mux) with a stub backend, and confirms GET / still serves the
// dashboard surface.
func TestLogsRouteServesLogsContext(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]")) // GET /api/sessions → empty list
	}))
	t.Cleanup(backend.Close)

	mux := http.NewServeMux()
	if _, err := NewServer(backend.URL, backend.URL, backend.URL, mux); err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	get := func(path string) string {
		req := httptest.NewRequest("GET", path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, rec.Code)
		}
		return rec.Body.String()
	}

	logs := get("/logs")
	if !strings.Contains(logs, `<span class="sub">logs</span>`) || !strings.Contains(logs, `class="lz-list"`) {
		t.Error("GET /logs did not render the logs context")
	}
	if strings.Contains(logs, `class="scard session-card"`) {
		t.Error("GET /logs leaked session cards")
	}

	dash := get("/")
	if !strings.Contains(dash, `<span class="sub">dashboard</span>`) || !strings.Contains(dash, `id="session-list"`) {
		t.Error("GET / did not render the dashboard context")
	}
	if strings.Contains(dash, `class="lz-list"`) {
		t.Error("GET / leaked the logs view")
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
