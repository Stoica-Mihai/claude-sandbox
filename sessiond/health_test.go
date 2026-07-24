package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthHandler(t *testing.T) {
	cases := []struct {
		name     string
		check    func() error
		wantCode int
		wantBody string
	}{
		{"ok", func() error { return nil }, http.StatusOK, "ok"},
		{"down", func() error { return errors.New("socket refused") }, http.StatusServiceUnavailable, "unavailable"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			healthHandler(c.check)(rec, httptest.NewRequest("GET", "/healthz", nil))
			if rec.Code != c.wantCode {
				t.Errorf("code = %d, want %d", rec.Code, c.wantCode)
			}
			if !strings.Contains(rec.Body.String(), c.wantBody) {
				t.Errorf("body = %q, want contains %q", rec.Body.String(), c.wantBody)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("content-type = %q, want application/json", ct)
			}
		})
	}
}
