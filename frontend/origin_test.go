package main

import (
	"net/http"
	"testing"
)

func req(origin, host string) *http.Request {
	r := &http.Request{Header: http.Header{}, Host: host}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	return r
}

func TestCheckWSOrigin(t *testing.T) {
	allowedWSOrigins = map[string]bool{"https://proxy.example.com": true}

	cases := []struct {
		name   string
		origin string
		host   string
		want   bool
	}{
		{"same origin", "http://localhost:8080", "localhost:8080", true},
		{"no origin (non-browser)", "", "localhost:8080", true},
		{"allowlisted", "https://proxy.example.com", "localhost:8080", true},
		{"cross origin", "https://evil.com", "localhost:8080", false},
		{"malformed origin", "://nope", "localhost:8080", false},
		{"host mismatch port", "http://localhost:9999", "localhost:8080", false},
	}
	for _, c := range cases {
		if got := checkWSOrigin(req(c.origin, c.host)); got != c.want {
			t.Errorf("%s: checkWSOrigin(origin=%q host=%q)=%v want %v", c.name, c.origin, c.host, got, c.want)
		}
	}
}
