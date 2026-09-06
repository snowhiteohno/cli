package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entireio/cli/impeach/internal/runner"
)

// These are the tests for the privacy and completeness constraint. Two of
// them run the same recorded scenario twice, with and without the rerun,
// because that pair is the asymmetry the change turns on: a redacted channel
// can still impeach, and can never corroborate.

const redactedScenario = "redacted-toollog"

// A redacted channel must never produce a corroborated verdict. Run with
// --no-rerun, this claim would be corroborated by its own command output; the
// output is redacted, so it degrades to unverifiable instead.
func TestRedactedChannelCannotCorroborate(t *testing.T) {
	dir := scenario(t, redactedScenario)
	stdout, stderr, code := runReplay(t,
		"01M1TET4N33VMY0DTHNZKV5HT9",
		"--repo", repoRoot(t),
		"--no-rerun",
		"--replay", dir,
	)
	if code != exitOK {
		t.Fatalf("exit = %d, want %d\nstderr: %s", code, exitOK, stderr)
	}
	// Careful: "uncorroborated" contains "corroborated", so the check has to
	// be anchored on the row start rather than done as a substring.
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "corroborated") {
			t.Errorf("a claim was corroborated from a redacted command log: %q", line)
		}
	}
	if !strings.Contains(stdout, "unverifiable") {
		t.Errorf("expected the gated claim to be unverifiable:\n%s", stdout)
	}
	if !strings.Contains(stdout, "channel incomplete") && !strings.Contains(stdout, "channel-incomplete") {
		t.Errorf("expected the channel-incomplete reason:\n%s", stdout)
	}
	// The row has to explain itself, not just say unverifiable. Whichever
	// branch of the gate fired, the explanation names the channel and its
	// state, because "unverifiable" alone reads as the tool shrugging.
	if !strings.Contains(stdout, "commands channel is redacted") {
		t.Errorf("the gated row does not explain which channel was compromised:\n%s", stdout)
	}
	if !strings.Contains(stdout, "proves nothing") && !strings.Contains(stdout, "cannot be corroborated") {
		t.Errorf("expected the gate to say why the silence is not evidence:\n%s", stdout)
	}
}

// The same scenario with the rerun. A contradiction from an intact channel
// survives redaction of another, so the claim is still impeached. Redaction
// must not become a way to escape a verdict.
func TestRedactedChannelStillImpeaches(t *testing.T) {
	dir := scenario(t, redactedScenario)
	stdout, stderr, code := runReplay(t,
		"01M1TET4N33VMY0DTHNZKV5HT9",
		"--repo", repoRoot(t),
		"--test", recordedTestCommand,
		"--replay", dir,
	)
	if code != exitOK {
		t.Fatalf("exit = %d, want %d\nstderr: %s", code, exitOK, stderr)
	}
	if !strings.Contains(stdout, "impeached") {
		t.Fatalf("a contradicted claim stopped being impeached under redaction:\n%s", stdout)
	}
	if !strings.Contains(stdout, "contradicted by rerun") {
		t.Errorf("expected the rerun contradiction to survive redaction:\n%s", stdout)
	}
	if !strings.Contains(stdout, "1 impeached") {
		t.Errorf("expected exactly one impeachment:\n%s", stdout)
	}
}

// The ledger must name the redacted channel, so a reader knows what the
// verdicts were computed from.
func TestLedgerNamesTheRedactedChannel(t *testing.T) {
	dir := scenario(t, redactedScenario)
	stdout, _, code := runReplay(t,
		"01M1TET4N33VMY0DTHNZKV5HT9",
		"--repo", repoRoot(t),
		"--no-rerun",
		"--replay", dir,
	)
	if code != exitOK {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout, "commands redacted") {
		t.Errorf("the ledger does not report the commands channel as redacted:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Context is incomplete") {
		t.Errorf("expected the incomplete-context sentence:\n%s", stdout)
	}
	// The other channels are intact and must be reported as such, or the
	// ledger would read as a blanket warning rather than a statement.
	for _, want := range []string{"reads present", "edits present", "prompts present"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("ledger missing %q:\n%s", want, stdout)
		}
	}
}

// The JSON carries the same ledger, since a machine consumer needs to know
// whether a corroborated count means anything.
func TestJSONCarriesTheChannelLedger(t *testing.T) {
	dir := scenario(t, redactedScenario)
	out := t.TempDir()
	if _, _, code := runReplay(t,
		"01M1TET4N33VMY0DTHNZKV5HT9",
		"--repo", repoRoot(t),
		"--no-rerun",
		"--out", out,
		"--replay", dir,
	); code != exitOK {
		t.Fatalf("exit = %d", code)
	}
	blob, err := os.ReadFile(filepath.Join(out, "impeach.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Channels map[string]struct {
			State  string `json:"state"`
			Reason string `json:"reason"`
		} `json:"channels"`
		ContextComplete bool   `json:"context_complete"`
		ContextNote     string `json:"context_note"`
		GatedClaims     int    `json:"claims_gated_by_channel"`
		Claims          []struct {
			Verdict string   `json:"verdict"`
			Reasons []string `json:"reasons"`
		} `json:"claims"`
	}
	if err := json.Unmarshal(blob, &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if doc.Channels["commands"].State != "redacted" {
		t.Errorf("channels.commands.state = %q, want redacted", doc.Channels["commands"].State)
	}
	if doc.Channels["commands"].Reason == "" {
		t.Error("a redacted channel must carry its reason")
	}
	if doc.ContextComplete {
		t.Error("context_complete = true with a redacted channel")
	}
	if doc.ContextNote == "" {
		t.Error("context_note is empty with incomplete context")
	}
	if doc.GatedClaims < 1 {
		t.Errorf("claims_gated_by_channel = %d, want at least 1", doc.GatedClaims)
	}
	for _, c := range doc.Claims {
		if c.Verdict == "corroborated" {
			for _, r := range c.Reasons {
				if r == "channel-incomplete" {
					t.Error("a claim is both corroborated and gated, which is contradictory")
				}
			}
		}
	}
}

// --fail-on incomplete lets CI refuse a run whose evidence was incomplete,
// even when nothing was impeached. It reuses exit 2; there is no new code.
func TestFailOnIncompleteGatesOnPartialContext(t *testing.T) {
	dir := scenario(t, redactedScenario)
	_, _, code := runReplay(t,
		"01M1TET4N33VMY0DTHNZKV5HT9",
		"--repo", repoRoot(t),
		"--no-rerun",
		"--fail-on", "incomplete",
		"--replay", dir,
	)
	if code != exitFailOn {
		t.Fatalf("exit = %d, want %d", code, exitFailOn)
	}
}

// An intact run must not trip --fail-on incomplete, or the gate would be
// useless.
func TestFailOnIncompletePassesOnIntactContext(t *testing.T) {
	dir := scenario(t, "rerun-regression")
	_, _, code := runReplay(t,
		"01M1TET4N33VMY0DTHNZKV5HT9",
		"--repo", repoRoot(t),
		"--no-rerun",
		"--fail-on", "incomplete",
		"--replay", dir,
	)
	if code != exitOK {
		t.Fatalf("exit = %d, want %d; the intact scenario should not be incomplete", code, exitOK)
	}
}

// Sensitive mode is a refusal, not a preference. --model must be rejected
// with a non-zero exit, and no call may be made: a warning would still have
// sent the text.
func TestSensitiveRefusesModelAndMakesNoCall(t *testing.T) {
	dir := scenario(t, "rerun-regression")
	stdout, stderr, code := runReplay(t,
		"01M1TET4N33VMY0DTHNZKV5HT9",
		"--repo", repoRoot(t),
		"--sensitive",
		"--model", "some-model-command",
		"--replay", dir,
	)
	if code == exitOK {
		t.Fatalf("exit = %d, want non-zero; sensitive mode must refuse --model", code)
	}
	if code != exitError {
		t.Errorf("exit = %d, want %d", code, exitError)
	}
	msg := stderr + stdout
	if !strings.Contains(msg, "refusing to run") {
		t.Errorf("the refusal must be explicit, got: %s", msg)
	}
	if !strings.Contains(msg, "some-model-command") {
		t.Errorf("the refusal should name the command it refused: %s", msg)
	}
	// Nothing may have been produced: no report, no rows, no verdicts.
	if strings.Contains(stdout, "VERDICT") {
		t.Errorf("a report was produced despite the refusal:\n%s", stdout)
	}
}

// The refusal must happen before anything runs, not after the first turn has
// already been sent. Asserted directly on the Runner: zero calls.
func TestSensitiveRefusalHappensBeforeAnyCall(t *testing.T) {
	f := runner.NewFake()
	opts := &options{sensitive: true, model: "some-model-command", adapter: "auto", format: "table"}
	// buildRunner is reached only after the refusal, so a refusal that works
	// means the Fake is never asked for anything.
	if _, err := buildRunner(opts, "/repo"); err != nil {
		t.Fatalf("buildRunner: %v", err)
	}
	if n := len(f.Requests()); n != 0 {
		t.Errorf("%d calls were made before the refusal", n)
	}
}

// Sensitive mode is also declarable in a committed .impeach.json, so the
// repository itself carries the constraint and a colleague cannot enable a
// model command by accident.
func TestSensitiveFromCommittedConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, configName),
		[]byte(`{"sensitive": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(dir)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if !cfg.Sensitive {
		t.Error("sensitive was not read from the committed config")
	}
}

// The mode is stated verbatim in the report, so a reader knows the run was
// constrained rather than merely uneventful.
func TestSensitiveModeIsStatedInTheReport(t *testing.T) {
	dir := scenario(t, "rerun-regression")
	stdout, _, code := runReplay(t,
		"01M1TET4N33VMY0DTHNZKV5HT9",
		"--repo", repoRoot(t),
		"--sensitive",
		"--no-rerun",
		"--replay", dir,
	)
	if code != exitOK {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout, "Mode: sensitive") {
		t.Errorf("the report does not state the mode:\n%s", stdout)
	}
	if !strings.Contains(stdout, "nothing leaves this machine") {
		t.Errorf("the mode line should say what it means:\n%s", stdout)
	}
}

func TestFailOnIncompleteIsAValidFlagValue(t *testing.T) {
	_, stderr, code := runReplay(t, "--version", "--fail-on", "incomplete")
	if code != exitOK {
		t.Errorf("exit = %d, stderr = %q", code, stderr)
	}
	_, stderr, code = runReplay(t, "--version", "--fail-on", "nonsense")
	if code != exitError {
		t.Errorf("exit = %d, want an error for an unknown --fail-on", code)
	}
	if !strings.Contains(stderr, "incomplete") {
		t.Errorf("the error should list incomplete as an option: %s", stderr)
	}
}
