// Package checkpoint turns whatever the user typed into a checkpoint id, a
// commit and a parent. It is one of the four boundaries, and it talks to the
// outside world only through a Runner and only through documented Entire CLI
// output.
package checkpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/entireio/cli/impeach/internal/runner"
)

// TrailerKey is the commit-message trailer Entire writes to link a commit to
// the checkpoint that produced it.
//
// The Step 0 probe established that this is the only link that exists: neither
// `entire checkpoint list --json` nor `entire checkpoint explain --json`
// reports a commit sha. So this constant is load bearing, not a fallback.
const TrailerKey = "Entire-Checkpoint:"

// Resolved is the answer the rest of the pipeline builds on.
type Resolved struct {
	// ID is the checkpoint id, a 26-character ULID.
	ID string
	// Commit is the full sha of the commit carrying the trailer.
	Commit string
	// Parent is the first parent of Commit, or empty for a root commit.
	Parent string
	// Ambiguous holds the other commits that carried the same checkpoint
	// trailer, newest first, when more than one did. Reported, never guessed.
	Ambiguous []string
	// IsMerge is true when Commit has more than one parent. v1 diffs against
	// the first parent and says so.
	IsMerge bool
	// SessionIDs comes from the checkpoint metadata when it is available.
	SessionIDs []string
	// Agent is the agent that produced the checkpoint, when known.
	Agent string
	// FilesTouched is the checkpoint's changed-file list as Entire reports it,
	// repository-relative. Empty when the metadata could not be read.
	FilesTouched []string
	// MetadataErr records why the metadata lookup failed, if it did. A
	// checkpoint that resolves from the trailer alone is still usable, so this
	// is a note rather than an error.
	MetadataErr string
}

// Resolver resolves references. Its only dependency is a Runner.
type Resolver struct {
	Run  runner.Runner
	Repo string // repository path passed to git as -C
}

// Resolve accepts a checkpoint id, a checkpoint id prefix, or any commit-ish.
//
// A commit-ish is tried first, because a git reference resolves and a
// checkpoint id does not. Checkpoint ids are Crockford base32 ULIDs, so they
// cannot be assumed to be hex and cannot be told apart from a short sha by
// their alphabet alone. Asking git is cheaper and more honest than guessing.
func (r *Resolver) Resolve(ctx context.Context, ref string) (*Resolved, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("no checkpoint or commit given")
	}

	if sha, ok := r.revParse(ctx, ref); ok {
		return r.fromCommit(ctx, sha)
	}
	return r.fromCheckpointID(ctx, ref)
}

// revParse reports the full sha for a commit-ish, and whether it resolved.
func (r *Resolver) revParse(ctx context.Context, ref string) (string, bool) {
	stdout, _, exit, err := r.git(ctx, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil || exit != 0 {
		return "", false
	}
	sha := strings.TrimSpace(string(stdout))
	if sha == "" {
		return "", false
	}
	return sha, true
}

// fromCommit reads the trailer out of a commit's message.
func (r *Resolver) fromCommit(ctx context.Context, sha string) (*Resolved, error) {
	stdout, stderr, exit, err := r.git(ctx, "log", "-1", "--format=%B", sha)
	if err != nil {
		return nil, fmt.Errorf("read commit message for %s: %w", short(sha), err)
	}
	if exit != 0 {
		return nil, fmt.Errorf("read commit message for %s: %s", short(sha), firstLine(stderr))
	}

	id := ParseTrailer(string(stdout))
	if id == "" {
		return nil, fmt.Errorf("commit %s carries no %s trailer, so it has no checkpoint to audit", short(sha), TrailerKey)
	}

	res := &Resolved{ID: id, Commit: sha}
	if err := r.fillParents(ctx, res); err != nil {
		return nil, err
	}
	r.fillMetadata(ctx, res)
	return res, nil
}

// fromCheckpointID finds the commit that carries a checkpoint's trailer.
func (r *Resolver) fromCheckpointID(ctx context.Context, id string) (*Resolved, error) {
	if !looksLikeCheckpointID(id) {
		return nil, fmt.Errorf("%q is neither a commit this repository knows nor a checkpoint id", id)
	}

	// --grep is a regular expression, so the id is anchored and quoted to keep
	// a crafted argument from widening the search. The id is already known to
	// be base32 by looksLikeCheckpointID, but anchoring is free.
	pattern := "^" + TrailerKey + " " + id
	stdout, stderr, exit, err := r.git(ctx, "log", "--all", "--format=%H", "--grep="+pattern, "--regexp-ignore-case")
	if err != nil {
		return nil, fmt.Errorf("search for checkpoint %s: %w", id, err)
	}
	if exit != 0 {
		return nil, fmt.Errorf("search for checkpoint %s: %s", id, firstLine(stderr))
	}

	shas := nonEmptyLines(string(stdout))
	if len(shas) == 0 {
		return nil, fmt.Errorf("no commit in this repository carries checkpoint %s; it may not be fetched locally", id)
	}

	// git log lists newest first. Take the newest and keep the rest as the
	// reported ambiguity.
	res := &Resolved{ID: id, Commit: shas[0]}
	if len(shas) > 1 {
		res.Ambiguous = shas[1:]
	}
	// The typed id may be a prefix; recover the full one from the commit.
	if full := r.fullIDFromCommit(ctx, shas[0]); full != "" {
		res.ID = full
	}
	if err := r.fillParents(ctx, res); err != nil {
		return nil, err
	}
	r.fillMetadata(ctx, res)
	return res, nil
}

func (r *Resolver) fullIDFromCommit(ctx context.Context, sha string) string {
	stdout, _, exit, err := r.git(ctx, "log", "-1", "--format=%B", sha)
	if err != nil || exit != 0 {
		return ""
	}
	return ParseTrailer(string(stdout))
}

// fillParents records the first parent and whether the commit is a merge.
func (r *Resolver) fillParents(ctx context.Context, res *Resolved) error {
	stdout, stderr, exit, err := r.git(ctx, "rev-list", "--parents", "-n", "1", res.Commit)
	if err != nil {
		return fmt.Errorf("read parents of %s: %w", short(res.Commit), err)
	}
	if exit != 0 {
		return fmt.Errorf("read parents of %s: %s", short(res.Commit), firstLine(stderr))
	}
	fields := strings.Fields(strings.TrimSpace(string(stdout)))
	if len(fields) > 1 {
		res.Parent = fields[1]
	}
	res.IsMerge = len(fields) > 2
	return nil
}

// fillMetadata adds session ids, the agent and the changed-file list from
// `entire checkpoint explain --json`. Failure is recorded and tolerated: the
// trailer alone is enough to audit a commit, and missing data is a state.
func (r *Resolver) fillMetadata(ctx context.Context, res *Resolved) {
	ctx, cancel := context.WithTimeout(ctx, runner.TimeoutGraph)
	defer cancel()

	stdout, stderr, exit, err := r.Run.Run(ctx, "entire",
		[]string{"checkpoint", "explain", res.ID, "--json"}, nil)
	if err != nil {
		res.MetadataErr = err.Error()
		return
	}
	if exit != 0 {
		res.MetadataErr = fmt.Sprintf("entire checkpoint explain exited %d: %s", exit, firstLine(stderr))
		return
	}

	var env explainEnvelope
	if err := json.Unmarshal(stdout, &env); err != nil {
		res.MetadataErr = fmt.Sprintf("parse checkpoint metadata: %v", err)
		return
	}
	res.FilesTouched = env.FilesTouched
	for _, s := range env.Sessions {
		if s.SessionID != "" {
			res.SessionIDs = append(res.SessionIDs, s.SessionID)
		}
		if res.Agent == "" {
			res.Agent = s.Agent
		}
	}
}

// explainEnvelope is the documented shape of `checkpoint explain --json`, as
// observed by the Step 0 probe. Only the fields Impeach uses are named.
type explainEnvelope struct {
	CheckpointID string   `json:"checkpoint_id"`
	Branch       string   `json:"branch"`
	FilesTouched []string `json:"files_touched"`
	Sessions     []struct {
		Index     int      `json:"index"`
		SessionID string   `json:"session_id"`
		Agent     string   `json:"agent"`
		Model     string   `json:"model"`
		Files     []string `json:"files_touched"`
	} `json:"sessions"`
}

func (r *Resolver) git(ctx context.Context, args ...string) ([]byte, []byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, runner.TimeoutGit)
	defer cancel()
	full := append([]string{"-C", r.Repo}, args...)
	return r.Run.Run(ctx, "git", full, nil)
}

// ParseTrailer pulls the checkpoint id out of a commit message.
//
// It scans every line rather than only the trailer block, because a trailer
// can be followed by other trailers and Impeach should find it either way. The
// last occurrence wins, which matches git's own trailer semantics for a
// repeated key.
func ParseTrailer(message string) string {
	id := ""
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimSpace(line)
		if len(line) <= len(TrailerKey) {
			continue
		}
		if !strings.EqualFold(line[:len(TrailerKey)], TrailerKey) {
			continue
		}
		val := strings.TrimSpace(line[len(TrailerKey):])
		// A trailer value is a single token; anything after whitespace is not
		// part of the id.
		if i := strings.IndexAny(val, " \t"); i >= 0 {
			val = val[:i]
		}
		if val != "" {
			id = val
		}
	}
	return id
}

// checkpointIDLen is the length of a ULID in Crockford base32.
const checkpointIDLen = 26

// looksLikeCheckpointID reports whether a string could be a checkpoint id or a
// prefix of one. Crockford base32 excludes I, L, O and U to avoid confusion
// with 1, 1, 0 and V.
//
// A prefix must be at least eight characters. Shorter than that and a typo
// would scan the whole history for a pattern that could match anything.
func looksLikeCheckpointID(s string) bool {
	if len(s) < 8 || len(s) > checkpointIDLen {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'A' && c <= 'Z':
			if c == 'I' || c == 'L' || c == 'O' || c == 'U' {
				return false
			}
		case c >= 'a' && c <= 'z':
			if c == 'i' || c == 'l' || c == 'o' || c == 'u' {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
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

func short(sha string) string {
	if len(sha) > 9 {
		return sha[:9]
	}
	return sha
}
