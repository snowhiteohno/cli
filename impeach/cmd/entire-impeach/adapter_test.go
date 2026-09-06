package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The adapter registry is the boundary that absorbs a change of agent, so its
// selection rules are worth pinning even while only one adapter exists.

func probeTranscript(t *testing.T) []byte {
	t.Helper()
	p := filepath.Join("..", "..", "testdata", "transcript", "claudecode_probe.jsonl")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Skipf("probe transcript not present: %v", err)
	}
	return raw
}

func TestPickAdapterAutoDetects(t *testing.T) {
	t.Parallel()
	a, err := pickAdapter("auto", "<repo>", probeTranscript(t))
	if err != nil {
		t.Fatalf("pickAdapter(auto) error = %v", err)
	}
	if a.Name() != "claude-code" {
		t.Errorf("adapter = %q", a.Name())
	}
}

// An explicit name skips detection, which matters when a transcript is
// recognised by more than one adapter or by none.
func TestPickAdapterExplicitSkipsDetection(t *testing.T) {
	t.Parallel()
	a, err := pickAdapter("claude-code", "<repo>", []byte("not a transcript at all"))
	if err != nil {
		t.Fatalf("pickAdapter(claude-code) error = %v", err)
	}
	if a.Name() != "claude-code" {
		t.Errorf("adapter = %q", a.Name())
	}
}

// With auto and nothing recognisable, the error names what was tried. The
// caller turns this into unverifiable rows rather than a crash, because a
// transcript nobody can read is a missing channel and missing data is a state.
func TestPickAdapterAutoWithNoMatchNamesWhatItTried(t *testing.T) {
	t.Parallel()
	_, err := pickAdapter("auto", "<repo>", []byte("this is not any agent's transcript"))
	if err == nil {
		t.Fatal("error = nil, want a no-adapter error")
	}
	if !strings.Contains(err.Error(), "claude-code") {
		t.Errorf("error = %v, want it to name the adapters tried", err)
	}
}

func TestPickAdapterUnknownName(t *testing.T) {
	t.Parallel()
	if _, err := pickAdapter("cursor", "<repo>", probeTranscript(t)); err == nil {
		t.Fatal("error = nil, want an error for an adapter that does not exist yet")
	}
}

// An unreadable transcript must not end the run. The record-side rows still
// have something to say.
func TestUnreadableTranscriptDegradesRatherThanFails(t *testing.T) {
	dir := scenario(t, "rerun-regression")
	stdout, _, code := runReplay(t,
		"01M1TET4N33VMY0DTHNZKV5HT9",
		"--repo", repoRoot(t),
		"--adapter", "claude-code",
		"--replay", dir,
	)
	if code != exitOK {
		t.Fatalf("exit = %d, want %d", code, exitOK)
	}
	// The report still renders, which is the point.
	if !strings.Contains(stdout, "Impeach") {
		t.Errorf("no report was produced:\n%s", stdout)
	}
}

func TestBadAdapterNameIsRejectedEarly(t *testing.T) {
	_, stderr, code := runReplay(t, "HEAD", "--adapter", "nonsense")
	if code != exitError {
		t.Errorf("exit = %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr, "unknown --adapter") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestModelTurnsFlagIsAccepted(t *testing.T) {
	_, stderr, code := runReplay(t, "--version", "--model-turns", "5")
	if code != exitOK {
		t.Errorf("exit = %d, stderr = %q", code, stderr)
	}
}
