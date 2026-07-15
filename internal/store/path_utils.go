package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ResolveManagedPath(baseDir, path string) (string, error) {
	baseDir = strings.TrimSpace(baseDir)
	path = strings.TrimSpace(path)
	if baseDir == "" {
		return "", fmt.Errorf("managed path: base directory not configured")
	}
	if path == "" {
		return "", fmt.Errorf("managed path: path is empty")
	}

	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("managed path: resolve base directory: %w", err)
	}
	absBase = filepath.Clean(absBase)

	candidate := filepath.FromSlash(path)
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(absBase, candidate)
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("managed path: resolve path: %w", err)
	}
	absCandidate = filepath.Clean(absCandidate)

	rel, err := filepath.Rel(absBase, absCandidate)
	if err != nil {
		return "", fmt.Errorf("managed path: compute relative path: %w", err)
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("managed path: path outside managed directory")
	}
	return absCandidate, nil
}
