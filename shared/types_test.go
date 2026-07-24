package api

import (
	"encoding/json"
	"testing"
)

func TestCreateDirectoryRequestUnmarshal(t *testing.T) {
	const body = `{"path":"sub/dir","name":"proj","gitInit":true}`
	var got CreateDirectoryRequest
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := CreateDirectoryRequest{Path: "sub/dir", Name: "proj", GitInit: true}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestCreateDirectoryRequestJSONKeys(t *testing.T) {
	b, err := json.Marshal(CreateDirectoryRequest{Path: "p", Name: "n", GitInit: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}
	for _, key := range []string{"path", "name", "gitInit"} {
		if _, ok := m[key]; !ok {
			t.Errorf("marshaled JSON missing key %q: %s", key, b)
		}
	}
	if len(m) != 3 {
		t.Errorf("got %d keys, want 3: %s", len(m), b)
	}
}

func TestCreateDirectoryRequestRoundTrip(t *testing.T) {
	cases := []CreateDirectoryRequest{
		{},
		{Path: "", Name: "proj", GitInit: false},
		{Path: "a/b/c", Name: "new-project.1_2", GitInit: true},
	}
	for _, want := range cases {
		b, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("marshal %+v: %v", want, err)
		}
		var got CreateDirectoryRequest
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal %s: %v", b, err)
		}
		if got != want {
			t.Errorf("round-trip: got %+v, want %+v", got, want)
		}
	}
}

func TestCreateDirectoryResponseOmitsEmptyWarning(t *testing.T) {
	b, err := json.Marshal(CreateDirectoryResponse{Path: "sub/proj"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}
	if _, ok := m["warning"]; ok {
		t.Errorf("empty Warning must be omitted, got %s", b)
	}
	if got, _ := m["path"].(string); got != "sub/proj" {
		t.Errorf("path = %q, want %q: %s", got, "sub/proj", b)
	}
	if len(m) != 1 {
		t.Errorf("got %d keys, want 1 (path only): %s", len(m), b)
	}
}

// TestSpawnWireShapes pins the spawn request/response JSON keys both services
// depend on.
func TestSpawnWireShapes(t *testing.T) {
	b, err := json.Marshal(SpawnRequest{CWD: "/workspace/x", Resume: "u1"})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if got, want := string(b), `{"cwd":"/workspace/x","resume":"u1"}`; got != want {
		t.Errorf("SpawnRequest JSON = %s, want %s", got, want)
	}
	b, _ = json.Marshal(SpawnRequest{CWD: "/w"})
	if got, want := string(b), `{"cwd":"/w"}`; got != want {
		t.Errorf("SpawnRequest without resume = %s, want %s (resume omitted)", got, want)
	}

	b, err = json.Marshal(SpawnResponse{SessionName: "claude-abc"})
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	if got, want := string(b), `{"session_name":"claude-abc"}`; got != want {
		t.Errorf("SpawnResponse JSON = %s, want %s", got, want)
	}
}

// TestShareStatusWireShape pins the ShareStatus envelope against the sidecar's
// JSON (holesail/server.js status()): null url/error when not applicable.
func TestShareStatusWireShape(t *testing.T) {
	b, err := json.Marshal(ShareStatus{State: SharePrivate})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got, want := string(b), `{"state":"private","url":null,"error":null}`; got != want {
		t.Errorf("private ShareStatus = %s, want %s", got, want)
	}

	// Round-trip the exact string server.js produces when public.
	const sidecar = `{"state":"public","url":"hs://s000ab","error":null}`
	var got ShareStatus
	if err := json.Unmarshal([]byte(sidecar), &got); err != nil {
		t.Fatalf("unmarshal sidecar status: %v", err)
	}
	if got.State != SharePublic || got.URL == nil || *got.URL != "hs://s000ab" || got.Error != nil {
		t.Errorf("sidecar status decoded to %+v", got)
	}
}

// TestShareLogsStatusWireShape pins the /api/share/logs envelope the frontend
// serves and share.js consumes.
func TestShareLogsStatusWireShape(t *testing.T) {
	b, err := json.Marshal(ShareLogsStatus{Enabled: false})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got, want := string(b), `{"enabled":false}`; got != want {
		t.Errorf("ShareLogsStatus = %s, want %s", got, want)
	}
	var got ShareLogsStatus
	if err := json.Unmarshal([]byte(`{"enabled":true}`), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.Enabled {
		t.Errorf("decoded Enabled = false, want true")
	}
}

func TestCreateDirectoryResponseIncludesWarning(t *testing.T) {
	b, err := json.Marshal(CreateDirectoryResponse{Path: "sub/proj", Warning: "git init failed"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got CreateDirectoryResponse
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal %s: %v", b, err)
	}
	want := CreateDirectoryResponse{Path: "sub/proj", Warning: "git init failed"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}
	if _, ok := m["warning"]; !ok {
		t.Errorf("non-empty Warning must be present, got %s", b)
	}
}
