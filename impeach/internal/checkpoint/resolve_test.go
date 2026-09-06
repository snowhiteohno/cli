package checkpoint

import (
	"context"
	"strings"
	"testing"

	"github.com/entireio/cli/impeach/internal/runner"
)

// The real trailer from the Step 0 probe commit, so the parser is tested
// against a message Entire actually wrote.
const probeMessage = `fixture: use bankers rounding in round_money

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Entire-Checkpoint: 01M1TET4N33VMY0DTHNZKV5HT9
`

func TestParseTrailer(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		msg  string
		want string
	}{
		{"probe commit", probeMessage, "01M1TET4N33VMY0DTHNZKV5HT9"},
		{"trailer only", "Entire-Checkpoint: 01ABCDEFGHJKMNPQRSTVWXYZ00", "01ABCDEFGHJKMNPQRSTVWXYZ00"},
		{"no trailer", "just a commit\n\nwith a body\n", ""},
		{"empty message", "", ""},
		{"case insensitive key", "entire-checkpoint: 01M1TET4N33VMY0DTHNZKV5HT9", "01M1TET4N33VMY0DTHNZKV5HT9"},
		{"indented trailer", "  Entire-Checkpoint: 01M1TET4N33VMY0DTHNZKV5HT9  ", "01M1TET4N33VMY0DTHNZKV5HT9"},
		{"value with trailing junk", "Entire-Checkpoint: 01M1TET4N33VMY0DTHNZKV5HT9 extra", "01M1TET4N33VMY0DTHNZKV5HT9"},
		{"empty value", "Entire-Checkpoint:", ""},
		{"repeated key takes the last", "Entire-Checkpoint: 01AAAAAAAAAAAAAAAAAAAAAAAA\nEntire-Checkpoint: 01BBBBBBBBBBBBBBBBBBBBBBBB", "01BBBBBBBBBBBBBBBBBBBBBBBB"},
		{"substring is not a trailer", "see Entire-Checkpoint: nope inline", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := ParseTrailer(c.msg); got != c.want {
				t.Errorf("ParseTrailer() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestLooksLikeCheckpointID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want bool
	}{
		{"01M1TET4N33VMY0DTHNZKV5HT9", true},
		{"01M1TET4", true},
		{"01M1TE", false},                      // too short to scan history for
		{"01M1TET4N33VMY0DTHNZKV5HT99", false}, // longer than a ULID
		{"458bb14", false},                     // short sha, too short anyway
		{"01M1TETIN33VMY0DTHNZKV5HT9", false},  // I is not in Crockford base32
		{"01M1TETLN33VMY0DTHNZKV5HT9", false},  // L is not either
		{"01M1TETON33VMY0DTHNZKV5HT9", false},  // nor O
		{"01M1TETUN33VMY0DTHNZKV5HT9", false},  // nor U
		{"01M1TET4-N33VMY0DTHNZKV5H", false},   // punctuation
		{"../../etc/passwd", false},            // path traversal shaped
		{"", false},
	}
	for _, c := range cases {
		if got := looksLikeCheckpointID(c.in); got != c.want {
			t.Errorf("looksLikeCheckpointID(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// explainJSON is the envelope shape the probe observed, trimmed to what
// Impeach reads.
const explainJSON = `{
  "checkpoint_id": "01M1TET4N33VMY0DTHNZKV5HT9",
  "branch": "main",
  "files_touched": ["impeach/fixtures/app/app/service.py"],
  "sessions": [
    {"index": 0, "session_id": "1c439358-7307-4721-8422-949e897dbfba",
     "agent": "Claude Code", "model": "claude-opus-5",
     "files_touched": ["impeach/fixtures/app/app/service.py"]}
  ]
}`

func TestResolveFromCommitish(t *testing.T) {
	t.Parallel()
	f := runner.NewFake(
		runner.Call{
			Name: "git", Args: []string{"-C", "/repo", "rev-parse", "--verify", "--quiet", "HEAD^{commit}"},
			Stdout: "458bb14287ec2e0dcd4b6c52a779c9213af5be52\n",
		},
		runner.Call{
			Name: "git", Args: []string{"-C", "/repo", "log", "-1", "--format=%B", "458bb14287ec2e0dcd4b6c52a779c9213af5be52"},
			Stdout: probeMessage,
		},
		runner.Call{
			Name: "git", Args: []string{"-C", "/repo", "rev-list", "--parents", "-n", "1", "458bb14287ec2e0dcd4b6c52a779c9213af5be52"},
			Stdout: "458bb14287ec2e0dcd4b6c52a779c9213af5be52 650a885fdf96dde7f230729ca19db0c8ea217f53\n",
		},
		runner.Call{
			Name: "entire", Args: []string{"checkpoint", "explain", "01M1TET4N33VMY0DTHNZKV5HT9", "--json"},
			Stdout: explainJSON,
		},
	)
	r := &Resolver{Run: f, Repo: "/repo"}

	got, err := r.Resolve(context.Background(), "HEAD")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.ID != "01M1TET4N33VMY0DTHNZKV5HT9" {
		t.Errorf("ID = %q", got.ID)
	}
	if got.Commit != "458bb14287ec2e0dcd4b6c52a779c9213af5be52" {
		t.Errorf("Commit = %q", got.Commit)
	}
	if got.Parent != "650a885fdf96dde7f230729ca19db0c8ea217f53" {
		t.Errorf("Parent = %q", got.Parent)
	}
	if got.IsMerge {
		t.Error("IsMerge = true, want false for a single-parent commit")
	}
	if len(got.SessionIDs) != 1 || got.SessionIDs[0] != "1c439358-7307-4721-8422-949e897dbfba" {
		t.Errorf("SessionIDs = %v", got.SessionIDs)
	}
	if got.Agent != "Claude Code" {
		t.Errorf("Agent = %q", got.Agent)
	}
	if len(got.FilesTouched) != 1 || got.FilesTouched[0] != "impeach/fixtures/app/app/service.py" {
		t.Errorf("FilesTouched = %v", got.FilesTouched)
	}
	if got.MetadataErr != "" {
		t.Errorf("MetadataErr = %q, want empty", got.MetadataErr)
	}
}

func TestResolveFromCheckpointID(t *testing.T) {
	t.Parallel()
	const id = "01M1TET4N33VMY0DTHNZKV5HT9"
	f := runner.NewFake(
		// A checkpoint id is not a commit-ish, so rev-parse must fail first.
		runner.Call{
			Name: "git", Args: []string{"-C", "/repo", "rev-parse", "--verify", "--quiet", id + "^{commit}"},
			Exit: 1,
		},
		runner.Call{
			Name: "git", Args: []string{"-C", "/repo", "log", "--all", "--format=%H",
				"--grep=^Entire-Checkpoint: " + id, "--regexp-ignore-case"},
			Stdout: "458bb14287ec2e0dcd4b6c52a779c9213af5be52\n",
		},
		runner.Call{
			Name: "git", Args: []string{"-C", "/repo", "log", "-1", "--format=%B", "458bb14287ec2e0dcd4b6c52a779c9213af5be52"},
			Stdout: probeMessage,
		},
		runner.Call{
			Name: "git", Args: []string{"-C", "/repo", "rev-list", "--parents", "-n", "1", "458bb14287ec2e0dcd4b6c52a779c9213af5be52"},
			Stdout: "458bb14287ec2e0dcd4b6c52a779c9213af5be52 650a885fdf96dde7f230729ca19db0c8ea217f53\n",
		},
		runner.Call{
			Name: "entire", Args: []string{"checkpoint", "explain", id, "--json"},
			Stdout: explainJSON,
		},
	)
	r := &Resolver{Run: f, Repo: "/repo"}

	got, err := r.Resolve(context.Background(), id)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Commit != "458bb14287ec2e0dcd4b6c52a779c9213af5be52" {
		t.Errorf("Commit = %q", got.Commit)
	}
	if len(got.Ambiguous) != 0 {
		t.Errorf("Ambiguous = %v, want none", got.Ambiguous)
	}
}

func TestResolveReportsAmbiguity(t *testing.T) {
	t.Parallel()
	const id = "01M1TET4N33VMY0DTHNZKV5HT9"
	newest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	older := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	f := runner.NewFake(
		runner.Call{Name: "git", Args: []string{"-C", "/repo", "rev-parse", "--verify", "--quiet", id + "^{commit}"}, Exit: 1},
		runner.Call{
			Name: "git", Args: []string{"-C", "/repo", "log", "--all", "--format=%H",
				"--grep=^Entire-Checkpoint: " + id, "--regexp-ignore-case"},
			Stdout: newest + "\n" + older + "\n",
		},
		runner.Call{Name: "git", Args: []string{"-C", "/repo", "log", "-1", "--format=%B", newest}, Stdout: probeMessage},
		runner.Call{
			Name: "git", Args: []string{"-C", "/repo", "rev-list", "--parents", "-n", "1", newest},
			Stdout: newest + " 650a885fdf96dde7f230729ca19db0c8ea217f53\n",
		},
		runner.Call{Name: "entire", Args: []string{"checkpoint", "explain", id, "--json"}, Stdout: explainJSON},
	)
	r := &Resolver{Run: f, Repo: "/repo"}

	got, err := r.Resolve(context.Background(), id)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	// The newest wins and the rest is reported, never silently dropped.
	if got.Commit != newest {
		t.Errorf("Commit = %q, want the newest %q", got.Commit, newest)
	}
	if len(got.Ambiguous) != 1 || got.Ambiguous[0] != older {
		t.Errorf("Ambiguous = %v, want [%s]", got.Ambiguous, older)
	}
}

func TestResolveDetectsMergeCommit(t *testing.T) {
	t.Parallel()
	sha := "458bb14287ec2e0dcd4b6c52a779c9213af5be52"
	f := runner.NewFake(
		runner.Call{Name: "git", Args: []string{"-C", "/repo", "rev-parse", "--verify", "--quiet", "HEAD^{commit}"}, Stdout: sha},
		runner.Call{Name: "git", Args: []string{"-C", "/repo", "log", "-1", "--format=%B", sha}, Stdout: probeMessage},
		runner.Call{
			Name: "git", Args: []string{"-C", "/repo", "rev-list", "--parents", "-n", "1", sha},
			Stdout: sha + " 650a885fdf96dde7f230729ca19db0c8ea217f53 999a885fdf96dde7f230729ca19db0c8ea217f53\n",
		},
		runner.Call{Name: "entire", Args: []string{"checkpoint", "explain", "01M1TET4N33VMY0DTHNZKV5HT9", "--json"}, Stdout: explainJSON},
	)
	r := &Resolver{Run: f, Repo: "/repo"}

	got, err := r.Resolve(context.Background(), "HEAD")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !got.IsMerge {
		t.Error("IsMerge = false, want true")
	}
	// First parent is used, per the v1 scope decision.
	if got.Parent != "650a885fdf96dde7f230729ca19db0c8ea217f53" {
		t.Errorf("Parent = %q, want the first parent", got.Parent)
	}
}

func TestResolveRootCommitHasNoParent(t *testing.T) {
	t.Parallel()
	sha := "458bb14287ec2e0dcd4b6c52a779c9213af5be52"
	f := runner.NewFake(
		runner.Call{Name: "git", Args: []string{"-C", "/repo", "rev-parse", "--verify", "--quiet", "HEAD^{commit}"}, Stdout: sha},
		runner.Call{Name: "git", Args: []string{"-C", "/repo", "log", "-1", "--format=%B", sha}, Stdout: probeMessage},
		runner.Call{Name: "git", Args: []string{"-C", "/repo", "rev-list", "--parents", "-n", "1", sha}, Stdout: sha + "\n"},
		runner.Call{Name: "entire", Args: []string{"checkpoint", "explain", "01M1TET4N33VMY0DTHNZKV5HT9", "--json"}, Stdout: explainJSON},
	)
	r := &Resolver{Run: f, Repo: "/repo"}

	got, err := r.Resolve(context.Background(), "HEAD")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Parent != "" {
		t.Errorf("Parent = %q, want empty for a root commit", got.Parent)
	}
}

func TestResolveCommitWithoutTrailerIsAnError(t *testing.T) {
	t.Parallel()
	sha := "458bb14287ec2e0dcd4b6c52a779c9213af5be52"
	f := runner.NewFake(
		runner.Call{Name: "git", Args: []string{"-C", "/repo", "rev-parse", "--verify", "--quiet", "HEAD^{commit}"}, Stdout: sha},
		runner.Call{Name: "git", Args: []string{"-C", "/repo", "log", "-1", "--format=%B", sha}, Stdout: "no trailer here\n"},
	)
	r := &Resolver{Run: f, Repo: "/repo"}

	_, err := r.Resolve(context.Background(), "HEAD")
	if err == nil {
		t.Fatal("Resolve() error = nil, want an error naming the missing trailer")
	}
	if !strings.Contains(err.Error(), TrailerKey) {
		t.Errorf("error %q does not name %s", err, TrailerKey)
	}
}

// Metadata is a convenience, not a requirement. A checkpoint that resolves
// from the trailer alone must still resolve when the metadata lookup fails,
// because missing data is a state and never an error.
func TestResolveToleratesMissingMetadata(t *testing.T) {
	t.Parallel()
	sha := "458bb14287ec2e0dcd4b6c52a779c9213af5be52"
	f := runner.NewFake(
		runner.Call{Name: "git", Args: []string{"-C", "/repo", "rev-parse", "--verify", "--quiet", "HEAD^{commit}"}, Stdout: sha},
		runner.Call{Name: "git", Args: []string{"-C", "/repo", "log", "-1", "--format=%B", sha}, Stdout: probeMessage},
		runner.Call{
			Name: "git", Args: []string{"-C", "/repo", "rev-list", "--parents", "-n", "1", sha},
			Stdout: sha + " 650a885fdf96dde7f230729ca19db0c8ea217f53\n",
		},
		runner.Call{
			Name: "entire", Args: []string{"checkpoint", "explain", "01M1TET4N33VMY0DTHNZKV5HT9", "--json"},
			Stderr: "checkpoint not found locally\n", Exit: 1,
		},
	)
	r := &Resolver{Run: f, Repo: "/repo"}

	got, err := r.Resolve(context.Background(), "HEAD")
	if err != nil {
		t.Fatalf("Resolve() error = %v, want success with a recorded note", err)
	}
	if got.MetadataErr == "" {
		t.Error("MetadataErr is empty, want the reason recorded")
	}
	if len(got.FilesTouched) != 0 {
		t.Errorf("FilesTouched = %v, want none", got.FilesTouched)
	}
}

func TestResolveRejectsUnknownReference(t *testing.T) {
	t.Parallel()
	f := runner.NewFake(
		runner.Call{Name: "git", Args: []string{"-C", "/repo", "rev-parse", "--verify", "--quiet", "not-a-ref^{commit}"}, Exit: 1},
	)
	r := &Resolver{Run: f, Repo: "/repo"}

	if _, err := r.Resolve(context.Background(), "not-a-ref"); err == nil {
		t.Fatal("Resolve() error = nil, want an error")
	}
}

func TestResolveRejectsEmptyReference(t *testing.T) {
	t.Parallel()
	r := &Resolver{Run: runner.NewFake(), Repo: "/repo"}
	if _, err := r.Resolve(context.Background(), "   "); err == nil {
		t.Fatal("Resolve() error = nil, want an error")
	}
}
