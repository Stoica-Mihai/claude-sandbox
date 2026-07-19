package main

import (
	"errors"
	"fmt"
	"net"
	"time"

	"claude-sandbox-sessiond/protocol"
)

// sessionHost is the backend's view of sessiond: spawn/list/kill on the
// control socket plus per-session attach dials. Faked in tests.
type sessionHost interface {
	Spawn(cwd, uuid string, resume bool) (string, error)
	List() ([]protocol.SessionInfo, error)
	Kill(name string) error
	DialSession(name string) (net.Conn, error)
}

// errHostSession distinguishes "sessiond doesn't know this session" from
// transport failures, so Kill can treat it as already-dead.
var errHostSession = errors.New("unknown to sessiond")

// protocolHost talks to the real sessiond over the shared socket dir.
type protocolHost struct {
	sockDir string
}

func (h *protocolHost) do(req protocol.Request) (protocol.Response, error) {
	resp, err := protocol.Do(h.sockDir, req)
	if err != nil {
		return resp, fmt.Errorf("sessiond %s: %w", req.Op, err)
	}
	return resp, nil
}

func (h *protocolHost) Spawn(cwd, uuid string, resume bool) (string, error) {
	resp, err := h.do(protocol.Request{Op: protocol.OpSpawn, CWD: cwd, UUID: uuid, Resume: resume})
	if err != nil {
		return "", err
	}
	if !resp.OK {
		return "", fmt.Errorf("sessiond spawn: %s", resp.Error)
	}
	return resp.Name, nil
}

func (h *protocolHost) List() ([]protocol.SessionInfo, error) {
	resp, err := h.do(protocol.Request{Op: protocol.OpList})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("sessiond list: %s", resp.Error)
	}
	return resp.Sessions, nil
}

func (h *protocolHost) Kill(name string) error {
	resp, err := h.do(protocol.Request{Op: protocol.OpKill, Name: name})
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("%w: %s", errHostSession, resp.Error)
	}
	return nil
}

func (h *protocolHost) DialSession(name string) (net.Conn, error) {
	return net.DialTimeout("unix", protocol.SessionSock(h.sockDir, name), 5*time.Second)
}
