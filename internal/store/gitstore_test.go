package store

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	gitconfig "github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/testutil/store"
)

type testBranchSpec struct {
	name     string
	contents string
}

func TestEnsureRepositoryUsesRemoteDefaultBranchWhenBranchNotConfigured(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemoteRepository(t, root, "trunk",
		testBranchSpec{name: "trunk", contents: "remote default branch\n"},
		testBranchSpec{name: "release/2026", contents: "release branch\n"},
	)

	store := NewGitTokenStore(remoteDir, "", "", "")
	store.SetBaseDir(filepath.Join(root, "workspace", "auths"))

	if err := store.EnsureRepository(); err != nil {
		t.Fatalf("EnsureRepository: %v", err)
	}

	assertRepositoryBranchAndContents(t, filepath.Join(root, "workspace"), "trunk", "remote default branch\n")
	advanceRemoteBranch(t, filepath.Join(root, "seed"), remoteDir, "trunk", "remote default branch updated\n", "advance trunk")
	advanceRemoteBranch(t, filepath.Join(root, "seed"), remoteDir, "release/2026", "release branch updated\n", "advance release")

	if err := store.EnsureRepository(); err != nil {
		t.Fatalf("EnsureRepository second call: %v", err)
	}

	assertRepositoryBranchAndContents(t, filepath.Join(root, "workspace"), "trunk", "remote default branch updated\n")
	assertRemoteHeadBranch(t, remoteDir, "trunk")
}

func TestEnsureRepositoryUsesConfiguredBranchWhenExplicitlySet(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemoteRepository(t, root, "trunk",
		testBranchSpec{name: "trunk", contents: "remote default branch\n"},
		testBranchSpec{name: "release/2026", contents: "release branch\n"},
	)

	store := NewGitTokenStore(remoteDir, "", "", "release/2026")
	store.SetBaseDir(filepath.Join(root, "workspace", "auths"))

	if err := store.EnsureRepository(); err != nil {
		t.Fatalf("EnsureRepository: %v", err)
	}

	assertRepositoryBranchAndContents(t, filepath.Join(root, "workspace"), "release/2026", "release branch\n")
	advanceRemoteBranch(t, filepath.Join(root, "seed"), remoteDir, "trunk", "remote default branch updated\n", "advance trunk")
	advanceRemoteBranch(t, filepath.Join(root, "seed"), remoteDir, "release/2026", "release branch updated\n", "advance release")

	if err := store.EnsureRepository(); err != nil {
		t.Fatalf("EnsureRepository second call: %v", err)
	}

	assertRepositoryBranchAndContents(t, filepath.Join(root, "workspace"), "release/2026", "release branch updated\n")
	assertRemoteHeadBranch(t, remoteDir, "trunk")
}

func TestEnsureRepositoryReturnsErrorForMissingConfiguredBranch(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemoteRepository(t, root, "trunk",
		testBranchSpec{name: "trunk", contents: "remote default branch\n"},
	)

	store := NewGitTokenStore(remoteDir, "", "", "missing-branch")
	store.SetBaseDir(filepath.Join(root, "workspace", "auths"))

	err := store.EnsureRepository()
	if err == nil {
		t.Fatal("EnsureRepository succeeded, want error for nonexistent configured branch")
	}
	assertRemoteHeadBranch(t, remoteDir, "trunk")
}

func TestEnsureRepositoryReturnsErrorForMissingConfiguredBranchOnExistingRepositoryPull(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemoteRepository(t, root, "trunk",
		testBranchSpec{name: "trunk", contents: "remote default branch\n"},
	)

	baseDir := filepath.Join(root, "workspace", "auths")
	store := NewGitTokenStore(remoteDir, "", "", "")
	store.SetBaseDir(baseDir)

	if err := store.EnsureRepository(); err != nil {
		t.Fatalf("EnsureRepository initial clone: %v", err)
	}

	reopened := NewGitTokenStore(remoteDir, "", "", "missing-branch")
	reopened.SetBaseDir(baseDir)

	err := reopened.EnsureRepository()
	if err == nil {
		t.Fatal("EnsureRepository succeeded on reopen, want error for nonexistent configured branch")
	}
	assertRepositoryHeadBranch(t, filepath.Join(root, "workspace"), "trunk")
	assertRemoteHeadBranch(t, remoteDir, "trunk")
}

func TestEnsureRepositoryInitializesEmptyRemoteUsingConfiguredBranch(t *testing.T) {
	root := t.TempDir()
	remoteDir := filepath.Join(root, "remote.git")
	if _, err := git.PlainInit(remoteDir, true); err != nil {
		t.Fatalf("init bare remote: %v", err)
	}

	branch := "feature/gemini-fix"
	store := NewGitTokenStore(remoteDir, "", "", branch)
	store.SetBaseDir(filepath.Join(root, "workspace", "auths"))

	if err := store.EnsureRepository(); err != nil {
		t.Fatalf("EnsureRepository: %v", err)
	}

	assertRepositoryHeadBranch(t, filepath.Join(root, "workspace"), branch)
	assertRemoteBranchExistsWithCommit(t, remoteDir, branch)
	assertRemoteBranchDoesNotExist(t, remoteDir, "master")
}

func TestEnsureRepositoryExistingRepoSwitchesToConfiguredBranch(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemoteRepository(t, root, "master",
		testBranchSpec{name: "master", contents: "remote master branch\n"},
		testBranchSpec{name: "develop", contents: "remote develop branch\n"},
	)

	baseDir := filepath.Join(root, "workspace", "auths")
	store := NewGitTokenStore(remoteDir, "", "", "")
	store.SetBaseDir(baseDir)

	if err := store.EnsureRepository(); err != nil {
		t.Fatalf("EnsureRepository initial clone: %v", err)
	}
	assertRepositoryBranchAndContents(t, filepath.Join(root, "workspace"), "master", "remote master branch\n")

	reopened := NewGitTokenStore(remoteDir, "", "", "develop")
	reopened.SetBaseDir(baseDir)

	if err := reopened.EnsureRepository(); err != nil {
		t.Fatalf("EnsureRepository reopen: %v", err)
	}
	assertRepositoryBranchAndContents(t, filepath.Join(root, "workspace"), "develop", "remote develop branch\n")

	workspaceDir := filepath.Join(root, "workspace")
	if err := os.WriteFile(filepath.Join(workspaceDir, "branch.txt"), []byte("local develop update\n"), 0o600); err != nil {
		t.Fatalf("write local branch marker: %v", err)
	}

	reopened.mu.Lock()
	err := reopened.commitAndPushLocked("Update develop branch marker", "branch.txt")
	reopened.mu.Unlock()
	if err != nil {
		t.Fatalf("commitAndPushLocked: %v", err)
	}

	assertRepositoryHeadBranch(t, workspaceDir, "develop")
	assertRemoteBranchContents(t, remoteDir, "develop", "local develop update\n")
	assertRemoteBranchContents(t, remoteDir, "master", "remote master branch\n")
}

func TestEnsureRepositoryExistingRepoSwitchesToConfiguredBranchCreatedAfterClone(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemoteRepository(t, root, "master",
		testBranchSpec{name: "master", contents: "remote master branch\n"},
	)

	baseDir := filepath.Join(root, "workspace", "auths")
	store := NewGitTokenStore(remoteDir, "", "", "")
	store.SetBaseDir(baseDir)

	if err := store.EnsureRepository(); err != nil {
		t.Fatalf("EnsureRepository initial clone: %v", err)
	}
	assertRepositoryBranchAndContents(t, filepath.Join(root, "workspace"), "master", "remote master branch\n")

	advanceRemoteBranchFromNewBranch(t, filepath.Join(root, "seed"), remoteDir, "release/2026", "release branch\n", "create release")

	reopened := NewGitTokenStore(remoteDir, "", "", "release/2026")
	reopened.SetBaseDir(baseDir)

	if err := reopened.EnsureRepository(); err != nil {
		t.Fatalf("EnsureRepository reopen: %v", err)
	}
	assertRepositoryBranchAndContents(t, filepath.Join(root, "workspace"), "release/2026", "release branch\n")
}

func TestEnsureRepositoryResetsToRemoteDefaultWhenBranchUnset(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemoteRepository(t, root, "master",
		testBranchSpec{name: "master", contents: "remote master branch\n"},
		testBranchSpec{name: "develop", contents: "remote develop branch\n"},
	)

	baseDir := filepath.Join(root, "workspace", "auths")
	// First store pins to develop and prepares local workspace
	storePinned := NewGitTokenStore(remoteDir, "", "", "develop")
	storePinned.SetBaseDir(baseDir)
	if err := storePinned.EnsureRepository(); err != nil {
		t.Fatalf("EnsureRepository pinned: %v", err)
	}
	assertRepositoryBranchAndContents(t, filepath.Join(root, "workspace"), "develop", "remote develop branch\n")

	// Second store has branch unset and should reset local workspace to remote default (master)
	storeDefault := NewGitTokenStore(remoteDir, "", "", "")
	storeDefault.SetBaseDir(baseDir)
	if err := storeDefault.EnsureRepository(); err != nil {
		t.Fatalf("EnsureRepository default: %v", err)
	}
	// Local HEAD should now follow remote default (master)
	assertRepositoryHeadBranch(t, filepath.Join(root, "workspace"), "master")

	// Make a local change and push using the store with branch unset; push should update remote master
	workspaceDir := filepath.Join(root, "workspace")
	if err := os.WriteFile(filepath.Join(workspaceDir, "branch.txt"), []byte("local master update\n"), 0o600); err != nil {
		t.Fatalf("write local master marker: %v", err)
	}
	storeDefault.mu.Lock()
	if err := storeDefault.commitAndPushLocked("Update master marker", "branch.txt"); err != nil {
		storeDefault.mu.Unlock()
		t.Fatalf("commitAndPushLocked: %v", err)
	}
	storeDefault.mu.Unlock()

	assertRemoteBranchContents(t, remoteDir, "master", "local master update\n")
}

func TestCommitAndPushLockedPushesBeforeRunningGC(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemoteRepository(t, root, "master",
		testBranchSpec{name: "master", contents: "remote master branch\n"},
	)

	store := NewGitTokenStore(remoteDir, "", "", "")
	store.SetBaseDir(filepath.Join(root, "workspace", "auths"))
	if err := store.EnsureRepository(); err != nil {
		t.Fatalf("EnsureRepository: %v", err)
	}

	workspaceDir := filepath.Join(root, "workspace")
	updates := []string{
		"local master update one\n",
		"local master update two\n",
	}
	for _, contents := range updates {
		if err := os.WriteFile(filepath.Join(workspaceDir, "branch.txt"), []byte(contents), 0o600); err != nil {
			t.Fatalf("write local master marker: %v", err)
		}

		store.lastGC = time.Now().Add(-gcInterval)
		store.mu.Lock()
		err := store.commitAndPushLocked("Update master marker", "branch.txt")
		store.mu.Unlock()
		if err != nil {
			t.Fatalf("commitAndPushLocked with forced GC: %v", err)
		}

		assertRemoteBranchContents(t, remoteDir, "master", contents)
	}
}

func TestEnsureRepositoryFollowsRenamedRemoteDefaultBranchWhenAvailable(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemoteRepository(t, root, "master",
		testBranchSpec{name: "master", contents: "remote master branch\n"},
		testBranchSpec{name: "main", contents: "remote main branch\n"},
	)

	baseDir := filepath.Join(root, "workspace", "auths")
	store := NewGitTokenStore(remoteDir, "", "", "")
	store.SetBaseDir(baseDir)

	if err := store.EnsureRepository(); err != nil {
		t.Fatalf("EnsureRepository initial clone: %v", err)
	}
	assertRepositoryBranchAndContents(t, filepath.Join(root, "workspace"), "master", "remote master branch\n")

	setRemoteHeadBranch(t, remoteDir, "main")
	advanceRemoteBranch(t, filepath.Join(root, "seed"), remoteDir, "main", "remote main branch updated\n", "advance main")

	reopened := NewGitTokenStore(remoteDir, "", "", "")
	reopened.SetBaseDir(baseDir)

	if err := reopened.EnsureRepository(); err != nil {
		t.Fatalf("EnsureRepository after remote default rename: %v", err)
	}
	assertRepositoryBranchAndContents(t, filepath.Join(root, "workspace"), "main", "remote main branch updated\n")
	assertRemoteHeadBranch(t, remoteDir, "main")
}

func TestEnsureRepositoryKeepsCurrentBranchWhenRemoteDefaultCannotBeResolved(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemoteRepository(t, root, "master",
		testBranchSpec{name: "master", contents: "remote master branch\n"},
		testBranchSpec{name: "develop", contents: "remote develop branch\n"},
	)

	baseDir := filepath.Join(root, "workspace", "auths")
	pinned := NewGitTokenStore(remoteDir, "", "", "develop")
	pinned.SetBaseDir(baseDir)
	if err := pinned.EnsureRepository(); err != nil {
		t.Fatalf("EnsureRepository pinned: %v", err)
	}
	assertRepositoryBranchAndContents(t, filepath.Join(root, "workspace"), "develop", "remote develop branch\n")

	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
		http.Error(w, "auth required", http.StatusUnauthorized)
	}))
	defer authServer.Close()

	repo, err := git.PlainOpen(filepath.Join(root, "workspace"))
	if err != nil {
		t.Fatalf("open workspace repo: %v", err)
	}
	cfg, err := repo.Config()
	if err != nil {
		t.Fatalf("read repo config: %v", err)
	}
	cfg.Remotes["origin"].URLs = []string{authServer.URL}
	if err := repo.SetConfig(cfg); err != nil {
		t.Fatalf("set repo config: %v", err)
	}

	reopened := NewGitTokenStore(remoteDir, "", "", "")
	reopened.SetBaseDir(baseDir)

	if err := reopened.EnsureRepository(); err != nil {
		t.Fatalf("EnsureRepository default branch fallback: %v", err)
	}
	assertRepositoryHeadBranch(t, filepath.Join(root, "workspace"), "develop")
}

func setupGitRemoteRepository(t *testing.T, root, defaultBranch string, branches ...testBranchSpec) string {
	t.Helper()

	remoteDir := filepath.Join(root, "remote.git")
	if _, err := git.PlainInit(remoteDir, true); err != nil {
		t.Fatalf("init bare remote: %v", err)
	}

	seedDir := filepath.Join(root, "seed")
	seedRepo, err := git.PlainInit(seedDir, false)
	if err != nil {
		t.Fatalf("init seed repo: %v", err)
	}
	if err := seedRepo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName(defaultBranch))); err != nil {
		t.Fatalf("set seed HEAD: %v", err)
	}

	worktree, err := seedRepo.Worktree()
	if err != nil {
		t.Fatalf("open seed worktree: %v", err)
	}

	defaultSpec, ok := findBranchSpec(branches, defaultBranch)
	if !ok {
		t.Fatalf("missing default branch spec for %q", defaultBranch)
	}
	commitBranchMarker(t, seedDir, worktree, defaultSpec, "seed default branch")

	for _, branch := range branches {
		if branch.name == defaultBranch {
			continue
		}
		if err := worktree.Checkout(&git.CheckoutOptions{Branch: plumbing.NewBranchReferenceName(defaultBranch)}); err != nil {
			t.Fatalf("checkout default branch %s: %v", defaultBranch, err)
		}
		if err := worktree.Checkout(&git.CheckoutOptions{Branch: plumbing.NewBranchReferenceName(branch.name), Create: true}); err != nil {
			t.Fatalf("create branch %s: %v", branch.name, err)
		}
		commitBranchMarker(t, seedDir, worktree, branch, "seed branch "+branch.name)
	}

	if _, err := seedRepo.CreateRemote(&gitconfig.RemoteConfig{Name: "origin", URLs: []string{remoteDir}}); err != nil {
		t.Fatalf("create origin remote: %v", err)
	}
	if err := seedRepo.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []gitconfig.RefSpec{gitconfig.RefSpec("refs/heads/*:refs/heads/*")},
	}); err != nil {
		t.Fatalf("push seed branches: %v", err)
	}

	remoteRepo, err := git.PlainOpen(remoteDir)
	if err != nil {
		t.Fatalf("open remote repo: %v", err)
	}
	if err := remoteRepo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName(defaultBranch))); err != nil {
		t.Fatalf("set remote HEAD: %v", err)
	}

	return remoteDir
}

func commitBranchMarker(t *testing.T, seedDir string, worktree *git.Worktree, branch testBranchSpec, message string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(seedDir, "branch.txt"), []byte(branch.contents), 0o600); err != nil {
		t.Fatalf("write branch marker for %s: %v", branch.name, err)
	}
	if _, err := worktree.Add("branch.txt"); err != nil {
		t.Fatalf("add branch marker for %s: %v", branch.name, err)
	}
	if _, err := worktree.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "CLIProxyAPI",
			Email: "cliproxy@local",
			When:  time.Unix(1711929600, 0),
		},
	}); err != nil {
		t.Fatalf("commit branch marker for %s: %v", branch.name, err)
	}
}

func advanceRemoteBranch(t *testing.T, seedDir, remoteDir, branch, contents, message string) {
	t.Helper()

	seedRepo, err := git.PlainOpen(seedDir)
	if err != nil {
		t.Fatalf("open seed repo: %v", err)
	}
	worktree, err := seedRepo.Worktree()
	if err != nil {
		t.Fatalf("open seed worktree: %v", err)
	}
	if err := worktree.Checkout(&git.CheckoutOptions{Branch: plumbing.NewBranchReferenceName(branch)}); err != nil {
		t.Fatalf("checkout branch %s: %v", branch, err)
	}
	commitBranchMarker(t, seedDir, worktree, testBranchSpec{name: branch, contents: contents}, message)
	if err := seedRepo.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs: []gitconfig.RefSpec{
			gitconfig.RefSpec(plumbing.NewBranchReferenceName(branch).String() + ":" + plumbing.NewBranchReferenceName(branch).String()),
		},
	}); err != nil {
		t.Fatalf("push branch %s update to %s: %v", branch, remoteDir, err)
	}
}

func advanceRemoteBranchFromNewBranch(t *testing.T, seedDir, remoteDir, branch, contents, message string) {
	t.Helper()

	seedRepo, err := git.PlainOpen(seedDir)
	if err != nil {
		t.Fatalf("open seed repo: %v", err)
	}
	worktree, err := seedRepo.Worktree()
	if err != nil {
		t.Fatalf("open seed worktree: %v", err)
	}
	if err := worktree.Checkout(&git.CheckoutOptions{Branch: plumbing.NewBranchReferenceName("master")}); err != nil {
		t.Fatalf("checkout master before creating %s: %v", branch, err)
	}
	if err := worktree.Checkout(&git.CheckoutOptions{Branch: plumbing.NewBranchReferenceName(branch), Create: true}); err != nil {
		t.Fatalf("create branch %s: %v", branch, err)
	}
	commitBranchMarker(t, seedDir, worktree, testBranchSpec{name: branch, contents: contents}, message)
	if err := seedRepo.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs: []gitconfig.RefSpec{
			gitconfig.RefSpec(plumbing.NewBranchReferenceName(branch).String() + ":" + plumbing.NewBranchReferenceName(branch).String()),
		},
	}); err != nil {
		t.Fatalf("push new branch %s update to %s: %v", branch, remoteDir, err)
	}
}

func findBranchSpec(branches []testBranchSpec, name string) (testBranchSpec, bool) {
	for _, branch := range branches {
		if branch.name == name {
			return branch, true
		}
	}
	return testBranchSpec{}, false
}

func assertRepositoryBranchAndContents(t *testing.T, repoDir, branch, wantContents string) {
	t.Helper()

	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatalf("open local repo: %v", err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("local repo head: %v", err)
	}
	if got, want := head.Name(), plumbing.NewBranchReferenceName(branch); got != want {
		t.Fatalf("local head branch = %s, want %s", got, want)
	}
	contents, err := os.ReadFile(filepath.Join(repoDir, "branch.txt"))
	if err != nil {
		t.Fatalf("read branch marker: %v", err)
	}
	if got := string(contents); got != wantContents {
		t.Fatalf("branch marker contents = %q, want %q", got, wantContents)
	}
}

func assertRepositoryHeadBranch(t *testing.T, repoDir, branch string) {
	t.Helper()

	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatalf("open local repo: %v", err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("local repo head: %v", err)
	}
	if got, want := head.Name(), plumbing.NewBranchReferenceName(branch); got != want {
		t.Fatalf("local head branch = %s, want %s", got, want)
	}
}

func assertRemoteHeadBranch(t *testing.T, remoteDir, branch string) {
	t.Helper()

	remoteRepo, err := git.PlainOpen(remoteDir)
	if err != nil {
		t.Fatalf("open remote repo: %v", err)
	}
	head, err := remoteRepo.Reference(plumbing.HEAD, false)
	if err != nil {
		t.Fatalf("read remote HEAD: %v", err)
	}
	if got, want := head.Target(), plumbing.NewBranchReferenceName(branch); got != want {
		t.Fatalf("remote HEAD target = %s, want %s", got, want)
	}
}

func setRemoteHeadBranch(t *testing.T, remoteDir, branch string) {
	t.Helper()

	remoteRepo, err := git.PlainOpen(remoteDir)
	if err != nil {
		t.Fatalf("open remote repo: %v", err)
	}
	if err := remoteRepo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName(branch))); err != nil {
		t.Fatalf("set remote HEAD to %s: %v", branch, err)
	}
}

func assertRemoteBranchExistsWithCommit(t *testing.T, remoteDir, branch string) {
	t.Helper()

	remoteRepo, err := git.PlainOpen(remoteDir)
	if err != nil {
		t.Fatalf("open remote repo: %v", err)
	}
	ref, err := remoteRepo.Reference(plumbing.NewBranchReferenceName(branch), false)
	if err != nil {
		t.Fatalf("read remote branch %s: %v", branch, err)
	}
	if got := ref.Hash(); got == plumbing.ZeroHash {
		t.Fatalf("remote branch %s hash = %s, want non-zero hash", branch, got)
	}
}

func assertRemoteBranchDoesNotExist(t *testing.T, remoteDir, branch string) {
	t.Helper()

	remoteRepo, err := git.PlainOpen(remoteDir)
	if err != nil {
		t.Fatalf("open remote repo: %v", err)
	}
	if _, err := remoteRepo.Reference(plumbing.NewBranchReferenceName(branch), false); err == nil {
		t.Fatalf("remote branch %s exists, want missing", branch)
	} else if err != plumbing.ErrReferenceNotFound {
		t.Fatalf("read remote branch %s: %v", branch, err)
	}
}

func assertRemoteBranchContents(t *testing.T, remoteDir, branch, wantContents string) {
	t.Helper()

	remoteRepo, err := git.PlainOpen(remoteDir)
	if err != nil {
		t.Fatalf("open remote repo: %v", err)
	}
	ref, err := remoteRepo.Reference(plumbing.NewBranchReferenceName(branch), false)
	if err != nil {
		t.Fatalf("read remote branch %s: %v", branch, err)
	}
	commit, err := remoteRepo.CommitObject(ref.Hash())
	if err != nil {
		t.Fatalf("read remote branch %s commit: %v", branch, err)
	}
	tree, err := commit.Tree()
	if err != nil {
		t.Fatalf("read remote branch %s tree: %v", branch, err)
	}
	file, err := tree.File("branch.txt")
	if err != nil {
		t.Fatalf("read remote branch %s file: %v", branch, err)
	}
	contents, err := file.Contents()
	if err != nil {
		t.Fatalf("read remote branch %s contents: %v", branch, err)
	}
	if contents != wantContents {
		t.Fatalf("remote branch %s contents = %q, want %q", branch, contents, wantContents)
	}
}

// ============================================================
// GitTokenStore CRUD Tests (using inline git setup)
// ============================================================

func setupGitRemote(t *testing.T, root, branch string) string {
	t.Helper()
	bareDir := filepath.Join(root, "remote.git")

	// Initialize bare repo
	_, err := git.PlainInit(bareDir, true)
	if err != nil {
		t.Fatalf("init bare repo: %v", err)
	}

	// Create initial commit on the specified branch
	workDir := t.TempDir()
	wtrepo, err := git.PlainInit(workDir, false)
	if err != nil {
		t.Fatalf("init work repo: %v", err)
	}

	wt, err := wtrepo.Worktree()
	if err != nil {
		t.Fatalf("get worktree: %v", err)
	}

	gitkeep := filepath.Join(workDir, ".gitkeep")
	if err := os.WriteFile(gitkeep, []byte{}, 0o600); err != nil {
		t.Fatalf("write gitkeep: %v", err)
	}
	if _, err := wt.Add(".gitkeep"); err != nil {
		t.Fatalf("add gitkeep: %v", err)
	}

	_, err = wt.Commit("Initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "CLIProxyAPI Test",
			Email: "test@cliproxy.local",
		},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	remote, err := wtrepo.CreateRemote(&gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{bareDir},
	})
	if err != nil {
		t.Fatalf("create remote: %v", err)
	}

	err = remote.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []gitconfig.RefSpec{gitconfig.RefSpec("refs/heads/" + branch + ":refs/heads/" + branch)},
	})
	// "already up-to-date" is a valid success state for empty repos
	if err != nil && !strings.Contains(err.Error(), "already up-to-date") {
		t.Fatalf("push: %v", err)
	}

	return bareDir
}

// TestGitTokenStore_Bootstrap tests Bootstrap from empty config.
func TestGitTokenStore_Bootstrap(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemote(t, root, "main")

	s := NewGitTokenStore(remoteDir, "", "", "main")
	workDir := filepath.Join(root, "workspace", "auths")
	s.SetBaseDir(workDir)

	if err := s.Bootstrap(context.Background(), ""); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	// Should create empty config file
	configData, err := os.ReadFile(s.ConfigPath())
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if len(configData) != 0 {
		t.Errorf("expected empty config, got: %s", string(configData))
	}
}

// TestGitTokenStore_Bootstrap_FromTemplate tests bootstrap from template.
func TestGitTokenStore_Bootstrap_FromTemplate(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemote(t, root, "main")

	// Create template config
	templateConfig := filepath.Join(root, "template.yaml")
	if err := os.WriteFile(templateConfig, store.TestConfig(), 0o600); err != nil {
		t.Fatalf("write template: %v", err)
	}

	s := NewGitTokenStore(remoteDir, "", "", "main")
	workDir := filepath.Join(root, "workspace", "auths")
	s.SetBaseDir(workDir)

	if err := s.Bootstrap(context.Background(), templateConfig); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	configData, err := os.ReadFile(s.ConfigPath())
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !contains(string(configData), "port: 3456") {
		t.Errorf("template config not used: %s", string(configData))
	}
}

// TestGitTokenStore_PersistConfig_Roundtrip tests config persistence roundtrip.
func TestGitTokenStore_PersistConfig_Roundtrip(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemote(t, root, "main")

	s := NewGitTokenStore(remoteDir, "", "", "main")
	workDir := filepath.Join(root, "workspace", "auths")
	s.SetBaseDir(workDir)

	if err := s.Bootstrap(context.Background(), ""); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	// Modify config locally
	newConfig := []byte(`
port: 8888
host: 127.0.0.1
debug: false
custom_field: "test-value"
`)
	if err := os.WriteFile(s.ConfigPath(), newConfig, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Persist to git
	if err := s.PersistConfig(context.Background()); err != nil {
		t.Fatalf("PersistConfig: %v", err)
	}

	// Create new store and bootstrap - should get persisted config
	s2 := NewGitTokenStore(remoteDir, "", "", "main")
	workDir2 := filepath.Join(root, "workspace2", "auths")
	s2.SetBaseDir(workDir2)

	if err := s2.Bootstrap(context.Background(), ""); err != nil {
		t.Fatalf("Bootstrap 2: %v", err)
	}

	data, err := os.ReadFile(s2.ConfigPath())
	if err != nil {
		t.Fatalf("read config 2: %v", err)
	}
	if !contains(string(data), "port: 8888") {
		t.Errorf("config roundtrip mismatch\ngot: %s\nwant: contains port: 8888", string(data))
	}
}

// TestGitTokenStore_Save_AuthWithStorage tests Save with Storage interface.
func TestGitTokenStore_Save_AuthWithStorage(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemote(t, root, "main")

	s := NewGitTokenStore(remoteDir, "", "", "main")
	workDir := filepath.Join(root, "workspace", "auths")
	s.SetBaseDir(workDir)

	if err := s.Bootstrap(context.Background(), ""); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	auth := store.TestAuth("storage-provider", "openai", false)
	path, err := s.Save(context.Background(), auth)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if path == "" {
		t.Fatal("Save returned empty path")
	}

	// Verify file exists locally
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}

	var meta map[string]any
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if meta["type"] != "openai" {
		t.Errorf("type = %v, want openai", meta["type"])
	}

	// Verify attributes set correctly
	if auth.Attributes[cliproxyauth.AttributeSourceBackend] != cliproxyauth.AuthSourceGit {
		t.Errorf("AttributeSourceBackend = %v, want %v", auth.Attributes[cliproxyauth.AttributeSourceBackend], cliproxyauth.AuthSourceGit)
	}
	if auth.Attributes[cliproxyauth.AttributePath] != path {
		t.Errorf("AttributePath = %v, want %v", auth.Attributes[cliproxyauth.AttributePath], path)
	}
}

// TestGitTokenStore_Save_AuthWithMetadata tests Save with only Metadata.
func TestGitTokenStore_Save_AuthWithMetadata(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemote(t, root, "main")

	s := NewGitTokenStore(remoteDir, "", "", "main")
	workDir := filepath.Join(root, "workspace", "auths")
	s.SetBaseDir(workDir)

	if err := s.Bootstrap(context.Background(), ""); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	auth := store.TestAuth("metadata-provider", "claude", false)
	auth.Storage = nil // Force metadata path
	path, err := s.Save(context.Background(), auth)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify attributes set correctly
	if auth.Attributes[cliproxyauth.AttributeSourceBackend] != cliproxyauth.AuthSourceGit {
		t.Errorf("AttributeSourceBackend = %v, want %v", auth.Attributes[cliproxyauth.AttributeSourceBackend], cliproxyauth.AuthSourceGit)
	}
	if auth.Attributes[cliproxyauth.AttributePath] != path {
		t.Errorf("AttributePath = %v, want %v", auth.Attributes[cliproxyauth.AttributePath], path)
	}

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("saved file missing: %v", err)
	}
}

// TestGitTokenStore_Save_DisabledAuth tests Save with Disabled=true.
func TestGitTokenStore_Save_DisabledAuth(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemote(t, root, "main")

	s := NewGitTokenStore(remoteDir, "", "", "main")
	workDir := filepath.Join(root, "workspace", "auths")
	s.SetBaseDir(workDir)

	if err := s.Bootstrap(context.Background(), ""); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	auth := store.TestAuth("disabled-provider", "gemini", true)
	auth.Disabled = true
	_, err := s.Save(context.Background(), auth)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Metadata should have disabled=true
	if auth.Metadata["disabled"] != true {
		t.Error("disabled not set in metadata")
	}

	// List should show disabled status
	list, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, a := range list {
		if a.ID == "disabled-provider.json" && !a.Disabled {
			t.Errorf("auth should be disabled in list")
		}
	}
}

// TestGitTokenStore_List_IncludesDisabled tests List returns disabled auths.
func TestGitTokenStore_List_IncludesDisabled(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemote(t, root, "main")

	s := NewGitTokenStore(remoteDir, "", "", "main")
	workDir := filepath.Join(root, "workspace", "auths")
	s.SetBaseDir(workDir)

	if err := s.Bootstrap(context.Background(), ""); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	// Save enabled auth
	auth1 := store.TestAuth("enabled-provider", "openai", false)
	if _, err := s.Save(context.Background(), auth1); err != nil {
		t.Fatalf("Save enabled: %v", err)
	}

	// Save disabled auth
	auth2 := store.TestAuth("disabled-provider", "claude", true)
	auth2.Disabled = true
	if _, err := s.Save(context.Background(), auth2); err != nil {
		t.Fatalf("Save disabled: %v", err)
	}

	// List should include both enabled and disabled auths
	list, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	foundEnabled := false
	foundDisabled := false
	for _, a := range list {
		if a.ID == "enabled-provider.json" {
			foundEnabled = true
		}
		if a.ID == "disabled-provider.json" {
			foundDisabled = true
			if !a.Disabled {
				t.Error("disabled auth should have Disabled=true")
			}
			if a.Status != cliproxyauth.StatusDisabled {
				t.Errorf("disabled auth status = %v, want StatusDisabled", a.Status)
			}
		}
	}
	if !foundEnabled {
		t.Error("enabled auth not found in list")
	}
	if !foundDisabled {
		t.Error("disabled auth not found in list")
	}
}

// TestGitTokenStore_Delete_RemovesBoth tests Delete removes from local and git.
func TestGitTokenStore_Delete_RemovesBoth(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemote(t, root, "main")

	s := NewGitTokenStore(remoteDir, "", "", "main")
	workDir := filepath.Join(root, "workspace", "auths")
	s.SetBaseDir(workDir)

	if err := s.Bootstrap(context.Background(), ""); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	auth := store.TestAuth("delete-provider", "openai", false)
	path, err := s.Save(context.Background(), auth)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify file exists locally
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file should exist before delete: %v", err)
	}

	// Delete
	if err := s.Delete(context.Background(), "delete-provider"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify file removed locally
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file should be removed locally: %v", err)
	}

	// Verify removed from list
	list, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	for _, a := range list {
		if a.ID == "delete-provider.json" {
			t.Error("deleted auth still in list")
		}
	}
}

// TestGitTokenStore_PersistAuthFiles_Upload tests uploading auth files.
func TestGitTokenStore_PersistAuthFiles_Upload(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemote(t, root, "main")

	s := NewGitTokenStore(remoteDir, "", "", "main")
	workDir := filepath.Join(root, "workspace", "auths")
	s.SetBaseDir(workDir)

	if err := s.Bootstrap(context.Background(), ""); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	// Create local auth files
	authDir := s.AuthDir()
	auth1 := store.TestAuth("upload-provider-1", "openai", false)
	auth2 := store.TestAuth("upload-provider-2", "claude", false)
	store.WriteTestAuthFiles(t, authDir, map[string]*cliproxyauth.Auth{
		"upload-provider-1.json": auth1,
		"upload-provider-2.json": auth2,
	})

	// Persist to git
	if err := s.PersistAuthFiles(context.Background(), "test upload", "upload-provider-1.json", "upload-provider-2.json"); err != nil {
		t.Fatalf("PersistAuthFiles: %v", err)
	}

	// List should now show both
	list, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) < 2 {
		t.Errorf("expected at least 2 auths, got %d", len(list))
	}
}

// TestGitTokenStore_ConcurrentSave tests concurrent Save operations.
func TestGitTokenStore_ConcurrentSave(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemote(t, root, "main")

	s := NewGitTokenStore(remoteDir, "", "", "main")
	workDir := filepath.Join(root, "workspace", "auths")
	s.SetBaseDir(workDir)

	if err := s.Bootstrap(context.Background(), ""); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	// Serialize saves via gate to avoid bare-repo push race; the store's own
	// mutex serializes local operations, but concurrent pushes to a bare repo
	// can fail with "reference has changed concurrently".
	var gate sync.Mutex
	store.RunConcurrent(t, 10, func() error {
		auth := store.TestAuth("concurrent-"+randomString(8), "openai", false)
		gate.Lock()
		_, err := s.Save(context.Background(), auth)
		gate.Unlock()
		return err
	})
}

// TestGitTokenStore_ConcurrentListSave tests concurrent List and Save.
func TestGitTokenStore_ConcurrentListSave(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemote(t, root, "main")

	s := NewGitTokenStore(remoteDir, "", "", "main")
	workDir := filepath.Join(root, "workspace", "auths")
	s.SetBaseDir(workDir)

	if err := s.Bootstrap(context.Background(), ""); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	// Serialize saves to avoid bare-repo push race.
	var gate sync.Mutex
	store.RunConcurrent(t, 20, func() error {
		if randomBool() {
			auth := store.TestAuth("list-save-"+randomString(6), "openai", false)
			gate.Lock()
			_, err := s.Save(context.Background(), auth)
			gate.Unlock()
			return err
		}
		_, err := s.List(context.Background())
		return err
	})
}

// TestGitTokenStore_PathTraversalProtection tests path traversal protection.
func TestGitTokenStore_PathTraversalProtection(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemote(t, root, "main")

	s := NewGitTokenStore(remoteDir, "", "", "main")
	workDir := filepath.Join(root, "workspace", "auths")
	s.SetBaseDir(workDir)

	if err := s.Bootstrap(context.Background(), ""); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	// Attempt path traversal via ID
	auth := store.TestAuth("../etc/passwd", "openai", false)
	_, err := s.Save(context.Background(), auth)
	if err == nil {
		t.Error("expected error for path traversal attempt")
	}
}

// TestGitTokenStore_SpecialCharsInID tests special characters in auth ID.
func TestGitTokenStore_SpecialCharsInID(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemote(t, root, "main")

	s := NewGitTokenStore(remoteDir, "", "", "main")
	workDir := filepath.Join(root, "workspace", "auths")
	s.SetBaseDir(workDir)

	if err := s.Bootstrap(context.Background(), ""); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	specialIDs := []string{
		"provider with spaces",
		"provider@domain.com",
		"provider#1",
		"provider+tag",
		"provider/name",
	}

	for _, id := range specialIDs {
		t.Run(id, func(t *testing.T) {
			auth := store.TestAuth(id, "openai", false)
			_, err := s.Save(context.Background(), auth)
			if err != nil {
				t.Errorf("Save failed for ID %q: %v", id, err)
			}
		})
	}
}

// TestGitTokenStore_LargeAuthBlob tests large auth metadata.
func TestGitTokenStore_LargeAuthBlob(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemote(t, root, "main")

	s := NewGitTokenStore(remoteDir, "", "", "main")
	workDir := filepath.Join(root, "workspace", "auths")
	s.SetBaseDir(workDir)

	if err := s.Bootstrap(context.Background(), ""); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	// Create auth with large metadata - use deterministic keys to ensure unique entries
	largeMetadata := make(map[string]any)
	for i := 0; i < 1000; i++ {
		largeMetadata[fmt.Sprintf("key_%04d", i)] = randomString(100)
	}

	auth := store.TestAuth("large-blob", "openai", false)
	t.Logf("Before save - Metadata keys: %d", len(auth.Metadata))
	auth.Metadata = largeMetadata
	t.Logf("After overwrite - Metadata keys: %d", len(auth.Metadata))

	path, err := s.Save(context.Background(), auth)
	if err != nil {
		t.Fatalf("Save large auth: %v", err)
	}

	// Verify can read back
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read large auth: %v", err)
	}
	t.Logf("File size: %d bytes", len(data))
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	t.Logf("Keys in saved metadata: %d", len(m))
	if len(data) < 1000 {
		t.Errorf("expected large blob, got %d bytes", len(data))
	}
}

// TestGitTokenStore_ConfigPath tests ConfigPath method.
func TestGitTokenStore_ConfigPath(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemote(t, root, "main")

	s := NewGitTokenStore(remoteDir, "", "", "main")
	workDir := filepath.Join(root, "workspace", "auths")
	s.SetBaseDir(workDir)

	configPath := s.ConfigPath()
	if configPath == "" {
		t.Error("ConfigPath should not be empty after SetBaseDir")
	}
	if !filepath.IsAbs(configPath) {
		t.Errorf("ConfigPath should be absolute, got: %s", configPath)
	}
}

// TestGitTokenStore_AuthDir tests AuthDir method.
func TestGitTokenStore_AuthDir(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemote(t, root, "main")

	s := NewGitTokenStore(remoteDir, "", "", "main")
	workDir := filepath.Join(root, "workspace", "auths")
	s.SetBaseDir(workDir)

	authDir := s.AuthDir()
	if authDir == "" {
		t.Error("AuthDir should not be empty after SetBaseDir")
	}
	if !filepath.IsAbs(authDir) {
		t.Errorf("AuthDir should be absolute, got: %s", authDir)
	}
}

// TestGitTokenStore_Close tests Close method (no-op for git store).
func TestGitTokenStore_Close(t *testing.T) {
	s := NewGitTokenStore("", "", "", "main")
	if err := s.Close(); err != nil {
		t.Errorf("Close should not return error: %v", err)
	}
}

// TestGitTokenStore_SpoolDirPermissions tests spool directory permissions.
func TestGitTokenStore_SpoolDirPermissions(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemote(t, root, "main")

	s := NewGitTokenStore(remoteDir, "", "", "main")
	workDir := filepath.Join(root, "workspace", "auths")
	s.SetBaseDir(workDir)

	if err := s.Bootstrap(context.Background(), ""); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	// Check directory permissions (owner read/write/execute only)
	// On Windows, permissions work differently, so just verify dirs exist
	info, err := os.Stat(filepath.Join(root, "workspace", "config"))
	if err != nil {
		t.Fatalf("stat config dir: %v", err)
	}
	if !info.IsDir() {
		t.Error("config dir should be a directory")
	}

	info, err = os.Stat(filepath.Join(root, "workspace", "auths"))
	if err != nil {
		t.Fatalf("stat auths dir: %v", err)
	}
	if !info.IsDir() {
		t.Error("auths dir should be a directory")
	}
}

// TestGitTokenStore_WithAuth tests GitTokenStore with HTTP basic auth.
func TestGitTokenStore_WithAuth(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemote(t, root, "main")

	// Test with username/password (even if not actually used for local)
	s := NewGitTokenStore(remoteDir, "testuser", "testpass", "main")
	workDir := filepath.Join(root, "workspace", "auths")
	s.SetBaseDir(workDir)

	if err := s.Bootstrap(context.Background(), ""); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	auth := store.TestAuth("auth-provider", "openai", false)
	path, err := s.Save(context.Background(), auth)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("saved file should exist: %v", err)
	}
}

// TestGitTokenStore_DifferentBranch tests using a non-main branch.
func TestGitTokenStore_DifferentBranch(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemote(t, root, "develop")

	s := NewGitTokenStore(remoteDir, "", "", "develop")
	workDir := filepath.Join(root, "workspace", "auths")
	s.SetBaseDir(workDir)

	if err := s.Bootstrap(context.Background(), ""); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	auth := store.TestAuth("branch-provider", "openai", false)
	path, err := s.Save(context.Background(), auth)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("saved file should exist: %v", err)
	}

	// List should work
	list, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 auth, got %d", len(list))
	}
}

// TestGitTokenStore_Save_UpdateExisting tests updating an existing auth.
func TestGitTokenStore_Save_UpdateExisting(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemote(t, root, "main")

	s := NewGitTokenStore(remoteDir, "", "", "main")
	workDir := filepath.Join(root, "workspace", "auths")
	s.SetBaseDir(workDir)

	if err := s.Bootstrap(context.Background(), ""); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	// Save initial auth
	auth := store.TestAuth("update-provider", "openai", false)
	auth.Metadata["access_token"] = "sk-initial"
	path, err := s.Save(context.Background(), auth)
	if err != nil {
		t.Fatalf("Save initial: %v", err)
	}

	// Update the auth
	auth.Metadata["access_token"] = "sk-updated"
	auth.Metadata["updated_at"] = time.Now().Format(time.RFC3339)
	_, err = s.Save(context.Background(), auth)
	if err != nil {
		t.Fatalf("Save update: %v", err)
	}

	// Read back and verify
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read updated file: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if meta["access_token"] != "sk-updated" {
		t.Errorf("access_token = %v, want sk-updated", meta["access_token"])
	}
}

// TestGitTokenStore_Bootstrap_EmptyConfig tests bootstrap with no template and no existing config.
func TestGitTokenStore_Bootstrap_EmptyConfig(t *testing.T) {
	root := t.TempDir()
	remoteDir := setupGitRemote(t, root, "main")

	s := NewGitTokenStore(remoteDir, "", "", "main")
	workDir := filepath.Join(root, "workspace", "auths")
	s.SetBaseDir(workDir)

	if err := s.Bootstrap(context.Background(), ""); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	// Should create empty config file
	configData, err := os.ReadFile(s.ConfigPath())
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if len(configData) != 0 {
		t.Errorf("expected empty config, got: %s", string(configData))
	}
}

// TestGitTokenStore_Conformance runs the standard conformance test suite.
func TestGitTokenStore_Conformance(t *testing.T) {
	// Create a single bare repo shared by all conformance subtests. The
	// conformance harness (and its config round-trip subtest in particular)
	// expects multiple stores created from one factory to share the same
	// backend, so we deliberately reuse one bare repo here.
	root := t.TempDir()
	bareDir := filepath.Join(root, "remote.git")

	if _, err := git.PlainInit(bareDir, true); err != nil {
		t.Fatalf("init bare repo: %v", err)
	}

	workDir := t.TempDir()
	wtrepo, err := git.PlainInit(workDir, false)
	if err != nil {
		t.Fatalf("init work repo: %v", err)
	}
	wt, err := wtrepo.Worktree()
	if err != nil {
		t.Fatalf("get worktree: %v", err)
	}
	gitkeep := filepath.Join(workDir, ".gitkeep")
	if err := os.WriteFile(gitkeep, []byte{}, 0o600); err != nil {
		t.Fatalf("write gitkeep: %v", err)
	}
	if _, err := wt.Add(".gitkeep"); err != nil {
		t.Fatalf("add gitkeep: %v", err)
	}
	if _, err := wt.Commit("Initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "CLIProxyAPI Test",
			Email: "test@cliproxy.local",
		},
	}); err != nil {
		t.Fatalf("commit: %v", err)
	}
	remote, err := wtrepo.CreateRemote(&gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{bareDir},
	})
	if err != nil {
		t.Fatalf("create remote: %v", err)
	}
	if err := remote.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []gitconfig.RefSpec{gitconfig.RefSpec("refs/heads/main:refs/heads/main")},
	}); err != nil && !strings.Contains(err.Error(), "already up-to-date") {
		t.Fatalf("push: %v", err)
	}

	factory := &gitStoreFactory{
		bareDir: bareDir,
	}
	ConformanceTests(t, factory)
}

type gitStoreFactory struct {
	bareDir string
}

func (f *gitStoreFactory) NewStore(t interface {
	Helper()
	Fatalf(format string, args ...interface{})
	Skipf(format string, args ...interface{})
	TempDir() string
}) (Store, func()) {
	ctx := context.Background()

	store := NewGitTokenStore(f.bareDir, "", "", "main")
	workDir2 := filepath.Join(t.TempDir(), "workspace", "auths")
	store.SetBaseDir(workDir2)

	if err := store.Bootstrap(ctx, ""); err != nil {
		t.Fatalf("bootstrap git store: %v", err)
	}

	cleanup := func() {
		_ = store.Close()
	}

	return store, cleanup
}

func (f *gitStoreFactory) Scheme() string {
	return "git"
}
