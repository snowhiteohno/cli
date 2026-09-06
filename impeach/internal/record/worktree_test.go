package record

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entireio/cli/impeach/internal/runner"
)

const (
	headSHA = "458bb14287ec2e0dcd4b6c52a779c9213af5be52"
	baseSHA = "650a885fdf96dde7f230729ca19db0c8ea217f53"
)

func TestDataDirPrefersPluginDataDir(t *testing.T) {
	// Not parallel: mutates the environment.
	t.Setenv("ENTIRE_PLUGIN_DATA_DIR", "/data/entire/impeach")
	t.Setenv("XDG_CACHE_HOME", "/cache")
	got, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir() error = %v", err)
	}
	if got != "/data/entire/impeach" {
		t.Errorf("DataDir() = %q, want the plugin data dir", got)
	}
}

// The dispatcher strips the variable rather than pass a value it did not
// sanction, so an empty or relative value means absent and must not win.
func TestDataDirIgnoresUnusablePluginDataDir(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/cache")
	for _, bad := range []string{"", "   ", "relative/path"} {
		t.Setenv("ENTIRE_PLUGIN_DATA_DIR", bad)
		got, err := DataDir()
		if err != nil {
			t.Fatalf("DataDir() error = %v", err)
		}
		if got != filepath.Join("/cache", "impeach") {
			t.Errorf("ENTIRE_PLUGIN_DATA_DIR=%q gave %q, want the XDG fallback", bad, got)
		}
	}
}

func TestDataDirFallsBackToHomeCache(t *testing.T) {
	t.Setenv("ENTIRE_PLUGIN_DATA_DIR", "")
	t.Setenv("XDG_CACHE_HOME", "")
	got, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir() error = %v", err)
	}
	if !strings.HasSuffix(got, filepath.Join(".cache", "impeach")) {
		t.Errorf("DataDir() = %q, want a ~/.cache/impeach fallback", got)
	}
}

func TestAddWorktreesCheckoutsHeadAndBase(t *testing.T) {
	t.Parallel()
	data := t.TempDir()
	root := filepath.Join(data, "wt", headSHA[:12])
	f := runner.NewFake(
		runner.Call{Name: "git", Args: []string{"-C", "/repo", "worktree", "add", "--detach", "--quiet", filepath.Join(root, "head"), headSHA}},
		runner.Call{Name: "git", Args: []string{"-C", "/repo", "worktree", "add", "--detach", "--quiet", filepath.Join(root, "base"), baseSHA}},
	)

	w, err := AddWorktrees(context.Background(), f, "/repo", data, headSHA, baseSHA, false)
	if err != nil {
		t.Fatalf("AddWorktrees() error = %v", err)
	}
	if w.Head != filepath.Join(root, "head") {
		t.Errorf("Head = %q", w.Head)
	}
	if w.Base != filepath.Join(root, "base") {
		t.Errorf("Base = %q", w.Base)
	}
	// Keyed by sha, so auditing a different commit cannot collide.
	if !strings.Contains(w.Root(), headSHA[:12]) {
		t.Errorf("Root() = %q, want it keyed by the commit sha", w.Root())
	}
}

// A root commit has no parent. That is a missing channel, not a failure.
func TestAddWorktreesWithoutParent(t *testing.T) {
	t.Parallel()
	data := t.TempDir()
	root := filepath.Join(data, "wt", headSHA[:12])
	f := runner.NewFake(
		runner.Call{Name: "git", Args: []string{"-C", "/repo", "worktree", "add", "--detach", "--quiet", filepath.Join(root, "head"), headSHA}},
	)

	w, err := AddWorktrees(context.Background(), f, "/repo", data, headSHA, "", false)
	if err != nil {
		t.Fatalf("AddWorktrees() error = %v", err)
	}
	if w.Base != "" {
		t.Errorf("Base = %q, want empty", w.Base)
	}
}

func TestAddWorktreesRequiresACommit(t *testing.T) {
	t.Parallel()
	if _, err := AddWorktrees(context.Background(), runner.NewFake(), "/repo", t.TempDir(), "", "", false); err == nil {
		t.Fatal("AddWorktrees() error = nil, want an error")
	}
}

func TestAddWorktreesReportsGitFailure(t *testing.T) {
	t.Parallel()
	data := t.TempDir()
	root := filepath.Join(data, "wt", headSHA[:12])
	f := runner.NewFake(runner.Call{
		Name: "git", Args: []string{"-C", "/repo", "worktree", "add", "--detach", "--quiet", filepath.Join(root, "head"), headSHA},
		Stderr: "fatal: invalid reference\n", Exit: 128,
	})
	_, err := AddWorktrees(context.Background(), f, "/repo", data, headSHA, "", false)
	if err == nil {
		t.Fatal("AddWorktrees() error = nil, want the git failure surfaced")
	}
	if !strings.Contains(err.Error(), "invalid reference") {
		t.Errorf("error = %v, want git's stderr included", err)
	}
}

// If the base checkout fails the head must not be left behind.
func TestAddWorktreesCleansUpHeadWhenBaseFails(t *testing.T) {
	t.Parallel()
	data := t.TempDir()
	root := filepath.Join(data, "wt", headSHA[:12])
	f := runner.NewFake(
		runner.Call{Name: "git", Args: []string{"-C", "/repo", "worktree", "add", "--detach", "--quiet", filepath.Join(root, "head"), headSHA}},
		runner.Call{
			Name: "git", Args: []string{"-C", "/repo", "worktree", "add", "--detach", "--quiet", filepath.Join(root, "base"), baseSHA},
			Stderr: "fatal: cannot add\n", Exit: 128,
		},
		runner.Call{Name: "git", Args: []string{"-C", "/repo", "worktree", "remove", "--force", filepath.Join(root, "head")}},
	)
	if _, err := AddWorktrees(context.Background(), f, "/repo", data, headSHA, baseSHA, false); err == nil {
		t.Fatal("AddWorktrees() error = nil, want an error")
	}
	var removed bool
	for _, r := range f.Requests() {
		if strings.Contains(r, "worktree remove") && strings.Contains(r, "head") {
			removed = true
		}
	}
	if !removed {
		t.Errorf("head worktree was not removed after the base failed; requests: %v", f.Requests())
	}
}

func TestCloseRemovesBothWorktrees(t *testing.T) {
	t.Parallel()
	data := t.TempDir()
	root := filepath.Join(data, "wt", headSHA[:12])
	f := runner.NewFake(
		runner.Call{Name: "git", Args: []string{"-C", "/repo", "worktree", "add", "--detach", "--quiet", filepath.Join(root, "head"), headSHA}},
		runner.Call{Name: "git", Args: []string{"-C", "/repo", "worktree", "add", "--detach", "--quiet", filepath.Join(root, "base"), baseSHA}},
		runner.Call{Name: "git", Args: []string{"-C", "/repo", "worktree", "remove", "--force", filepath.Join(root, "head")}},
		runner.Call{Name: "git", Args: []string{"-C", "/repo", "worktree", "remove", "--force", filepath.Join(root, "base")}},
	)
	w, err := AddWorktrees(context.Background(), f, "/repo", data, headSHA, baseSHA, false)
	if err != nil {
		t.Fatalf("AddWorktrees() error = %v", err)
	}
	if err := w.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	var removes int
	for _, r := range f.Requests() {
		if strings.Contains(r, "worktree remove") {
			removes++
		}
	}
	if removes != 2 {
		t.Errorf("worktree remove called %d times, want 2; requests: %v", removes, f.Requests())
	}
	// Close is idempotent, so a deferred Close after an explicit one is safe.
	if err := w.Close(context.Background()); err != nil {
		t.Errorf("second Close() error = %v", err)
	}
}

func TestCloseKeepsWorktreesWhenAsked(t *testing.T) {
	t.Parallel()
	data := t.TempDir()
	root := filepath.Join(data, "wt", headSHA[:12])
	f := runner.NewFake(
		runner.Call{Name: "git", Args: []string{"-C", "/repo", "worktree", "add", "--detach", "--quiet", filepath.Join(root, "head"), headSHA}},
	)
	w, err := AddWorktrees(context.Background(), f, "/repo", data, headSHA, "", true)
	if err != nil {
		t.Fatalf("AddWorktrees() error = %v", err)
	}
	if err := w.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	for _, r := range f.Requests() {
		if strings.Contains(r, "worktree remove") {
			t.Errorf("--keep-worktrees still removed a worktree: %v", f.Requests())
		}
	}
}

// A leftover checkout of the wrong commit is the one thing that must never be
// audited silently, since auditing stale code is the failure Impeach exists to
// catch.
func TestAddWorktreesReplacesStaleCheckout(t *testing.T) {
	t.Parallel()
	data := t.TempDir()
	root := filepath.Join(data, "wt", headSHA[:12])
	head := filepath.Join(root, "head")
	if err := os.MkdirAll(head, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(head, ".git"), []byte("gitdir: elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	f := runner.NewFake(
		// The existing checkout is at some other commit.
		runner.Call{Name: "git", Args: []string{"-C", head, "rev-parse", "HEAD"}, Stdout: "ffffffffffffffffffffffffffffffffffffffff\n"},
		runner.Call{Name: "git", Args: []string{"-C", "/repo", "worktree", "remove", "--force", head}},
		runner.Call{Name: "git", Args: []string{"-C", "/repo", "worktree", "add", "--detach", "--quiet", head, headSHA}},
	)
	if _, err := AddWorktrees(context.Background(), f, "/repo", data, headSHA, "", false); err != nil {
		t.Fatalf("AddWorktrees() error = %v", err)
	}
	reqs := strings.Join(f.Requests(), " | ")
	if !strings.Contains(reqs, "worktree remove") {
		t.Errorf("stale checkout was not removed; requests: %s", reqs)
	}
	if !strings.Contains(reqs, "worktree add") {
		t.Errorf("replacement checkout was not created; requests: %s", reqs)
	}
}

// An existing checkout already at the right commit is reused rather than
// rebuilt, so a second audit of the same commit is cheap.
func TestAddWorktreesReusesMatchingCheckout(t *testing.T) {
	t.Parallel()
	data := t.TempDir()
	root := filepath.Join(data, "wt", headSHA[:12])
	head := filepath.Join(root, "head")
	if err := os.MkdirAll(head, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(head, ".git"), []byte("gitdir: elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := runner.NewFake(
		runner.Call{Name: "git", Args: []string{"-C", head, "rev-parse", "HEAD"}, Stdout: headSHA + "\n"},
	)
	if _, err := AddWorktrees(context.Background(), f, "/repo", data, headSHA, "", false); err != nil {
		t.Fatalf("AddWorktrees() error = %v", err)
	}
	for _, r := range f.Requests() {
		if strings.Contains(r, "worktree add") {
			t.Errorf("matching checkout was rebuilt: %v", f.Requests())
		}
	}
}
