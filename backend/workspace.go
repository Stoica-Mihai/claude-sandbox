package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// underWorkspace reports whether abs is workspaceRoot itself or a path beneath
// it (separator-anchored, so "/workspace-evil" does not qualify).
func underWorkspace(abs string) bool {
	return abs == workspaceRoot || strings.HasPrefix(abs, workspaceRoot+"/")
}

// errNotUnderWorkspace / errNotDir are the sentinel results of resolveWorkspaceDir
// so each caller can map them to its own status/message.
var (
	errNotUnderWorkspace = errors.New("path is not under the workspace root")
	errNotDir            = errors.New("path is not an existing directory")
)

// resolveWorkspaceDir resolves candidate to an absolute path and requires it to
// be an existing directory under /workspace. The absolute path is returned
// whenever it resolves (even on errNotDir) so callers can name it.
func resolveWorkspaceDir(candidate string) (string, error) {
	abs, err := filepath.Abs(candidate)
	if err != nil || !underWorkspace(abs) {
		return "", errNotUnderWorkspace
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return abs, errNotDir
	}
	return abs, nil
}

// validWorkspaceDir resolves cwd and ensures it is an existing directory under
// /workspace (used by Spawn/Resume, whose cwd is already an absolute path).
func validWorkspaceDir(cwd string) (string, error) {
	abs, err := resolveWorkspaceDir(cwd)
	if err != nil {
		if errors.Is(err, errNotDir) {
			return "", fmt.Errorf("directory does not exist: %s", abs)
		}
		return "", fmt.Errorf("directory must be under %s", workspaceRoot)
	}
	return abs, nil
}
