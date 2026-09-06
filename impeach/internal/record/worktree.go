// Package record builds the record side of the cross-examination: the
// worktrees the checkpoint's code is read from, the entity-level changes and
// impact from Entire Graph, and the test rerun.
package record

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/entireio/cli/impeach/internal/runner"
)

// DataDir returns the directory Impeach may write to.
//
// ENTIRE_PLUGIN_DATA_DIR is preferred. The Step 0 probe established that the
// Entire dispatcher sets it for plugins found on a raw PATH as well as for
// managed installs, and that it does not create the directory, so creating it
// is Impeach's job. An empty value means absent: the dispatcher strips the
// variable rather than pass a value it did not sanction, so a stray inherited
// value must not be trusted over the fallback.
func DataDir() (string, error) {
	if v := strings.TrimSpace(os.Getenv("ENTIRE_PLUGIN_DATA_DIR")); v != "" && filepath.IsAbs(v) {
		return v, nil
	}
	if v := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); v != "" && filepath.IsAbs(v) {
		return filepath.Join(v, "impeach"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("no ENTIRE_PLUGIN_DATA_DIR, no XDG_CACHE_HOME and no home directory: %w", err)
	}
	return filepath.Join(home, ".cache", "impeach"), nil
}

// Worktrees holds the two detached checkouts an audit reads from: the
// checkpoint's commit and its first parent. Graph and the rerun must see the
// code as it was, not as it is today.
type Worktrees struct {
	Head string // path to the checkout of the commit
	Base string // path to the checkout of the parent, empty for a root commit

	run    runner.Runner
	repo   string
	root   string
	keep   bool
	closed bool
}

// AddWorktrees checks out commit and its parent under the data directory,
// keyed by sha so two audits of different commits do not collide.
//
// A missing parent is not an error. A root commit has no base, and the
// verifiers treat an absent base as a missing channel rather than a failure.
func AddWorktrees(ctx context.Context, run runner.Runner, repo, dataDir, commit, parent string, keep bool) (*Worktrees, error) {
	if commit == "" {
		return nil, fmt.Errorf("no commit to check out")
	}
	root := filepath.Join(dataDir, "wt", shortSHA(commit))
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create worktree root %s: %w", root, err)
	}

	w := &Worktrees{run: run, repo: repo, root: root, keep: keep}

	head := filepath.Join(root, "head")
	if err := w.add(ctx, head, commit); err != nil {
		return nil, err
	}
	w.Head = head

	if parent != "" {
		base := filepath.Join(root, "base")
		if err := w.add(ctx, base, parent); err != nil {
			// The head worktree is already live; do not leak it.
			_ = w.Close(ctx)
			return nil, err
		}
		w.Base = base
	}
	return w, nil
}

// add creates one detached worktree, reusing an existing checkout of the same
// commit rather than failing on a second run.
func (w *Worktrees) add(ctx context.Context, path, commit string) error {
	if at, ok := w.headOf(ctx, path); ok {
		if at == commit {
			return nil
		}
		// A stale checkout of a different commit would be audited silently,
		// which is exactly the failure Impeach exists to catch. Remove it.
		if err := w.remove(ctx, path); err != nil {
			return fmt.Errorf("replace stale worktree %s: %w", path, err)
		}
	}
	_, stderr, exit, err := w.git(ctx, "worktree", "add", "--detach", "--quiet", path, commit)
	if err != nil {
		return fmt.Errorf("git worktree add %s: %w", path, err)
	}
	if exit != 0 {
		return fmt.Errorf("git worktree add %s: %s", path, firstLine(stderr))
	}
	return nil
}

// headOf reports the commit an existing worktree is checked out at.
//
// The question is asked of git rather than of the filesystem, deliberately.
// Statting a .git entry inside the directory answered a slightly different
// question, "is something git-shaped here", which counts a directory that is
// not one of this repository's worktrees at all, and it duplicated a probe the
// host CLI owns and guards against being reimplemented. `git worktree list`
// answers exactly what add needs to know: is this path registered as a
// worktree of this repository, and at what commit.
//
// A prunable record is treated as absent. Git still lists a worktree whose
// directory has been deleted, and reporting its recorded HEAD would make add
// reuse a checkout that is not there.
func (w *Worktrees) headOf(ctx context.Context, path string) (string, bool) {
	stdout, _, exit, err := w.git(ctx, "worktree", "list", "--porcelain")
	if err != nil || exit != 0 {
		return "", false
	}
	want := resolvePath(path)
	// Porcelain output is one record per worktree, separated by a blank line.
	for _, record := range strings.Split(string(stdout), "\n\n") {
		var head string
		matched, prunable := false, false
		for _, line := range strings.Split(record, "\n") {
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "worktree "):
				matched = resolvePath(strings.TrimPrefix(line, "worktree ")) == want
			case strings.HasPrefix(line, "HEAD "):
				head = strings.TrimSpace(strings.TrimPrefix(line, "HEAD "))
			case line == "prunable" || strings.HasPrefix(line, "prunable "):
				prunable = true
			}
		}
		if matched && !prunable && head != "" {
			return head, true
		}
	}
	return "", false
}

// resolvePath makes two spellings of one directory comparable. Git prints
// worktree paths with symlinks resolved, while ours are joined from the data
// directory, and on macOS both the cache and temp roots are symlinks.
func resolvePath(path string) string {
	if resolved, err := filepath.EvalSymlinks(strings.TrimSpace(path)); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(strings.TrimSpace(path))
}

func (w *Worktrees) remove(ctx context.Context, path string) error {
	_, stderr, exit, err := w.git(ctx, "worktree", "remove", "--force", path)
	if err != nil {
		return err
	}
	if exit != 0 {
		// Fall back to removing the directory and pruning the administrative
		// record, which is what git itself suggests for a broken worktree.
		if rmErr := os.RemoveAll(path); rmErr != nil {
			return fmt.Errorf("%s: %w", firstLine(stderr), rmErr)
		}
		_, _, _, _ = w.git(ctx, "worktree", "prune")
	}
	return nil
}

// Close removes the worktrees unless the caller asked to keep them. It is safe
// to call twice.
func (w *Worktrees) Close(ctx context.Context) error {
	if w == nil || w.closed {
		return nil
	}
	w.closed = true
	if w.keep {
		return nil
	}

	var errs []string
	for _, p := range []string{w.Head, w.Base} {
		if p == "" {
			continue
		}
		if err := w.remove(ctx, p); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", p, err))
		}
	}
	// The keyed parent directory is ours, so clean it up when it empties.
	if entries, err := os.ReadDir(w.root); err == nil && len(entries) == 0 {
		_ = os.Remove(w.root)
	}
	if len(errs) > 0 {
		return fmt.Errorf("remove worktrees: %s", strings.Join(errs, "; "))
	}
	return nil
}

// Root is the directory the worktrees live under. Baselines are written beside
// them.
func (w *Worktrees) Root() string { return w.root }

func (w *Worktrees) git(ctx context.Context, args ...string) ([]byte, []byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, runner.TimeoutGit)
	defer cancel()
	return w.run.Run(ctx, "git", append([]string{"-C", w.repo}, args...), nil)
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func firstLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return "no output"
	}
	return s
}
