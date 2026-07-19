package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestUIPrefsRoundTrip(t *testing.T) {
	testConfigDir(t)
	s := &Server{}

	// Unset: GET returns empty strings.
	rec := httptest.NewRecorder()
	s.handleGetUIPrefs(rec, httptest.NewRequest(http.MethodGet, "/api/ui-prefs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET unset: expected 200, got %d", rec.Code)
	}
	var got uiPrefs
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Accent != "" || got.Theme != "" {
		t.Fatalf("GET unset: expected empty prefs, got %+v", got)
	}

	// PUT valid prefs, then GET returns them.
	body, _ := json.Marshal(uiPrefs{Accent: "Cyan", Theme: "light"})
	rec = httptest.NewRecorder()
	s.handlePutUIPrefs(rec, httptest.NewRequest(http.MethodPut, "/api/ui-prefs", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT valid: expected 200, got %d (%s)", rec.Code, rec.Body)
	}

	rec = httptest.NewRecorder()
	s.handleGetUIPrefs(rec, httptest.NewRequest(http.MethodGet, "/api/ui-prefs", nil))
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Accent != "Cyan" || got.Theme != "light" {
		t.Fatalf("GET after PUT: expected Cyan/light, got %+v", got)
	}

	if _, err := os.Stat(dashboardPrefsPath()); err != nil {
		t.Fatalf("prefs file not written: %v", err)
	}
}

func TestUIPrefsRejectsInvalid(t *testing.T) {
	testConfigDir(t)
	s := &Server{}

	cases := []uiPrefs{
		{Accent: "Chartreuse", Theme: "dark"}, // bad accent
		{Accent: "Red", Theme: "sepia"},       // bad theme
		{Accent: "", Theme: "dark"},           // empty accent
	}
	for _, c := range cases {
		body, _ := json.Marshal(c)
		rec := httptest.NewRecorder()
		s.handlePutUIPrefs(rec, httptest.NewRequest(http.MethodPut, "/api/ui-prefs", bytes.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("PUT %+v: expected 400, got %d", c, rec.Code)
		}
	}
	// A rejected PUT must not create the file.
	if _, err := os.Stat(dashboardPrefsPath()); !os.IsNotExist(err) {
		t.Fatalf("rejected PUT should not write the prefs file")
	}
}
