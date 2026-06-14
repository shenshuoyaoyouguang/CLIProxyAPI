package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResolvedScriptPathsRejectsRelativeSymlinkEscapingPluginDir(t *testing.T) {
	pluginDir := t.TempDir()
	outsideDir := t.TempDir()
	outsideScript := filepath.Join(outsideDir, "handler.js")
	if errWrite := os.WriteFile(outsideScript, []byte("function on_before_request(ctx) { return ctx; }\n"), 0600); errWrite != nil {
		t.Fatalf("os.WriteFile() error = %v", errWrite)
	}

	linkPath := filepath.Join(pluginDir, "handler.js")
	if errSymlink := os.Symlink(outsideScript, linkPath); errSymlink != nil {
		t.Skipf("os.Symlink() is not available: %v", errSymlink)
	}

	cfg := jsHandlerConfig{ScriptPaths: []string{"handler.js"}}
	_, errResolve := cfg.resolvedScriptPaths(pluginDir)
	if errResolve == nil {
		t.Fatal("resolvedScriptPaths() expected error for escaping symlink")
	}
	if !strings.Contains(errResolve.Error(), "escapes plugin_dir") {
		t.Fatalf("resolvedScriptPaths() error = %v, want escapes plugin_dir", errResolve)
	}
}

func TestResolvedScriptPathsAllowsRelativeSymlinkInsidePluginDir(t *testing.T) {
	pluginDir := t.TempDir()
	scriptsDir := filepath.Join(pluginDir, "scripts")
	if errMkdir := os.Mkdir(scriptsDir, 0700); errMkdir != nil {
		t.Fatalf("os.Mkdir() error = %v", errMkdir)
	}
	realScript := filepath.Join(scriptsDir, "handler.js")
	if errWrite := os.WriteFile(realScript, []byte("function on_before_request(ctx) { return ctx; }\n"), 0600); errWrite != nil {
		t.Fatalf("os.WriteFile() error = %v", errWrite)
	}

	linkPath := filepath.Join(pluginDir, "handler.js")
	if errSymlink := os.Symlink(realScript, linkPath); errSymlink != nil {
		t.Skipf("os.Symlink() is not available: %v", errSymlink)
	}

	cfg := jsHandlerConfig{ScriptPaths: []string{"handler.js"}}
	paths, errResolve := cfg.resolvedScriptPaths(pluginDir)
	if errResolve != nil {
		t.Fatalf("resolvedScriptPaths() error = %v", errResolve)
	}
	if len(paths) != 1 {
		t.Fatalf("resolvedScriptPaths() returned %d paths, want 1", len(paths))
	}
	resolvedRealScript, errEval := filepath.EvalSymlinks(realScript)
	if errEval != nil {
		t.Fatalf("filepath.EvalSymlinks() error = %v", errEval)
	}
	if paths[0] != resolvedRealScript {
		t.Fatalf("resolvedScriptPaths()[0] = %q, want %q", paths[0], resolvedRealScript)
	}
}

// parseJSHandlerConfig tests

func TestParseJSHandlerConfigDefaultsWhenEmpty(t *testing.T) {
	cfg, err := parseJSHandlerConfig([]byte{})
	if err != nil {
		t.Fatalf("parseJSHandlerConfig(empty) error = %v", err)
	}
	if !cfg.Enabled {
		t.Fatalf("default Enabled = false, want true")
	}
	if cfg.Timeout != 1*time.Second {
		t.Fatalf("default Timeout = %v, want 1s", cfg.Timeout)
	}
	if len(cfg.ScriptPaths) != 0 {
		t.Fatalf("default ScriptPaths = %v, want empty", cfg.ScriptPaths)
	}
}

func TestParseJSHandlerConfigDefaultsWhenWhitespaceOnly(t *testing.T) {
	cfg, err := parseJSHandlerConfig([]byte("   \n\t  "))
	if err != nil {
		t.Fatalf("parseJSHandlerConfig(whitespace) error = %v", err)
	}
	if cfg.Timeout != 1*time.Second {
		t.Fatalf("Timeout = %v, want 1s", cfg.Timeout)
	}
}

func TestParseJSHandlerConfigParsesTimeout(t *testing.T) {
	cfg, err := parseJSHandlerConfig([]byte(`timeout: 500ms`))
	if err != nil {
		t.Fatalf("parseJSHandlerConfig() error = %v", err)
	}
	if cfg.Timeout != 500*time.Millisecond {
		t.Fatalf("Timeout = %v, want 500ms", cfg.Timeout)
	}
}

func TestParseJSHandlerConfigParsesScriptPaths(t *testing.T) {
	yaml := `script_paths:
  - /abs/path/script.js
  - relative/script.js
`
	cfg, err := parseJSHandlerConfig([]byte(yaml))
	if err != nil {
		t.Fatalf("parseJSHandlerConfig() error = %v", err)
	}
	if len(cfg.ScriptPaths) != 2 {
		t.Fatalf("ScriptPaths len = %d, want 2", len(cfg.ScriptPaths))
	}
	if cfg.ScriptPaths[0] != "/abs/path/script.js" {
		t.Fatalf("ScriptPaths[0] = %q, want /abs/path/script.js", cfg.ScriptPaths[0])
	}
}

func TestParseJSHandlerConfigRejectsInvalidTimeout(t *testing.T) {
	_, err := parseJSHandlerConfig([]byte(`timeout: notaduration`))
	if err == nil {
		t.Fatal("parseJSHandlerConfig(bad timeout) expected error")
	}
	if !strings.Contains(err.Error(), "invalid jshandler timeout") {
		t.Fatalf("error = %v, want invalid jshandler timeout", err)
	}
}

func TestParseJSHandlerConfigRejectsZeroTimeout(t *testing.T) {
	_, err := parseJSHandlerConfig([]byte(`timeout: 0s`))
	if err == nil {
		t.Fatal("parseJSHandlerConfig(zero timeout) expected error")
	}
	if !strings.Contains(err.Error(), "invalid jshandler timeout") {
		t.Fatalf("error = %v, want invalid jshandler timeout", err)
	}
}

func TestParseJSHandlerConfigRejectsNegativeTimeout(t *testing.T) {
	_, err := parseJSHandlerConfig([]byte(`timeout: -1s`))
	if err == nil {
		t.Fatal("parseJSHandlerConfig(negative timeout) expected error")
	}
}

func TestParseJSHandlerConfigSetsDefaultWhenTimeoutAbsent(t *testing.T) {
	// When timeout field is omitted entirely, Timeout should be 1s default.
	cfg, err := parseJSHandlerConfig([]byte(`enabled: true`))
	if err != nil {
		t.Fatalf("parseJSHandlerConfig() error = %v", err)
	}
	if cfg.Timeout != 1*time.Second {
		t.Fatalf("Timeout = %v, want 1s when timeout key absent", cfg.Timeout)
	}
}

func TestParseJSHandlerConfigParsesDisabled(t *testing.T) {
	cfg, err := parseJSHandlerConfig([]byte(`enabled: false`))
	if err != nil {
		t.Fatalf("parseJSHandlerConfig() error = %v", err)
	}
	if cfg.Enabled {
		t.Fatal("Enabled = true, want false")
	}
}

// resolvedScriptPaths additional cases

func TestResolvedScriptPathsReturnsEmptyWhenNoPaths(t *testing.T) {
	cfg := jsHandlerConfig{}
	paths, err := cfg.resolvedScriptPaths("/some/dir")
	if err != nil {
		t.Fatalf("resolvedScriptPaths(empty) error = %v", err)
	}
	if len(paths) != 0 {
		t.Fatalf("resolvedScriptPaths() = %v, want empty", paths)
	}
}

func TestResolvedScriptPathsSkipsBlankEntries(t *testing.T) {
	cfg := jsHandlerConfig{ScriptPaths: []string{"", "   ", ""}}
	paths, err := cfg.resolvedScriptPaths("/some/dir")
	if err != nil {
		t.Fatalf("resolvedScriptPaths(blank entries) error = %v", err)
	}
	if len(paths) != 0 {
		t.Fatalf("resolvedScriptPaths() = %v, want empty slice for blank entries", paths)
	}
}

func TestResolvedScriptPathsRequiresPluginDirForRelativePaths(t *testing.T) {
	cfg := jsHandlerConfig{ScriptPaths: []string{"some/script.js"}}
	_, err := cfg.resolvedScriptPaths("")
	if err == nil {
		t.Fatal("resolvedScriptPaths() expected error for relative path without pluginDir")
	}
	if !strings.Contains(err.Error(), "requires plugin_dir") {
		t.Fatalf("error = %v, want requires plugin_dir", err)
	}
}

func TestResolvedScriptPathsAcceptsAbsolutePathWithoutPluginDir(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "handler.js")
	if errWrite := os.WriteFile(scriptPath, []byte(""), 0600); errWrite != nil {
		t.Fatalf("os.WriteFile() error = %v", errWrite)
	}
	cfg := jsHandlerConfig{ScriptPaths: []string{scriptPath}}
	paths, err := cfg.resolvedScriptPaths("")
	if err != nil {
		t.Fatalf("resolvedScriptPaths(abs path, no pluginDir) error = %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("resolvedScriptPaths() = %v, want 1 path", paths)
	}
}

func TestResolvedScriptPathsTraversalRejectedForRelativePath(t *testing.T) {
	cfg := jsHandlerConfig{ScriptPaths: []string{"../outside.js"}}
	pluginDir := t.TempDir()
	_, err := cfg.resolvedScriptPaths(pluginDir)
	if err == nil {
		t.Fatal("resolvedScriptPaths() expected error for path escaping plugin_dir")
	}
	if !strings.Contains(err.Error(), "escapes plugin_dir") {
		t.Fatalf("error = %v, want escapes plugin_dir", err)
	}
}

// isPathWithinDir tests

func TestIsPathWithinDirReturnsTrueForDirectChild(t *testing.T) {
	dir := t.TempDir()
	child := filepath.Join(dir, "child.js")
	if !isPathWithinDir(child, dir) {
		t.Fatalf("isPathWithinDir(direct child) = false, want true")
	}
}

func TestIsPathWithinDirReturnsTrueForNestedChild(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "sub", "nested.js")
	if !isPathWithinDir(nested, dir) {
		t.Fatalf("isPathWithinDir(nested) = false, want true")
	}
}

func TestIsPathWithinDirReturnsFalseForParentDir(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Dir(dir)
	if isPathWithinDir(parent, dir) {
		t.Fatalf("isPathWithinDir(parent) = true, want false")
	}
}

func TestIsPathWithinDirReturnsFalseForSiblingDir(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "plugindir")
	sibling := filepath.Join(parent, "other", "file.js")
	if isPathWithinDir(sibling, dir) {
		t.Fatalf("isPathWithinDir(sibling) = true, want false")
	}
}

func TestIsPathWithinDirReturnsTrueForSelf(t *testing.T) {
	dir := t.TempDir()
	if !isPathWithinDir(dir, dir) {
		t.Fatalf("isPathWithinDir(self) = false, want true")
	}
}

func TestIsPathWithinDirReturnsFalseForDotDotTraversal(t *testing.T) {
	dir := t.TempDir()
	traversal := filepath.Join(dir, "..", "escape.js")
	if isPathWithinDir(traversal, dir) {
		t.Fatalf("isPathWithinDir(../escape) = true, want false")
	}
}

// builtinScriptPaths tests

func TestBuiltinScriptPathsReturnsNilWhenPluginDirEmpty(t *testing.T) {
	paths := builtinScriptPaths("")
	if len(paths) != 0 {
		t.Fatalf("builtinScriptPaths('') = %v, want nil", paths)
	}
}

func TestBuiltinScriptPathsReturnsNilWhenScriptsDirMissing(t *testing.T) {
	dir := t.TempDir()
	paths := builtinScriptPaths(dir)
	if len(paths) != 0 {
		t.Fatalf("builtinScriptPaths(no scripts dir) = %v, want nil", paths)
	}
}

func TestBuiltinScriptPathsReturnsJSFilesFromScriptsDir(t *testing.T) {
	pluginDir := t.TempDir()
	scriptsDir := filepath.Join(pluginDir, "scripts")
	if errMkdir := os.Mkdir(scriptsDir, 0700); errMkdir != nil {
		t.Fatalf("os.Mkdir() error = %v", errMkdir)
	}
	if errWrite := os.WriteFile(filepath.Join(scriptsDir, "a.js"), []byte(""), 0600); errWrite != nil {
		t.Fatalf("os.WriteFile() error = %v", errWrite)
	}
	if errWrite := os.WriteFile(filepath.Join(scriptsDir, "b.js"), []byte(""), 0600); errWrite != nil {
		t.Fatalf("os.WriteFile() error = %v", errWrite)
	}
	if errWrite := os.WriteFile(filepath.Join(scriptsDir, "readme.txt"), []byte(""), 0600); errWrite != nil {
		t.Fatalf("os.WriteFile() error = %v", errWrite)
	}

	paths := builtinScriptPaths(pluginDir)
	if len(paths) != 2 {
		t.Fatalf("builtinScriptPaths() = %d paths, want 2", len(paths))
	}
	for _, p := range paths {
		if !strings.HasSuffix(p, ".js") {
			t.Fatalf("builtinScriptPaths() returned non-JS path: %q", p)
		}
	}
}

func TestBuiltinScriptPathsIgnoresSubdirectories(t *testing.T) {
	pluginDir := t.TempDir()
	scriptsDir := filepath.Join(pluginDir, "scripts")
	if errMkdir := os.Mkdir(scriptsDir, 0700); errMkdir != nil {
		t.Fatalf("os.Mkdir() error = %v", errMkdir)
	}
	subDir := filepath.Join(scriptsDir, "subdir")
	if errMkdir := os.Mkdir(subDir, 0700); errMkdir != nil {
		t.Fatalf("os.Mkdir(subDir) error = %v", errMkdir)
	}
	if errWrite := os.WriteFile(filepath.Join(scriptsDir, "real.js"), []byte(""), 0600); errWrite != nil {
		t.Fatalf("os.WriteFile() error = %v", errWrite)
	}

	paths := builtinScriptPaths(pluginDir)
	if len(paths) != 1 {
		t.Fatalf("builtinScriptPaths() = %d paths, want 1 (subdirs must be ignored)", len(paths))
	}
}

func TestBuiltinScriptPathsExcludesSymlinkEscapingScriptsDir(t *testing.T) {
	pluginDir := t.TempDir()
	scriptsDir := filepath.Join(pluginDir, "scripts")
	if errMkdir := os.Mkdir(scriptsDir, 0700); errMkdir != nil {
		t.Fatalf("os.Mkdir() error = %v", errMkdir)
	}
	outsideDir := t.TempDir()
	outsideScript := filepath.Join(outsideDir, "outside.js")
	if errWrite := os.WriteFile(outsideScript, []byte(""), 0600); errWrite != nil {
		t.Fatalf("os.WriteFile() error = %v", errWrite)
	}
	linkPath := filepath.Join(scriptsDir, "linked.js")
	if errSymlink := os.Symlink(outsideScript, linkPath); errSymlink != nil {
		t.Skipf("os.Symlink() not available: %v", errSymlink)
	}
	// Also add a legitimate file so we can verify only the non-escaping one is returned.
	if errWrite := os.WriteFile(filepath.Join(scriptsDir, "safe.js"), []byte(""), 0600); errWrite != nil {
		t.Fatalf("os.WriteFile() error = %v", errWrite)
	}

	paths := builtinScriptPaths(pluginDir)
	for _, p := range paths {
		if strings.Contains(p, "linked.js") || strings.Contains(p, outsideDir) {
			t.Fatalf("builtinScriptPaths() included escaping symlink path: %q", p)
		}
	}
}
