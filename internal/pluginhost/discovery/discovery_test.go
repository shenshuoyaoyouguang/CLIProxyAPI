package discovery

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCandidateDirs(t *testing.T) {
	got := Dirs("plugins", "darwin", "arm64")
	want := []string{
		filepath.Join("plugins", "darwin", "arm64"),
		"plugins",
	}
	if len(got) != len(want) {
		t.Fatalf("len(Dirs) = %d, want %d", len(got), len(want))
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("Dirs[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}

func TestPluginExtensionForPlatform(t *testing.T) {
	cases := []struct {
		goos string
		want string
	}{
		{goos: "linux", want: ".so"},
		{goos: "freebsd", want: ".so"},
		{goos: "darwin", want: ".dylib"},
		{goos: "windows", want: ".dll"},
	}

	for _, tc := range cases {
		if got := Extension(tc.goos); got != tc.want {
			t.Fatalf("Extension(%q) = %q, want %q", tc.goos, got, tc.want)
		}
	}
}

func TestPluginIDFromDynamicLibraryPath(t *testing.T) {
	cases := map[string]string{
		"plugins/example.so":    "example",
		"plugins/example.dylib": "example",
		"plugins/example.dll":   "example",
	}

	for path, want := range cases {
		file, ok := FromPath(path, "")
		if !ok {
			t.Fatalf("FromPath(%q) = not ok, want id %q", path, want)
		}
		if file.ID != want {
			t.Fatalf("FromPath(%q).ID = %q, want %q", path, file.ID, want)
		}
	}
}

func TestAllFilesReturnsEveryParsedFileInDiscoveryOrder(t *testing.T) {
	root := t.TempDir()
	archDir := filepath.Join(root, runtime.GOOS, runtime.GOARCH)
	if errMkdirAll := os.MkdirAll(archDir, 0o755); errMkdirAll != nil {
		t.Fatalf("MkdirAll() error = %v", errMkdirAll)
	}

	extension := Extension(runtime.GOOS)
	paths := []string{
		filepath.Join(root, "beta"+extension),
		filepath.Join(archDir, "alpha-v1.0.3"+extension),
		filepath.Join(archDir, "alpha-v1.0.4"+extension),
		filepath.Join(archDir, "ignored.txt"),
	}
	for _, path := range paths {
		if errWriteFile := os.WriteFile(path, []byte("x"), 0o644); errWriteFile != nil {
			t.Fatalf("WriteFile(%s) error = %v", path, errWriteFile)
		}
	}
	if errMkdir := os.Mkdir(filepath.Join(archDir, "dir"+extension), 0o755); errMkdir != nil {
		t.Fatalf("Mkdir() error = %v", errMkdir)
	}

	files, errAll := AllFiles(root)
	if errAll != nil {
		t.Fatalf("AllFiles() error = %v", errAll)
	}

	want := []File{
		{ID: "alpha", Path: filepath.Join(archDir, "alpha-v1.0.3"+extension), Version: "1.0.3"},
		{ID: "alpha", Path: filepath.Join(archDir, "alpha-v1.0.4"+extension), Version: "1.0.4"},
		{ID: "beta", Path: filepath.Join(root, "beta"+extension)},
	}
	if len(files) != len(want) {
		t.Fatalf("AllFiles() = %v, want %v", files, want)
	}
	for index := range want {
		if files[index] != want[index] {
			t.Fatalf("AllFiles()[%d] = %v, want %v", index, files[index], want[index])
		}
	}
}
