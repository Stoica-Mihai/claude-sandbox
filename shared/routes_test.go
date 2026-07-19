package api

import (
	"strings"
	"testing"
)

func TestPathBuilders(t *testing.T) {
	cases := []struct {
		got, want string
	}{
		{SessionPath("claude-abc"), "/api/sessions/claude-abc"},
		{SessionNamePath("claude-abc"), "/api/sessions/claude-abc/name"},
		{HistoryItemPath("uuid-1"), "/api/sessions/history/uuid-1"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("path = %q, want %q", c.got, c.want)
		}
		// A leftover brace means the builder filled the wrong placeholder name.
		if strings.ContainsAny(c.got, "{}") {
			t.Errorf("unfilled placeholder in %q", c.got)
		}
	}
}
