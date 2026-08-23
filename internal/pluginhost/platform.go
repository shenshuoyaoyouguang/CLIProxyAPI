package pluginhost

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/pluginhost/discovery"
)

// pluginFile aliases the shared discovery type; the host loader, ABI, and
// platform loaders all speak this vocabulary.
type pluginFile = discovery.File

// PluginFileInfo describes a plugin binary selected by the host discovery rules.
type PluginFileInfo = discovery.File

// ValidatePluginID reports whether id can be used as a plugin configuration key.
func ValidatePluginID(id string) bool {
	return discovery.ValidID(id)
}

func selectPluginFiles(root string, desiredVersions ...map[string]string) ([]pluginFile, error) {
	selected, _, errSelect := selectPluginFilesWithCandidates(root, desiredVersions...)
	return selected, errSelect
}

func selectPluginFilesWithCandidates(root string, desiredVersions ...map[string]string) ([]pluginFile, []pluginFile, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "plugins"
	}
	desired := normalizeDesiredPluginVersions(desiredVersions...)

	all, errAll := discovery.AllFiles(root)
	if errAll != nil {
		return nil, nil, errAll
	}
	selectedByID := make(map[string]pluginFile)
	order := make([]string, 0)
	for _, file := range all {
		current, exists := selectedByID[file.ID]
		if !exists {
			selectedByID[file.ID] = file
			order = append(order, file.ID)
			continue
		}
		if pluginFilePreferredForDesired(file, current, desired[file.ID]) {
			selectedByID[file.ID] = file
		}
	}
	selected := make([]pluginFile, 0, len(order))
	for _, id := range order {
		file := selectedByID[id]
		if desiredVersion := desired[id]; desiredVersion != "" && file.Version != desiredVersion {
			continue
		}
		selected = append(selected, file)
	}
	return selected, all, nil
}

func normalizeDesiredPluginVersions(sources ...map[string]string) map[string]string {
	out := make(map[string]string)
	for _, source := range sources {
		for id, version := range source {
			id = strings.TrimSpace(id)
			version = normalizePluginDesiredVersion(version)
			if id == "" || version == "" {
				continue
			}
			out[id] = version
		}
	}
	return out
}

func pluginFilePreferredForDesired(candidate pluginFile, current pluginFile, desiredVersion string) bool {
	desiredVersion = normalizePluginDesiredVersion(desiredVersion)
	if desiredVersion != "" {
		candidateMatches := candidate.Version == desiredVersion
		currentMatches := current.Version == desiredVersion
		if candidateMatches != currentMatches {
			return candidateMatches
		}
	}
	return pluginFilePreferred(candidate, current)
}

func pluginFilePreferred(candidate pluginFile, current pluginFile) bool {
	if candidate.Version == "" {
		return false
	}
	if current.Version == "" {
		return true
	}
	comparison, comparable := comparePluginVersions(candidate.Version, current.Version)
	if !comparable {
		return candidate.Version > current.Version
	}
	return comparison > 0
}

func comparePluginVersions(a, b string) (int, bool) {
	segmentsA := strings.Split(a, ".")
	segmentsB := strings.Split(b, ".")
	length := len(segmentsA)
	if len(segmentsB) > length {
		length = len(segmentsB)
	}
	for index := 0; index < length; index++ {
		numberA, okA := pluginVersionSegment(segmentsA, index)
		numberB, okB := pluginVersionSegment(segmentsB, index)
		if !okA || !okB {
			return 0, false
		}
		if numberA != numberB {
			if numberA < numberB {
				return -1, true
			}
			return 1, true
		}
	}
	return 0, true
}

func pluginVersionSegment(segments []string, index int) (int64, bool) {
	if index >= len(segments) {
		return 0, true
	}
	number, errParse := strconv.ParseInt(segments[index], 10, 64)
	if errParse != nil || number < 0 {
		return 0, false
	}
	return number, true
}

func cleanupUnselectedPluginFiles(root string, loaded []pluginFile) error {
	if len(loaded) == 0 {
		return nil
	}
	_, candidates, errSelect := selectPluginFilesWithCandidates(root)
	if errSelect != nil {
		return errSelect
	}
	loadedByID := make(map[string]map[string]struct{}, len(loaded))
	for _, file := range loaded {
		if strings.TrimSpace(file.ID) == "" || strings.TrimSpace(file.Path) == "" {
			continue
		}
		paths := loadedByID[file.ID]
		if paths == nil {
			paths = make(map[string]struct{})
			loadedByID[file.ID] = paths
		}
		paths[filepath.Clean(file.Path)] = struct{}{}
	}
	var errs []error
	for _, candidate := range candidates {
		paths := loadedByID[candidate.ID]
		if len(paths) == 0 {
			continue
		}
		if _, selected := paths[filepath.Clean(candidate.Path)]; selected {
			continue
		}
		if errRemove := os.Remove(candidate.Path); errRemove != nil && !errors.Is(errRemove, os.ErrNotExist) {
			errs = append(errs, errRemove)
			log.WithError(errRemove).Warnf("pluginhost: failed to remove old plugin file %s", candidate.Path)
			continue
		}
		log.WithFields(pluginLogFields(candidate.ID, "", candidate.Version, candidate.Path)).Info("pluginhost: old plugin file removed")
	}
	return errors.Join(errs...)
}

// DiscoverPluginFiles returns plugin binaries selected by the current host discovery rules.
func DiscoverPluginFiles(root string, desiredVersions ...map[string]string) ([]PluginFileInfo, error) {
	files, errSelect := selectPluginFiles(root, desiredVersions...)
	if errSelect != nil {
		return nil, errSelect
	}
	return files, nil
}
