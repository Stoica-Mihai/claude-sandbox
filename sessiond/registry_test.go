package main

import (
	"net"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/creack/pty"

	"claude-sandbox-sessiond/protocol"
)

// startTestRegistry serves a registry whose sessions run `sleep` instead of
// claude, with the control socket live.
func startTestRegistry(t *testing.T) (*registry, string) {
	t.Helper()
	dir := t.TempDir()
	reg := newRegistry(dir)
	reg.newCommand = func(cwd, uuid string, resume bool) *exec.Cmd {
		cmd := exec.Command("sleep", "30")
		cmd.Dir = cwd
		return cmd
	}
	ln, err := net.Listen("unix", protocol.ControlSock(dir))
	if err != nil {
		t.Fatal(err)
	}
	go reg.serveControl(ln)
	t.Cleanup(func() {
		ln.Close()
		reg.shutdown(3 * time.Second)
	})
	return reg, dir
}

func TestControlPing(t *testing.T) {
	_, dir := startTestRegistry(t)
	resp, err := protocol.Do(dir, protocol.Request{Op: protocol.OpPing})
	if err != nil || !resp.OK {
		t.Fatalf("ping: resp=%+v err=%v", resp, err)
	}
}

func TestControlSpawnListKill(t *testing.T) {
	_, dir := startTestRegistry(t)

	resp, err := protocol.Do(dir, protocol.Request{Op: protocol.OpSpawn, CWD: t.TempDir(), UUID: "11111111-1111-4111-8111-111111111111"})
	if err != nil || !resp.OK || resp.Name == "" {
		t.Fatalf("spawn: resp=%+v err=%v", resp, err)
	}
	name := resp.Name

	resp, err = protocol.Do(dir, protocol.Request{Op: protocol.OpList})
	if err != nil || !resp.OK {
		t.Fatalf("list: resp=%+v err=%v", resp, err)
	}
	if len(resp.Sessions) != 1 || resp.Sessions[0].Name != name || resp.Sessions[0].UUID != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("list = %+v, want one session %s", resp.Sessions, name)
	}

	// The session socket accepts an attach.
	conn, err := net.DialTimeout("unix", protocol.SessionSock(dir, name), 2*time.Second)
	if err != nil {
		t.Fatalf("dial session socket: %v", err)
	}
	if err := protocol.WriteJSONFrame(conn, protocol.FrameAttach, protocol.Attach{Cols: 80, Rows: 24}); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	typ, _, err := protocol.ReadFrame(conn)
	if err != nil || typ != protocol.FrameSnapshot {
		t.Fatalf("attach reply: typ=0x%02x err=%v", typ, err)
	}
	conn.Close()

	resp, err = protocol.Do(dir, protocol.Request{Op: protocol.OpKill, Name: name})
	if err != nil || !resp.OK {
		t.Fatalf("kill: resp=%+v err=%v", resp, err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err = protocol.Do(dir, protocol.Request{Op: protocol.OpList})
		if err == nil && resp.OK && len(resp.Sessions) == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("session still listed after kill: %+v", resp.Sessions)
}

func TestControlKillUnknown(t *testing.T) {
	_, dir := startTestRegistry(t)
	resp, err := protocol.Do(dir, protocol.Request{Op: protocol.OpKill, Name: "claude-nope"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.OK || resp.Error == "" {
		t.Fatalf("kill unknown: resp=%+v, want error", resp)
	}
}

func TestControlUnknownOp(t *testing.T) {
	_, dir := startTestRegistry(t)
	resp, err := protocol.Do(dir, protocol.Request{Op: "bogus"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.OK || resp.Error == "" {
		t.Fatalf("bogus op: resp=%+v, want error", resp)
	}
}

// ptyOpen opens a real PTY pair with the master rewrapped pollable, as spawn does.
func ptyOpen() (*os.File, *os.File, error) {
	raw, tty, err := pty.Open()
	if err != nil {
		return nil, nil, err
	}
	master, err := pollableMaster(raw)
	if err != nil {
		_ = raw.Close()
		_ = tty.Close()
		return nil, nil, err
	}
	return master, tty, nil
}
