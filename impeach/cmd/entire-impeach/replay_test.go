package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These are the end-to-end tests, and they run the whole pipeline against
// recorded scenarios. No Entire, no git, no agent and no network is involved:
// every external call is served from fixtures/recorded/<scenario>/.
//
// That is the entire reason the Runner boundary carries no
// working-directory field. A recorded call is fully described by its name and
// argv, so replay is exact rather than approximate.

// scenario returns the path to a committed scenario, skipping if absent.
func scenario(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("..", "..", "fixtures", "recorded", name)
	if _, err := os.Stat(p); err != nil {
		t.Skipf("scenario %s not present: %v", name, err)
	}
	return p
}

// runReplay drives the real entry point and returns stdout, stderr and the
// exit code.
//
// It pins ENTIRE_PLUGIN_DATA_DIR to the layout the scenarios were recorded
// under. The recorded worktree paths live below that directory, and the
// scrubber rewrote the home prefix to <home>, so pinning the same relative
// layout under the current home makes the normalized keys match on any
// machine. Leaving it unset would fall back to ~/.cache and miss every
// worktree call.
func runReplay(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("resolve home: %v", err)
	}
	t.Setenv("ENTIRE_PLUGIN_DATA_DIR",
		filepath.Join(home, ".local", "share", "entire", "plugins", "data", "impeach"))
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return out.String(), errb.String(), code
}

// The demo checkpoint whose agent claim is contradicted by a fresh test run.
// This is the row the whole tool exists to produce.
func TestReplayRerunRegressionProducesTheImpeachedRow(t *testing.T) {
	dir := scenario(t, "rerun-regression")
	stdout, stderr, code := runReplay(t,
		"01M1TET4N33VMY0DTHNZKV5HT9",
		"--repo", repoRoot(t),
		"--test", recordedTestCommand,
		"--replay", dir,
	)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr: %s", code, stderr)
	}

	for _, want := range []string{
		"01M1TET4N33VMY0DTHNZKV5HT9",
		"impeached",
		"execution",
		"contradicted by rerun",
		"new failures",
		// The three genuine failures the rerun found.
		"tests/test_service.py::test_round_money_half_up",
		"tests/test_rounding.py::test_round_money_negative",
		"tests/test_rounding.py::test_round_money_two_places",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output missing %q\n---\n%s", want, stdout)
		}
	}
	if !strings.Contains(stdout, "1 impeached") {
		t.Errorf("expected exactly one impeachment in the counts\n---\n%s", stdout)
	}
}

// --fail-on turns the same run into a gate, which is the CI contract.
func TestReplayFailOnImpeachedExitsTwo(t *testing.T) {
	dir := scenario(t, "rerun-regression")
	_, stderr, code := runReplay(t,
		"01M1TET4N33VMY0DTHNZKV5HT9",
		"--repo", repoRoot(t),
		"--test", recordedTestCommand,
		"--fail-on", "impeached",
		"--replay", dir,
	)
	if code != exitFailOn {
		t.Fatalf("exit = %d, want %d\nstderr: %s", code, exitFailOn, stderr)
	}
}

// Without --fail-on the same impeachment is reported and the exit stays 0, so
// the report is not itself a failure.
func TestReplayWithoutFailOnExitsZero(t *testing.T) {
	dir := scenario(t, "rerun-regression")
	_, _, code := runReplay(t,
		"01M1TET4N33VMY0DTHNZKV5HT9",
		"--repo", repoRoot(t),
		"--test", recordedTestCommand,
		"--replay", dir,
	)
	if code != exitOK {
		t.Fatalf("exit = %d, want %d", code, exitOK)
	}
}

// The second demo checkpoint, where the agent was accurate. It yields the
// other three verdicts and the unrequested block.
func TestReplayDiscountRenameProducesTheOtherVerdicts(t *testing.T) {
	dir := scenario(t, "discount-rename")
	stdout, stderr, code := runReplay(t,
		"01M1TJCCYXR7ZZTK1H8167G249",
		"--repo", repoRoot(t),
		"--test", recordedTestCommand,
		"--replay", dir,
	)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr: %s", code, stderr)
	}
	for _, want := range []string{
		"01M1TJCCYXR7ZZTK1H8167G249",
		"corroborated",
		"uncorroborated",
		"unverifiable",
		"Unrequested changes",
		// The corpus is reported as a count, never reproduced.
		"prompt tokens for:",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output missing %q\n---\n%s", want, stdout)
		}
	}
}

// Replay must produce the documented JSON, and it must be valid.
func TestReplayWritesValidJSONReport(t *testing.T) {
	dir := scenario(t, "rerun-regression")
	out := t.TempDir()
	_, stderr, code := runReplay(t,
		"01M1TET4N33VMY0DTHNZKV5HT9",
		"--repo", repoRoot(t),
		"--test", recordedTestCommand,
		"--out", out,
		"--replay", dir,
	)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr: %s", code, stderr)
	}

	blob, err := os.ReadFile(filepath.Join(out, "impeach.json"))
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var doc struct {
		Version    string `json:"impeach_version"`
		Checkpoint struct {
			ID     string `json:"id"`
			Commit string `json:"commit"`
		} `json:"checkpoint"`
		Claims []struct {
			Verdict string   `json:"verdict"`
			Reasons []string `json:"reasons"`
			Rerun   string   `json:"rerun"`
		} `json:"claims"`
		Counts struct {
			Impeached int `json:"impeached"`
		} `json:"counts"`
		CommandsRun []string `json:"commands_run"`
	}
	if err := json.Unmarshal(blob, &doc); err != nil {
		t.Fatalf("report is not valid JSON: %v\n%s", err, blob)
	}
	if doc.Checkpoint.ID != "01M1TET4N33VMY0DTHNZKV5HT9" {
		t.Errorf("checkpoint.id = %q", doc.Checkpoint.ID)
	}
	if doc.Counts.Impeached != 1 {
		t.Errorf("counts.impeached = %d, want 1", doc.Counts.Impeached)
	}
	if len(doc.CommandsRun) == 0 {
		t.Error("commands_run is empty; a reader must be able to reproduce every line")
	}
	var found bool
	for _, c := range doc.Claims {
		for _, r := range c.Reasons {
			if r == "contradicted-rerun" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("no claim carries contradicted-rerun:\n%s", blob)
	}
}

// No transcript is ever written to --out, in whole or in part. What appears is
// a bounded, scrubbed excerpt of a command's own output.
func TestReplayJSONCarriesNoRawTranscript(t *testing.T) {
	dir := scenario(t, "rerun-regression")
	out := t.TempDir()
	if _, _, code := runReplay(t,
		"01M1TET4N33VMY0DTHNZKV5HT9",
		"--repo", repoRoot(t),
		"--test", recordedTestCommand,
		"--out", out,
		"--replay", dir,
	); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	blob, err := os.ReadFile(filepath.Join(out, "impeach.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(blob)
	// Shapes that only appear in a raw Claude Code transcript.
	for _, forbidden := range []string{
		`"type":"assistant"`, `"tool_use"`, `"toolUseResult"`,
		`"isSidechain"`, `"parentUuid"`, `"sessionId"`,
	} {
		if strings.Contains(s, forbidden) {
			t.Errorf("the report leaked transcript structure %q", forbidden)
		}
	}
}

// A scenario missing a call must fail loudly, not quietly succeed. A silent
// empty result would look like a channel that legitimately had nothing in it,
// which is exactly the confusion this tool exists to remove.
func TestReplayUnrecordedCallIsAnError(t *testing.T) {
	dir := scenario(t, "rerun-regression")
	_, stderr, code := runReplay(t,
		// A checkpoint the scenario knows nothing about.
		"01ZZZZZZZZZZZZZZZZZZZZZZZZ",
		"--repo", repoRoot(t),
		"--replay", dir,
	)
	if code != exitError {
		t.Fatalf("exit = %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr, "no recorded call") {
		t.Errorf("stderr should name the missing call, got: %s", stderr)
	}
}

func TestRecordAndReplayAreMutuallyExclusive(t *testing.T) {
	_, stderr, code := runReplay(t, "HEAD", "--record", "a", "--replay", "b")
	if code != exitError {
		t.Errorf("exit = %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr, "mutually exclusive") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestVersionFlag(t *testing.T) {
	stdout, _, code := runReplay(t, "--version")
	if code != exitOK {
		t.Errorf("exit = %d", code)
	}
	if !strings.Contains(stdout, Version) {
		t.Errorf("stdout = %q, want the version", stdout)
	}
}

func TestNoReferenceIsAnError(t *testing.T) {
	_, stderr, code := runReplay(t)
	if code != exitError {
		t.Errorf("exit = %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr, "no checkpoint or commit given") {
		t.Errorf("stderr = %q", stderr)
	}
}

// repoRoot is the repository the scenarios were recorded against. The
// scrubber rewrote it to <repo> in the recording, and replay normalizes the
// live key the same way, so the value only has to be the real root.
func repoRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	// The scenarios were recorded from the repository root, one level above
	// the impeach module.
	return filepath.Dir(abs)
}

// recordedTestCommand is the exact --test string the scenarios were recorded
// with. It has to match, because it appears inside the recorded argv of the
// graph verify calls.
const recordedTestCommand = `cd impeach/fixtures/app && { test -d .venv || { python3 -m venv .venv && ./.venv/bin/python -m pip install -q -r requirements.txt; }; } && ./.venv/bin/python -m pytest -q --tb=no -rA`
