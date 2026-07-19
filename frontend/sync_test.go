package main

import (
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"

	"claude-frontend/web"
	api "claude-sandbox-api"
)

// allJS concatenates every top-level JS module source (skipping the vendor and
// __tests__ subdirs), so the sync tests can scan what the browser actually runs.
func allJS(t *testing.T) string {
	t.Helper()
	entries, err := fs.ReadDir(web.Static, "static/js")
	if err != nil {
		t.Fatalf("reading static/js: %v", err)
	}
	var sb strings.Builder
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".js") {
			continue
		}
		data, err := fs.ReadFile(web.Static, "static/js/"+e.Name())
		if err != nil {
			t.Fatalf("reading %s: %v", e.Name(), err)
		}
		sb.Write(data)
		sb.WriteByte('\n')
	}
	return sb.String()
}

// renderNamed renders any embedded template by name with the given data.
func renderNamed(t *testing.T, name string, data any) string {
	t.Helper()
	tmpl, err := parseTemplates()
	if err != nil {
		t.Fatalf("parsing templates: %v", err)
	}
	var sb strings.Builder
	if err := tmpl.ExecuteTemplate(&sb, name, data); err != nil {
		t.Fatalf("rendering %s: %v", name, err)
	}
	return sb.String()
}

// allRenderedHTML renders every template that carries data-action or id
// attributes (layout + both fragments), with sample data so conditional rows
// appear, and returns them concatenated.
func allRenderedHTML(t *testing.T) string {
	t.Helper()
	sessions := []api.DisplaySession{{Name: "claude-abc12345", CWD: "/workspace/p", DirName: "p", DisplayName: "p"}}
	dir := api.DirectoryData{
		Path:        "sub",
		FullPath:    "/workspace/sub",
		Dirs:        []string{"child"},
		Breadcrumbs: []api.Breadcrumb{{Name: "sub", Path: "sub"}},
	}
	return renderLayout(t, sessions) + renderNamed(t, "sessions", DashboardData{Sessions: sessions}) +
		renderNamed(t, "directory-picker", dir)
}

func uniqueMatches(re *regexp.Regexp, s string) []string {
	set := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		set[m[1]] = true
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestActionRegistrationMatchesTemplates pins the delegated-dispatch contract in
// both directions: every action a JS module register()s must have a matching
// data-action in some template, and every template data-action must be
// registered. A one-directional check lets a typo'd register() (dead button) or
// an orphan data-action slip through — this closes both.
func TestActionRegistrationMatchesTemplates(t *testing.T) {
	registered := uniqueMatches(regexp.MustCompile(`register\(['"]([a-z-]+)['"]`), allJS(t))
	inTemplates := uniqueMatches(regexp.MustCompile(`data-action="([a-z-]+)"`), allRenderedHTML(t))

	if len(registered) == 0 || len(inTemplates) == 0 {
		t.Fatalf("extraction found nothing (registered=%d, templates=%d)", len(registered), len(inTemplates))
	}

	regSet := toSet(registered)
	tmplSet := toSet(inTemplates)
	for _, a := range registered {
		if !tmplSet[a] {
			t.Errorf("action %q is register()ed in JS but has no data-action in any template (dead handler)", a)
		}
	}
	for _, a := range inTemplates {
		if !regSet[a] {
			t.Errorf("data-action=%q appears in a template but no JS module register()s it (dead button)", a)
		}
	}
}

// TestElementIdLookupsResolve asserts every static id a JS module looks up via
// getElementById exists in a rendered template — a template id rename/removal
// that orphans a JS lookup (silent null) fails here. Ids the JS itself creates
// (.id = 'x') and concatenation prefixes (…'foo-' + n) are excluded.
func TestElementIdLookupsResolve(t *testing.T) {
	js := allJS(t)
	lookups := uniqueMatches(regexp.MustCompile(`getElementById\('([a-zA-Z0-9-]+)'`), js)
	created := toSet(uniqueMatches(regexp.MustCompile(`\.id = '([a-zA-Z0-9-]+)'`), js))
	rendered := toSet(uniqueMatches(regexp.MustCompile(`id="([a-zA-Z0-9-]+)"`), allRenderedHTML(t)))

	if len(lookups) == 0 || len(rendered) == 0 {
		t.Fatalf("extraction found nothing (lookups=%d, rendered=%d)", len(lookups), len(rendered))
	}

	for _, id := range lookups {
		if strings.HasSuffix(id, "-") || created[id] {
			continue // concatenation prefix, or an id JS creates at runtime
		}
		if !rendered[id] {
			t.Errorf("getElementById(%q) has no matching id in any rendered template", id)
		}
	}
}

func toSet(xs []string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}
