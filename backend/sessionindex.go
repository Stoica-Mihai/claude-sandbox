package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
)

// indexEntry is a recorded conversation: its folder, creation time, optional name.
type indexEntry struct {
	CWD     string `json:"cwd"`
	Created int64  `json:"created"`
	Name    string `json:"name,omitempty"`
}

// SessionHistoryEntry is the API view of a previous session in a folder.
type SessionHistoryEntry struct {
	UUID    string `json:"uuid"`
	Created int64  `json:"created"`
	Name    string `json:"name,omitempty"`
}

// SessionIndex is the persisted uuid → conversation map. It is the source for the
// per-folder resume list and for custom display names, and lives in the
// host-mounted config dir so it survives container restarts.
type SessionIndex struct {
	mu      sync.Mutex
	entries map[string]indexEntry
}

// loadSessionIndex reads the index from disk (empty if absent/unparseable).
func loadSessionIndex() *SessionIndex {
	idx := &SessionIndex{entries: map[string]indexEntry{}}
	if data, err := os.ReadFile(sessionIndexPath()); err == nil {
		_ = json.Unmarshal(data, &idx.entries)
	}
	return idx
}

// save writes the index atomically. Caller holds mu.
func (s *SessionIndex) save() error {
	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session index: %w", err)
	}
	if err := writeFileAtomic(sessionIndexPath(), data); err != nil {
		return fmt.Errorf("write session index: %w", err)
	}
	return nil
}

// add records a new conversation (no-op if the uuid already exists). On a
// persist failure the in-memory entry is rolled back so it never diverges from
// disk.
func (s *SessionIndex) add(uuid, cwd string, created int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[uuid]; ok {
		return nil
	}
	s.entries[uuid] = indexEntry{CWD: cwd, Created: created}
	if err := s.save(); err != nil {
		delete(s.entries, uuid)
		return err
	}
	return nil
}

// remove deletes a conversation from the index (no-op if the uuid is absent).
// A persist failure restores the entry so a caller reporting failure and the
// on-disk state agree.
func (s *SessionIndex) remove(uuid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[uuid]
	if !ok {
		return nil
	}
	delete(s.entries, uuid)
	if err := s.save(); err != nil {
		s.entries[uuid] = e
		return err
	}
	return nil
}

// setName sets or clears a conversation's custom name. A persist failure
// restores the previous name.
func (s *SessionIndex) setName(uuid, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[uuid]
	if !ok {
		return nil
	}
	prev := e.Name
	e.Name = name
	s.entries[uuid] = e
	if err := s.save(); err != nil {
		e.Name = prev
		s.entries[uuid] = e
		return err
	}
	return nil
}

// name returns a conversation's custom name, or "" if unset/unknown.
func (s *SessionIndex) name(uuid string) string {
	if uuid == "" {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.entries[uuid].Name
}

// cwd returns a conversation's recorded working directory.
func (s *SessionIndex) cwd(uuid string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[uuid]
	return e.CWD, ok
}

// listByCwd returns the conversations recorded for a folder, newest first.
func (s *SessionIndex) listByCwd(cwd string) []SessionHistoryEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []SessionHistoryEntry{}
	for id, e := range s.entries {
		if e.CWD == cwd {
			out = append(out, SessionHistoryEntry{UUID: id, Created: e.Created, Name: e.Name})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created > out[j].Created })
	return out
}
