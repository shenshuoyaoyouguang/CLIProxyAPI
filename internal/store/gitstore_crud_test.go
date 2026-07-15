package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestGitTokenStore_Save(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemoteRepository(t, root, "main",
		testBranchSpec{name: "main", contents: "initial\n"},
	)

	store := NewGitTokenStore(remoteDir, "", "", "main")
	workDir := filepath.Join(root, "workspace", "auths")
	store.SetBaseDir(workDir)

	auth := &cliproxyauth.Auth{
		ID:       "test-provider",
		FileName: "test-provider.json",
		Metadata: map[string]any{"access_token": "sk-test", "disabled": false},
	}

	ctx := context.Background()
	path, err := store.Save(ctx, auth)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if path == "" {
		t.Fatal("Save returned empty path")
	}

	// Verify the file exists on disk
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}

	var metadata map[string]any
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatalf("unmarshal saved file: %v", err)
	}
	if got, ok := metadata["access_token"].(string); !ok || got != "sk-test" {
		t.Fatalf("access_token = %v, want sk-test", metadata["access_token"])
	}

	// Verify git has a commit referencing the file
	repoDir := filepath.Join(root, "workspace")
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("get head: %v", err)
	}
	if head.Hash() == plumbing.ZeroHash {
		t.Fatal("expected non-zero commit hash after Save")
	}
}

func TestGitTokenStore_Save_NilAuth(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemoteRepository(t, root, "main",
		testBranchSpec{name: "main", contents: "initial\n"},
	)

	store := NewGitTokenStore(remoteDir, "", "", "main")
	store.SetBaseDir(filepath.Join(root, "workspace", "auths"))

	ctx := context.Background()
	_, err := store.Save(ctx, nil)
	if err == nil {
		t.Fatal("Save(nil) should return error")
	}
}

func TestGitTokenStore_Save_NothingToPersist(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemoteRepository(t, root, "main",
		testBranchSpec{name: "main", contents: "initial\n"},
	)

	store := NewGitTokenStore(remoteDir, "", "", "main")
	store.SetBaseDir(filepath.Join(root, "workspace", "auths"))

	auth := &cliproxyauth.Auth{
		ID: "empty-provider",
		// No Storage and no Metadata
	}

	ctx := context.Background()
	_, err := store.Save(ctx, auth)
	if err == nil {
		t.Fatal("Save with no Storage/Metadata should return error")
	}
}

func TestGitTokenStore_SaveRejectsExternalAbsolutePathBeforeWrite(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemoteRepository(t, root, "main",
		testBranchSpec{name: "main", contents: "initial\n"},
	)

	store := NewGitTokenStore(remoteDir, "", "", "main")
	store.SetBaseDir(filepath.Join(root, "workspace", "auths"))

	outsidePath := filepath.Join(root, "outside.json")
	auth := &cliproxyauth.Auth{
		ID:       "outside",
		FileName: outsidePath,
		Metadata: map[string]any{"access_token": "sk-outside"},
	}

	if _, err := store.Save(context.Background(), auth); err == nil {
		t.Fatal("Save with external absolute path succeeded, want error")
	}
	if _, err := os.Stat(outsidePath); !os.IsNotExist(err) {
		t.Fatalf("external path was written before validation, stat err=%v", err)
	}
}

func TestGitTokenStore_List(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemoteRepository(t, root, "main",
		testBranchSpec{name: "main", contents: "initial\n"},
	)

	store := NewGitTokenStore(remoteDir, "", "", "main")
	workDir := filepath.Join(root, "workspace", "auths")
	store.SetBaseDir(workDir)

	if err := store.EnsureRepository(); err != nil {
		t.Fatalf("EnsureRepository: %v", err)
	}

	// Place auth JSON files manually
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"provider1.json": `{"access_token":"tok1","type":"openai","email":"a@b.com","disabled":false}`,
		"provider2.json": `{"access_token":"tok2","type":"claude","disabled":false}`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(workDir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	ctx := context.Background()
	entries, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("List returned %d entries, want 2", len(entries))
	}

	found := make(map[string]bool)
	for _, e := range entries {
		found[e.ID] = true
	}
	if !found["provider1.json"] {
		t.Error("missing provider1 in List results")
	}
	if !found["provider2.json"] {
		t.Error("missing provider2 in List results")
	}
}

func TestGitTokenStore_Delete(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemoteRepository(t, root, "main",
		testBranchSpec{name: "main", contents: "initial\n"},
	)

	store := NewGitTokenStore(remoteDir, "", "", "main")
	workDir := filepath.Join(root, "workspace", "auths")
	store.SetBaseDir(workDir)

	// Save an entry first
	ctx := context.Background()
	auth := &cliproxyauth.Auth{
		ID:       "delete-me",
		FileName: "delete-me.json",
		Metadata: map[string]any{"access_token": "sk-delete", "disabled": false},
	}
	path, err := store.Save(ctx, auth)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("saved file should exist: %v", err)
	}

	// Delete the entry
	if err := store.Delete(ctx, "delete-me.json"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify the file is removed
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file should be removed after Delete, got err: %v", err)
	}

	// Verify List no longer returns it
	entries, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List after Delete: %v", err)
	}
	for _, e := range entries {
		if e.ID == "delete-me.json" {
			t.Fatal("deleted entry still appears in List")
		}
	}
}

func TestGitTokenStore_DeleteRejectsExternalAbsolutePathBeforeRemove(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemoteRepository(t, root, "main",
		testBranchSpec{name: "main", contents: "initial\n"},
	)

	store := NewGitTokenStore(remoteDir, "", "", "main")
	store.SetBaseDir(filepath.Join(root, "workspace", "auths"))

	outsidePath := filepath.Join(root, "outside.json")
	if err := os.WriteFile(outsidePath, []byte(`{"access_token":"keep"}`), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	if err := store.Delete(context.Background(), outsidePath); err == nil {
		t.Fatal("Delete with external absolute path succeeded, want error")
	}
	if _, err := os.Stat(outsidePath); err != nil {
		t.Fatalf("external path was removed before validation: %v", err)
	}
}

func TestGitTokenStore_Delete_EmptyID(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemoteRepository(t, root, "main",
		testBranchSpec{name: "main", contents: "initial\n"},
	)

	store := NewGitTokenStore(remoteDir, "", "", "main")
	store.SetBaseDir(filepath.Join(root, "workspace", "auths"))

	ctx := context.Background()
	err := store.Delete(ctx, "")
	if err == nil {
		t.Fatal("Delete with empty ID should return error")
	}
}

func TestGitTokenStore_Delete_NonExistent(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemoteRepository(t, root, "main",
		testBranchSpec{name: "main", contents: "initial\n"},
	)

	store := NewGitTokenStore(remoteDir, "", "", "main")
	store.SetBaseDir(filepath.Join(root, "workspace", "auths"))

	if err := store.EnsureRepository(); err != nil {
		t.Fatalf("EnsureRepository: %v", err)
	}

	ctx := context.Background()
	// Deleting a file that does not exist should not error (os.IsNotExist is suppressed)
	err := store.Delete(ctx, "nonexistent.json")
	if err != nil {
		t.Fatalf("Delete non-existent should not error, got: %v", err)
	}
}
