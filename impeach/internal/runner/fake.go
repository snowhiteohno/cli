package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Call is one recorded external command and its result. Recorded calls are
// committed under fixtures/recorded/<scenario>/ so tests can replay a whole
// audit with no Entire, no git and no agent.
//
// Stdin is stored as a hash, not as text, because the only thing that goes to
// stdin is a model prompt containing assistant text, and the security policy
// keeps transcript text out of committed data.
type Call struct {
	Name     string   `json:"name"`
	Args     []string `json:"args"`
	StdinSHA string   `json:"stdin_sha256,omitempty"`
	Stdout   string   `json:"stdout"`
	Stderr   string   `json:"stderr,omitempty"`
	Exit     int      `json:"exit"`
	Err      string   `json:"error,omitempty"`
}

// Key is the identity a replaying Fake matches on: the executable and its
// argv. Two calls with the same key are expected to be interchangeable, which
// holds because no command Impeach runs depends on ambient state.
func (c Call) Key() string {
	return Format(c.Name, c.Args)
}

// HashStdin returns the hash a Call stores for the given stdin.
func HashStdin(stdin []byte) string {
	if len(stdin) == 0 {
		return ""
	}
	sum := sha256.Sum256(stdin)
	return hex.EncodeToString(sum[:])
}

// Recording wraps a Runner and writes every call it makes to a directory as
// <n>.json, in order. This is what `impeach record` uses.
type Recording struct {
	Inner Runner
	Dir   string

	mu sync.Mutex
	n  int
	// Scrub rewrites recorded output before it is written. Fixtures are
	// committed, so absolute paths and identities have to go.
	Scrub func(string) string
}

// Run implements Runner.
func (r *Recording) Run(ctx context.Context, name string, args []string, stdin []byte) ([]byte, []byte, int, error) {
	stdout, stderr, exit, err := r.Inner.Run(ctx, name, args, stdin)

	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.n
	r.n++

	scrub := r.Scrub
	if scrub == nil {
		scrub = func(s string) string { return s }
	}
	c := Call{
		Name:     name,
		Args:     args,
		StdinSHA: HashStdin(stdin),
		Stdout:   scrub(string(stdout)),
		Stderr:   scrub(string(stderr)),
		Exit:     exit,
	}
	if err != nil {
		c.Err = scrub(err.Error())
	}
	// Args can carry absolute worktree paths, so they are scrubbed too.
	c.Args = make([]string, len(args))
	for i, a := range args {
		c.Args[i] = scrub(a)
	}

	if mkErr := os.MkdirAll(r.Dir, 0o755); mkErr != nil {
		return stdout, stderr, exit, err
	}
	// HTML escaping is off because these fixtures are committed and reviewed
	// by hand, and a scrub placeholder like <repo> is unreadable as \u003crepo\u003e.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if mErr := enc.Encode(c); mErr != nil {
		return stdout, stderr, exit, err
	}
	path := filepath.Join(r.Dir, strconv.Itoa(n)+".json")
	_ = os.WriteFile(path, buf.Bytes(), 0o644)

	return stdout, stderr, exit, err
}

// Fake replays recorded calls. It matches on name and argv, so the order calls
// are made in does not have to match the order they were recorded in.
type Fake struct {
	byKey map[string][]Call

	mu       sync.Mutex
	requests []string
	// Strict makes an unmatched call an error rather than an empty success.
	// Tests want strict; a partially recorded scenario does not.
	Strict bool
	// Normalize rewrites a live call's key before matching, and must be the
	// same transformation the Recording applied when it wrote the scenario.
	//
	// Without this, scrubbing defeats replay: a recording made portable by
	// rewriting an absolute repository path to <repo> can never match a live
	// call that still carries the real path. Recording and replay have to be
	// symmetric or the fixtures are write-only.
	Normalize func(string) string
}

// NewFake builds a Fake from calls held in memory.
func NewFake(calls ...Call) *Fake {
	f := &Fake{byKey: map[string][]Call{}, Strict: true}
	for _, c := range calls {
		f.byKey[c.Key()] = append(f.byKey[c.Key()], c)
	}
	return f
}

// LoadFake reads a recorded scenario directory written by Recording.
func LoadFake(dir string) (*Fake, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read scenario %s: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	// Numeric order, so a replay reads in the order it was recorded.
	sort.Slice(names, func(i, j int) bool {
		return numPrefix(names[i]) < numPrefix(names[j])
	})

	f := &Fake{byKey: map[string][]Call{}, Strict: true}
	for _, n := range names {
		blob, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", n, err)
		}
		var c Call
		if err := json.Unmarshal(blob, &c); err != nil {
			return nil, fmt.Errorf("parse %s: %w", n, err)
		}
		f.byKey[c.Key()] = append(f.byKey[c.Key()], c)
	}
	return f, nil
}

func numPrefix(name string) int {
	base := strings.TrimSuffix(name, ".json")
	n, err := strconv.Atoi(base)
	if err != nil {
		return 1 << 30
	}
	return n
}

// Run implements Runner. Repeated identical calls are served in the order they
// were recorded, and the last recording is reused once they run out, so a
// verifier that asks the same question twice gets the same answer.
func (f *Fake) Run(_ context.Context, name string, args []string, _ []byte) ([]byte, []byte, int, error) {
	key := Format(name, args)
	if f.Normalize != nil {
		key = f.Normalize(key)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, key)

	queue, ok := f.byKey[key]
	if !ok || len(queue) == 0 {
		if f.Strict {
			return nil, nil, -1, fmt.Errorf("no recorded call for: %s", key)
		}
		return nil, nil, 0, nil
	}
	c := queue[0]
	if len(queue) > 1 {
		f.byKey[key] = queue[1:]
	}
	var err error
	if c.Err != "" {
		err = fmt.Errorf("%s", c.Err)
	}
	return []byte(c.Stdout), []byte(c.Stderr), c.Exit, err
}

// Requests returns every command line the Fake was asked for, in order. Tests
// assert on this to pin down which commands a boundary actually runs.
func (f *Fake) Requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.requests))
	copy(out, f.requests)
	return out
}
