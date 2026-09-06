package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFormat(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"git", []string{"status"}, "git status"},
		{"git", nil, "git"},
		{"entire", []string{"graph", "impact", "--symbol", "compute_total"}, "entire graph impact --symbol compute_total"},
		// A test command carries spaces and must stay readable and quoted.
		{"entire", []string{"graph", "verify", "--test", "pytest -q"}, `entire graph verify --test "pytest -q"`},
		{"sh", []string{""}, `sh ""`},
	}
	for _, c := range cases {
		if got := Format(c.name, c.args); got != c.want {
			t.Errorf("Format(%q, %v) = %q, want %q", c.name, c.args, got, c.want)
		}
	}
}

// Exec is the one implementation that starts a process. These use only tools
// guaranteed on any POSIX machine, so no network and no Entire are involved.
func TestExecRunSuccess(t *testing.T) {
	t.Parallel()
	stdout, _, exit, err := Exec{}.Run(context.Background(), "echo", []string{"hello"}, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if exit != 0 {
		t.Errorf("exit = %d, want 0", exit)
	}
	if strings.TrimSpace(string(stdout)) != "hello" {
		t.Errorf("stdout = %q", stdout)
	}
}

// A non-zero exit is data, not an error. The verifiers depend on this: a
// failing test command is a finding, not a crash.
func TestExecRunNonZeroExitIsNotAnError(t *testing.T) {
	t.Parallel()
	_, _, exit, err := Exec{}.Run(context.Background(), "false", nil, nil)
	if err != nil {
		t.Fatalf("Run() error = %v, want nil for a non-zero exit", err)
	}
	if exit == 0 {
		t.Error("exit = 0, want non-zero")
	}
}

func TestExecRunStdin(t *testing.T) {
	t.Parallel()
	stdout, _, exit, err := Exec{}.Run(context.Background(), "cat", nil, []byte("piped"))
	if err != nil || exit != 0 {
		t.Fatalf("Run() error = %v, exit = %d", err, exit)
	}
	if string(stdout) != "piped" {
		t.Errorf("stdout = %q, want %q", stdout, "piped")
	}
}

func TestExecRunMissingExecutable(t *testing.T) {
	t.Parallel()
	_, _, _, err := Exec{}.Run(context.Background(), "impeach-no-such-binary", nil, nil)
	if err == nil {
		t.Fatal("Run() error = nil, want ErrNotFound")
	}
	if !strings.Contains(err.Error(), "executable not found") {
		t.Errorf("error = %v, want it to wrap ErrNotFound", err)
	}
}

func TestExecRunHonoursContextDeadline(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, exit, err := Exec{}.Run(ctx, "sleep", []string{"5"}, nil)
	if err == nil {
		t.Fatal("Run() error = nil, want a deadline error")
	}
	if exit != -1 {
		t.Errorf("exit = %d, want -1 for a timeout", exit)
	}
}

func TestLoggedRecordsCommandLines(t *testing.T) {
	t.Parallel()
	f := NewFake(Call{Name: "git", Args: []string{"status"}, Stdout: "clean"})
	l := &Logged{Inner: f}

	if _, _, _, err := l.Run(context.Background(), "git", []string{"status"}, nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	got := l.Calls()
	if len(got) != 1 || got[0] != "git status" {
		t.Errorf("Calls() = %v, want [git status]", got)
	}
	// Calls returns a copy, so a caller cannot mutate the log.
	got[0] = "tampered"
	if l.Calls()[0] != "git status" {
		t.Error("Calls() returned an aliased slice")
	}
}

func TestFakeMatchesOnNameAndArgs(t *testing.T) {
	t.Parallel()
	f := NewFake(
		Call{Name: "git", Args: []string{"rev-parse", "HEAD"}, Stdout: "abc123"},
		Call{Name: "git", Args: []string{"status"}, Stdout: "clean", Exit: 0},
	)
	stdout, _, _, err := f.Run(context.Background(), "git", []string{"rev-parse", "HEAD"}, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if string(stdout) != "abc123" {
		t.Errorf("stdout = %q", stdout)
	}
	if reqs := f.Requests(); len(reqs) != 1 || reqs[0] != "git rev-parse HEAD" {
		t.Errorf("Requests() = %v", reqs)
	}
}

func TestFakeStrictFailsOnUnrecordedCall(t *testing.T) {
	t.Parallel()
	f := NewFake()
	_, _, _, err := f.Run(context.Background(), "git", []string{"status"}, nil)
	if err == nil {
		t.Fatal("Run() error = nil, want an unrecorded-call error")
	}
	if !strings.Contains(err.Error(), "git status") {
		t.Errorf("error %q should name the missing call", err)
	}
}

// A verifier may ask the same question twice. The last recording is reused so
// the second answer matches the first, which keeps verdicts deterministic.
func TestFakeRepeatsLastRecording(t *testing.T) {
	t.Parallel()
	f := NewFake(Call{Name: "git", Args: []string{"status"}, Stdout: "clean"})
	for i := 0; i < 3; i++ {
		stdout, _, _, err := f.Run(context.Background(), "git", []string{"status"}, nil)
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if string(stdout) != "clean" {
			t.Errorf("call %d stdout = %q", i, stdout)
		}
	}
}

func TestFakeServesRepeatedCallsInOrder(t *testing.T) {
	t.Parallel()
	f := NewFake(
		Call{Name: "git", Args: []string{"status"}, Stdout: "first"},
		Call{Name: "git", Args: []string{"status"}, Stdout: "second"},
	)
	want := []string{"first", "second", "second"}
	for i, w := range want {
		stdout, _, _, err := f.Run(context.Background(), "git", []string{"status"}, nil)
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if string(stdout) != w {
			t.Errorf("call %d stdout = %q, want %q", i, stdout, w)
		}
	}
}

func TestHashStdin(t *testing.T) {
	t.Parallel()
	if got := HashStdin(nil); got != "" {
		t.Errorf("HashStdin(nil) = %q, want empty", got)
	}
	a := HashStdin([]byte("prompt"))
	if len(a) != 64 {
		t.Errorf("HashStdin() length = %d, want 64 hex chars", len(a))
	}
	if a == HashStdin([]byte("other")) {
		t.Error("different stdin hashed to the same value")
	}
}

// Recording and LoadFake are the two halves of `impeach record`, so they are
// tested as a round trip.
func TestRecordingRoundTripsThroughLoadFake(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	inner := NewFake(
		Call{Name: "git", Args: []string{"rev-parse", "HEAD"}, Stdout: "abc123\n"},
		Call{Name: "entire", Args: []string{"graph", "commit", "HEAD", "--json"}, Stdout: `{"files":[]}`},
	)
	rec := &Recording{Inner: inner, Dir: dir}

	if _, _, _, err := rec.Run(context.Background(), "git", []string{"rev-parse", "HEAD"}, nil); err != nil {
		t.Fatalf("record git: %v", err)
	}
	if _, _, _, err := rec.Run(context.Background(), "entire", []string{"graph", "commit", "HEAD", "--json"}, nil); err != nil {
		t.Fatalf("record entire: %v", err)
	}

	for _, n := range []string{"0.json", "1.json"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Fatalf("expected %s to be written: %v", n, err)
		}
	}

	replay, err := LoadFake(dir)
	if err != nil {
		t.Fatalf("LoadFake() error = %v", err)
	}
	stdout, _, _, err := replay.Run(context.Background(), "git", []string{"rev-parse", "HEAD"}, nil)
	if err != nil {
		t.Fatalf("replay error = %v", err)
	}
	if strings.TrimSpace(string(stdout)) != "abc123" {
		t.Errorf("replayed stdout = %q", stdout)
	}
}

// Fixtures are committed, so the recorder must be able to rewrite absolute
// paths out of both output and argv before anything lands on disk.
func TestRecordingScrubsOutputAndArgs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	inner := NewFake(Call{
		Name: "git", Args: []string{"-C", "/Users/someone/repo", "status"},
		Stdout: "on /Users/someone/repo\n",
	})
	rec := &Recording{
		Inner: inner,
		Dir:   dir,
		Scrub: func(s string) string { return strings.ReplaceAll(s, "/Users/someone/repo", "<repo>") },
	}
	if _, _, _, err := rec.Run(context.Background(), "git", []string{"-C", "/Users/someone/repo", "status"}, nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	blob, err := os.ReadFile(filepath.Join(dir, "0.json"))
	if err != nil {
		t.Fatalf("read recording: %v", err)
	}
	if strings.Contains(string(blob), "/Users/someone/repo") {
		t.Errorf("recording leaked an absolute path:\n%s", blob)
	}
	if !strings.Contains(string(blob), "<repo>") {
		t.Errorf("recording missing the scrub placeholder:\n%s", blob)
	}
}

func TestLoadFakeMissingDirectory(t *testing.T) {
	t.Parallel()
	if _, err := LoadFake(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("LoadFake() error = nil, want an error")
	}
}
