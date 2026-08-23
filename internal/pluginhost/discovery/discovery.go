// Package discovery implements the shared plugin binary file walk: candidate
// directories, per-platform extension rules, and filename parsing. It is a
// leaf package (stdlib only) so that both internal/pluginhost and
// internal/homeplugins can depend on it without an import cycle.
package discovery

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

var (
	idPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	versionPattern = regexp.MustCompile(`^[0-9][0-9A-Za-z.+-]*$`)
)

// File describes a plugin binary found on disk.
type File struct {
	ID      string
	Path    string
	Version string
}

// ValidID reports whether id can be used as a plugin configuration key.
func ValidID(id string) bool {
	return idPattern.MatchString(id)
}

// ValidVersion reports whether version is a usable plugin version suffix.
func ValidVersion(version string) bool {
	return version != "" && !strings.HasPrefix(version, "v") && versionPattern.MatchString(version)
}

// Extension reports the plugin binary file extension used on goos.
func Extension(goos string) string {
	switch goos {
	case "darwin":
		return ".dylib"
	case "windows":
		return ".dll"
	default:
		return ".so"
	}
}

// Dirs lists the candidate plugin directories for root, most specific first.
func Dirs(root, goos, goarch string) []string {
	dirs := make([]string, 0, 2)
	dirs = append(dirs, filepath.Join(root, goos, goarch))
	dirs = append(dirs, root)
	return dirs
}

// FromPath parses a plugin binary filename into its id and optional version.
func FromPath(filePath string, requiredExtension string) (File, bool) {
	base := filepath.Base(filePath)
	lowerBase := strings.ToLower(base)
	extension := strings.TrimSpace(requiredExtension)
	if extension != "" {
		if !strings.HasSuffix(lowerBase, strings.ToLower(extension)) {
			return File{}, false
		}
	} else {
		for _, candidateExtension := range []string{".so", ".dylib", ".dll"} {
			if strings.HasSuffix(lowerBase, candidateExtension) {
				extension = candidateExtension
				break
			}
		}
		if extension == "" {
			return File{}, false
		}
	}
	name := base[:len(base)-len(extension)]
	id := name
	version := ""
	if versionIndex := strings.LastIndex(name, "-v"); versionIndex > 0 {
		candidateID := name[:versionIndex]
		candidateVersion := name[versionIndex+2:]
		if ValidID(candidateID) && ValidVersion(candidateVersion) {
			id = candidateID
			version = candidateVersion
		}
	}
	if !ValidID(id) {
		return File{}, false
	}
	return File{ID: id, Path: filePath, Version: version}, true
}

// AllFiles walks every candidate directory under root and returns all parsable
// plugin files in discovery order (platform directory before root, sorted by
// name within each).
func AllFiles(root string) ([]File, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "plugins"
	}
	candidates := Dirs(root, runtime.GOOS, runtime.GOARCH)
	extension := Extension(runtime.GOOS)
	all := make([]File, 0)
	for _, dir := range candidates {
		entries, errReadDir := os.ReadDir(dir)
		if errReadDir != nil {
			if errors.Is(errReadDir, os.ErrNotExist) {
				continue
			}
			return nil, errReadDir
		}
		files := make([]string, 0, len(entries))
		for _, entry := range entries {
			if entry == nil || !entry.Type().IsRegular() {
				continue
			}
			if strings.HasSuffix(strings.ToLower(entry.Name()), extension) {
				files = append(files, filepath.Join(dir, entry.Name()))
			}
		}
		sort.Strings(files)
		for _, filePath := range files {
			file, okFile := FromPath(filePath, extension)
			if !okFile {
				continue
			}
			all = append(all, file)
		}
	}
	return all, nil
}
