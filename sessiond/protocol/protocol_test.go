package protocol

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	cases := []struct {
		typ     byte
		payload []byte
	}{
		{FrameData, []byte("hello \x1b[31mworld")},
		{FrameControl, []byte(`{"type":"resize","cols":80,"rows":24}`)},
		{FrameSnapshot, bytes.Repeat([]byte{0xAB}, 1<<16)},
		{FrameClose, []byte(`{"reason":"ended"}`)},
		{FrameAttach, []byte(`{"cols":120,"rows":30}`)},
		{FrameData, nil}, // zero payload
	}
	var buf bytes.Buffer
	for _, c := range cases {
		if err := WriteFrame(&buf, c.typ, c.payload); err != nil {
			t.Fatalf("WriteFrame(0x%02x): %v", c.typ, err)
		}
	}
	for _, c := range cases {
		typ, payload, err := ReadFrame(&buf)
		if err != nil {
			t.Fatalf("ReadFrame: %v", err)
		}
		if typ != c.typ {
			t.Fatalf("type = 0x%02x, want 0x%02x", typ, c.typ)
		}
		if !bytes.Equal(payload, c.payload) {
			t.Fatalf("payload mismatch for type 0x%02x: %d bytes vs %d", c.typ, len(payload), len(c.payload))
		}
	}
	if buf.Len() != 0 {
		t.Fatalf("%d leftover bytes after reading all frames", buf.Len())
	}
}

func TestReadFrameTruncated(t *testing.T) {
	var full bytes.Buffer
	if err := WriteFrame(&full, FrameData, []byte("abcdef")); err != nil {
		t.Fatal(err)
	}
	raw := full.Bytes()

	// Cut inside the header and inside the payload.
	for _, n := range []int{0, 3, len(raw) - 2} {
		_, _, err := ReadFrame(bytes.NewReader(raw[:n]))
		if err == nil {
			t.Fatalf("ReadFrame on %d/%d bytes: want error, got nil", n, len(raw))
		}
	}
}

func TestFrameOversize(t *testing.T) {
	if err := WriteFrame(io.Discard, FrameData, make([]byte, MaxFrame+1)); err == nil {
		t.Fatal("WriteFrame over MaxFrame: want error, got nil")
	}

	var hdr [5]byte
	hdr[0] = FrameData
	binary.BigEndian.PutUint32(hdr[1:], MaxFrame+1)
	if _, _, err := ReadFrame(bytes.NewReader(hdr[:])); err == nil {
		t.Fatal("ReadFrame with oversize length: want error, got nil")
	}
}

func TestJSONFrame(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSONFrame(&buf, FrameControl, Control{Type: "resize", Cols: 91, Rows: 22}); err != nil {
		t.Fatal(err)
	}
	typ, payload, err := ReadFrame(&buf)
	if err != nil || typ != FrameControl {
		t.Fatalf("typ=0x%02x err=%v", typ, err)
	}
	want := `{"type":"resize","cols":91,"rows":22}`
	if string(payload) != want {
		t.Fatalf("payload = %s, want %s", payload, want)
	}
}

func TestControlDo(t *testing.T) {
	dir := t.TempDir()
	ln, err := net.Listen("unix", ControlSock(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		typ, payload, err := ReadFrame(conn)
		if err != nil || typ != FrameRequest {
			return
		}
		if !bytes.Contains(payload, []byte(`"ping"`)) {
			_ = WriteJSONFrame(conn, FrameResponse, Response{OK: false, Error: "bad op"})
			return
		}
		_ = WriteJSONFrame(conn, FrameResponse, Response{OK: true})
	}()

	resp, err := Do(dir, Request{Op: OpPing})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if !resp.OK {
		t.Fatalf("resp = %+v, want OK", resp)
	}
}
