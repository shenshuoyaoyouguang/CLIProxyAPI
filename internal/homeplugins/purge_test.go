package homeplugins

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/pluginhost/discovery"
)

// writePluginArtifact creates a regular file with the given content.
func writePluginArtifact(t *testing.T, path string, content string) {
	t.Helper()
	if errMkdir := os.MkdirAll(filepath.Dir(path), 0o755); errMkdir != nil {
		t.Fatalf("MkdirAll() error = %v", errMkdir)
	}
	if errWrite := os.WriteFile(path, []byte(content), 0o644); errWrite != nil {
		t.Fatalf("WriteFile() error = %v", errWrite)
	}
}

func TestPurgeMalformedArtifactsRemovesInvalidKeepsValid(t *testing.T) {
	root := t.TempDir()
	extension := discoveryExtensionForTest()

	malformedNames := []string{
		"_old" + extension,
		".hidden" + extension,
		"sample-v1~2" + extension,
	}
	validName := "good-plugin-v1.2.3" + extension

	// Malformed files in both candidate dirs; valid files alongside them.
	for _, dir := range discoveryDirsForTest(root) {
		for _, name := range malformedNames {
			writePluginArtifact(t, filepath.Join(dir, name), "junk-"+name)
		}
		writePluginArtifact(t, filepath.Join(dir, validName), "valid")
	}

	purgeMalformedArtifacts(root)

	for _, dir := range discoveryDirsForTest(root) {
		for _, name := range malformedNames {
			if _, errStat := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(errStat) {
				t.Errorf("malformed artifact %s still present after purge", filepath.Join(dir, name))
			}
		}
		if _, errStat := os.Stat(filepath.Join(dir, validName)); errStat != nil {
			t.Errorf("valid artifact %s was removed: %v", filepath.Join(dir, validName), errStat)
		}
	}

	// Idempotent: second run finds nothing to purge and does not fail.
	purgeMalformedArtifacts(root)
}

func TestPurgeMalformedArtifactReasons(t *testing.T) {
	extension := discoveryExtensionForTest()
	cases := map[string]string{
		"_old" + extension:        "invalid plugin id",
		".hidden" + extension:     "invalid plugin id",
		"sample-v1~2" + extension: "invalid version suffix",
		"ok-v" + extension:        "invalid version suffix",
	}
	for name, want := range cases {
		if got := invalidArtifactReason(name, extension); got != want {
			t.Errorf("invalidArtifactReason(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestSyncPlatformPurgesMalformedArtifacts(t *testing.T) {
	root := t.TempDir()
	extension := discoveryExtensionForTest()
	stale := filepath.Join(discoveryDirsForTest(root)[0], "_legacy"+extension)
	writePluginArtifact(t, stale, "junk")

	cfg := &config.Config{
		Home:    config.HomeConfig{Enabled: true},
		Plugins: config.PluginsConfig{Enabled: true, Dir: root},
	}
	report, errSync := SyncPlatformWithReport(t.Context(), cfg, &fakePluginRuntime{}, CurrentPlatform())
	if errSync != nil {
		t.Fatalf("SyncPlatformWithReport() error = %v", errSync)
	}
	if !report.OK {
		t.Fatalf("SyncPlatformWithReport() not OK: %s", report.Error)
	}
	if _, errStat := os.Stat(stale); !os.IsNotExist(errStat) {
		t.Errorf("stale artifact %s still present after sync", stale)
	}
}

func discoveryExtensionForTest() string {
	return discovery.Extension(runtime.GOOS)
}

func discoveryDirsForTest(root string) []string {
	return []string{
		filepath.Join(root, runtime.GOOS, runtime.GOARCH),
		root,
	}
}
