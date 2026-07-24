package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// fileOffset is the resumable read position for one source file: the byte where
// the unconsumed remainder begins, plus the inode it was recorded against (so a
// rotation while logd was down is detected as an inode mismatch on resume).
type fileOffset struct {
	Inode  uint64 `json:"inode"`
	Offset int64  `json:"offset"`
}

// offsetStore persists per-file read offsets to a JSON file via atomic
// temp+rename, so logd resumes where it left off after a restart (at-least-once).
type offsetStore struct {
	mu   sync.Mutex
	path string
	data map[string]fileOffset
}

func loadOffsets(path string) *offsetStore {
	s := &offsetStore{path: path, data: map[string]fileOffset{}}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &s.data)
	}
	return s
}

func (s *offsetStore) get(key string) (fileOffset, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.data[key]
	return o, ok
}

func (s *offsetStore) set(key string, o fileOffset) {
	s.mu.Lock()
	s.data[key] = o
	s.mu.Unlock()
}

// flush writes the current offsets atomically (temp file in the same dir, then
// rename) so a crash mid-write can't leave a truncated checkpoint.
func (s *offsetStore) flush() error {
	s.mu.Lock()
	b, err := json.Marshal(s.data)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".offsets-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, s.path)
}
