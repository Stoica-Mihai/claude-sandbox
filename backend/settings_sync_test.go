package main

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// TestSettingsFieldsMirroredInFrontend guards the editable-settings field-name
// contract across the Go/JS boundary. editableSettings' json tags are the
// authority; the frontend settings.js reads and writes those exact keys and the
// settings template wires the dropdowns by data-field. A Go rename not mirrored
// in the JS makes that setting silently drop on decode (zero value), with no
// error — this fails instead. Reads the sibling frontend sources by relative
// path (run from the backend module dir, as `go test` does).
func TestSettingsFieldsMirroredInFrontend(t *testing.T) {
	tags := jsonTags(reflect.TypeOf(editableSettings{}))
	if len(tags) == 0 {
		t.Fatal("editableSettings exposed no json tags")
	}
	tagSet := make(map[string]bool, len(tags))
	for _, tag := range tags {
		tagSet[tag] = true
	}

	js := readSibling(t, "../frontend/web/static/js/settings.js")
	for _, tag := range tags {
		if !strings.Contains(js, tag) {
			t.Errorf("editableSettings json tag %q is not referenced in settings.js — a rename would silently drop the field", tag)
		}
	}

	// Every dropdown wired by data-field must name a real editable field.
	layout := readSibling(t, "../frontend/web/templates/layout.html")
	dfRe := regexp.MustCompile(`data-field="([a-zA-Z]+)"`)
	for _, m := range dfRe.FindAllStringSubmatch(layout, -1) {
		if !tagSet[m[1]] {
			t.Errorf("settings template data-field=%q is not an editableSettings field", m[1])
		}
	}
}

func jsonTags(t reflect.Type) []string {
	var out []string
	for i := range t.NumField() {
		tag := t.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		out = append(out, strings.Split(tag, ",")[0])
	}
	return out
}

func readSibling(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}
