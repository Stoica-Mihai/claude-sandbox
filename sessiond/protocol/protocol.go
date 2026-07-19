// Package protocol is the wire contract between sessiond and the backend
// bridge: length-prefixed frames over unix sockets, one control socket for
// request/response ops and one socket per session for attach streams.
package protocol

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"time"
)

// Frame types. DATA/CONTROL mirror the WebSocket binary/text split so the
// backend bridge translates without interpretation.
const (
	FrameData     byte = 0x01 // raw PTY bytes, both directions
	FrameControl  byte = 0x02 // JSON Control (resize, deactivated, error)
	FrameSnapshot byte = 0x03 // rendered terminal replay, sessiond → client
	FrameClose    byte = 0x04 // JSON Close; terminal, sessiond → client
	FrameAttach   byte = 0x05 // JSON Attach handshake, client → sessiond
	FrameRequest  byte = 0x10 // JSON Request, control socket
	FrameResponse byte = 0x11 // JSON Response, control socket
)

// MaxFrame bounds a frame payload (snapshots with full scrollback are the
// largest legitimate frames).
const MaxFrame = 16 << 20

// Attach is the handshake opening a session stream; dimensions are mandatory
// so the snapshot renders at the viewer's real size.
type Attach struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// Control mirrors the WS JSON control contract ("resize", "deactivated", "error").
type Control struct {
	Type    string `json:"type"`
	Cols    uint16 `json:"cols,omitempty"`
	Rows    uint16 `json:"rows,omitempty"`
	Message string `json:"message,omitempty"`
}

// Close reasons.
const (
	CloseEnded  = "ended"  // claude exited
	CloseKilled = "killed" // kill op
)

// Close is the terminal frame on a session stream.
type Close struct {
	Reason string `json:"reason"`
}

// Control-socket ops.
const (
	OpSpawn = "spawn"
	OpList  = "list"
	OpKill  = "kill"
	OpPing  = "ping"
)

// Request is a control-socket request.
type Request struct {
	Op     string `json:"op"`
	CWD    string `json:"cwd,omitempty"`
	UUID   string `json:"uuid,omitempty"`
	Resume bool   `json:"resume,omitempty"`
	Name   string `json:"name,omitempty"`
}

// SessionInfo is one live session in a list response.
type SessionInfo struct {
	Name    string `json:"name"`
	CWD     string `json:"cwd"`
	UUID    string `json:"uuid"`
	Created int64  `json:"created"`
}

// Response is a control-socket response.
type Response struct {
	OK       bool          `json:"ok"`
	Error    string        `json:"error,omitempty"`
	Name     string        `json:"name,omitempty"`
	Sessions []SessionInfo `json:"sessions,omitempty"`
}

// ControlSock returns the control socket path under the socket dir.
func ControlSock(dir string) string { return filepath.Join(dir, "control.sock") }

// SessionSock returns a session's stream socket path.
func SessionSock(dir, name string) string { return filepath.Join(dir, name+".sock") }

// WriteFrame writes one frame: type byte, big-endian u32 length, payload.
func WriteFrame(w io.Writer, typ byte, payload []byte) error {
	if len(payload) > MaxFrame {
		return fmt.Errorf("frame payload %d exceeds max %d", len(payload), MaxFrame)
	}
	var hdr [5]byte
	hdr[0] = typ
	binary.BigEndian.PutUint32(hdr[1:], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

// WriteJSONFrame marshals v and writes it as a frame of the given type.
func WriteJSONFrame(w io.Writer, typ byte, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return WriteFrame(w, typ, b)
}

// ReadFrame reads one frame, rejecting payloads over MaxFrame.
func ReadFrame(r io.Reader) (byte, []byte, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(hdr[1:])
	if n > MaxFrame {
		return 0, nil, fmt.Errorf("frame payload %d exceeds max %d", n, MaxFrame)
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	return hdr[0], payload, nil
}

// Do performs one control-socket request/response round-trip.
func Do(sockDir string, req Request) (Response, error) {
	var resp Response
	conn, err := net.DialTimeout("unix", ControlSock(sockDir), 5*time.Second)
	if err != nil {
		return resp, fmt.Errorf("dial sessiond control: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if err := WriteJSONFrame(conn, FrameRequest, req); err != nil {
		return resp, err
	}
	typ, payload, err := ReadFrame(conn)
	if err != nil {
		return resp, err
	}
	if typ != FrameResponse {
		return resp, fmt.Errorf("unexpected frame type 0x%02x from control socket", typ)
	}
	if err := json.Unmarshal(payload, &resp); err != nil {
		return resp, err
	}
	return resp, nil
}
