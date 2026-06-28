package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestValidLanguage(t *testing.T) {
	cases := map[string]bool{
		"english":   true,
		"":          true,
		"français":  true,
		"this language name is definitely longer than forty chars": false, // > 40
		"bad\nnewline": false,
		"tab\there":    false,
	}
	for in, want := range cases {
		if got := validLanguage(in); got != want {
			t.Errorf("validLanguage(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestAllowlists(t *testing.T) {
	if !slices.Contains(allowedModels, "opus[1m]") {
		t.Error("opus[1m] should be an allowed model")
	}
	if slices.Contains(allowedModels, "gpt-4") {
		t.Error("gpt-4 must not be an allowed model")
	}
	for _, e := range []string{"low", "medium", "high", "xhigh", "max"} {
		if !slices.Contains(allowedEffort, e) {
			t.Errorf("%q should be an allowed effort", e)
		}
	}
	if slices.Contains(allowedEffort, "extreme") {
		t.Error("extreme must not be an allowed effort")
	}
}

func TestCanonicalModelID(t *testing.T) {
	valid := []string{"claude-opus-4-8", "claude-sonnet-4-6", "claude-haiku-4-5-20251001"}
	invalid := []string{"opus[1m]", "opus", "sonnet", "haiku", "gpt-4", "", "claude-foo-1"}
	for _, v := range valid {
		if !canonicalModelID.MatchString(v) {
			t.Errorf("%q should be a valid advisor id", v)
		}
	}
	for _, v := range invalid {
		if canonicalModelID.MatchString(v) {
			t.Errorf("%q should NOT be a valid advisor id", v)
		}
	}
}

func TestWriteFileAtomicAndInPlace(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "out.json")
	if err := writeFileAtomic(p, []byte("hello")); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	got, err := os.ReadFile(p)
	if err != nil || string(got) != "hello" {
		t.Fatalf("read back = %q, %v", got, err)
	}
	// Overwrite an existing file (exercises the rename-over-existing path).
	if err := writeFileAtomic(p, []byte("world")); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	got, _ = os.ReadFile(p)
	if string(got) != "world" {
		t.Fatalf("rewrite read back = %q", got)
	}
	// In-place writer used as the bind-mount fallback.
	if err := writeInPlace(p, []byte("inplace")); err != nil {
		t.Fatalf("writeInPlace: %v", err)
	}
	got, _ = os.ReadFile(p)
	if string(got) != "inplace" {
		t.Fatalf("inplace read back = %q", got)
	}
	if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v (err %v), want 0600", fi.Mode().Perm(), err)
	}
}

// TestMergePreservesNonEditableKeys mirrors the handler's merge: only the
// whitelisted keys change; everything else survives.
func TestMergePreservesNonEditableKeys(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "container-settings.json")
	original := map[string]any{
		"model":         "sonnet",
		"effortLevel":   "high",
		"enabledPlugins": map[string]any{"caveman@caveman": true},
		"hooks":          map[string]any{"PreToolUse": []any{"x"}},
		"skipDangerousModePermissionPrompt": true,
	}
	b, _ := json.MarshalIndent(original, "", "  ")
	if err := os.WriteFile(src, b, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONTAINER_SETTINGS_PATH", src)

	m, err := readContainerSettings()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// Apply the same overlay the handler does.
	m["model"] = "haiku"
	m["effortLevel"] = "medium"
	m["alwaysThinkingEnabled"] = false
	m["language"] = "english"
	delete(m, "advisorModel")

	merged, _ := json.Marshal(m)
	var out map[string]any
	json.Unmarshal(merged, &out)

	if out["model"] != "haiku" || out["effortLevel"] != "medium" {
		t.Errorf("editable keys not applied: %v", out)
	}
	if _, ok := out["enabledPlugins"]; !ok {
		t.Error("enabledPlugins was dropped")
	}
	if _, ok := out["hooks"]; !ok {
		t.Error("hooks was dropped")
	}
	if out["skipDangerousModePermissionPrompt"] != true {
		t.Error("skipDangerousModePermissionPrompt was dropped/changed")
	}
}

func TestReadContainerSettingsMissingFile(t *testing.T) {
	t.Setenv("CONTAINER_SETTINGS_PATH", filepath.Join(t.TempDir(), "nope.json"))
	m, err := readContainerSettings()
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if len(m) != 0 {
		t.Errorf("missing file should yield empty map, got %v", m)
	}
}
