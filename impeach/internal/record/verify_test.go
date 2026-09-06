package record

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entireio/cli/impeach/internal/runner"
)

// The regression verdict captured verbatim from the Step 0 probe. Three tests
// changed state, and verify exited 0 while saying so.
func TestParseVerifyRealRegression(t *testing.T) {
	t.Parallel()
	out := string(readTestdata(t, "verify", "verdict_regression.txt"))
	r := ParseVerify(out)

	if r.Status != RerunNewFailures {
		t.Errorf("Status = %q, want %q", r.Status, RerunNewFailures)
	}
	want := []string{
		"tests/test_rounding.py::test_round_money_negative",
		"tests/test_rounding.py::test_round_money_two_places",
		"tests/test_service.py::test_round_money_half_up",
	}
	if !reflect.DeepEqual(r.NewFailures, want) {
		t.Errorf("NewFailures = %v, want %v", r.NewFailures, want)
	}
	if !strings.Contains(r.Verdict, "REGRESSION") {
		t.Errorf("Verdict = %q, want it kept verbatim", r.Verdict)
	}
	if r.ExitCodeOnly {
		t.Error("ExitCodeOnly = true, want false for a parsed pytest run")
	}
}

// The degradation the probe found: pytest -q emits no per-test ids, so verify
// adjudicates on the exit code alone. It must be detected, because every
// document specifies exactly that command.
func TestParseVerifyDetectsExitCodeOnlyDegradation(t *testing.T) {
	t.Parallel()
	out := string(readTestdata(t, "verify", "baseline_recorded_exitcode_only.txt"))
	r := ParseVerify(out)
	if !r.ExitCodeOnly {
		t.Errorf("ExitCodeOnly = false for %q, want true", strings.TrimSpace(out))
	}
}

func TestParseVerifyRecognisesAParsedBaseline(t *testing.T) {
	t.Parallel()
	out := string(readTestdata(t, "verify", "baseline_recorded_pytest.txt"))
	r := ParseVerify(out)
	if r.ExitCodeOnly {
		t.Error("ExitCodeOnly = true, want false when the pytest parser engaged")
	}
}

func TestParseVerifyVariants(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		out         string
		wantStatus  RerunStatus
		wantFailing int
		wantPre     int
	}{
		{
			name:       "no change",
			out:        "VERDICT: NO CHANGE in test outcomes",
			wantStatus: RerunPass,
		},
		{
			name:       "regression verdict alone still counts",
			out:        "VERDICT: REGRESSION in 2 tests: a::b, c::d",
			wantStatus: RerunNewFailures,
		},
		{
			name:        "newly failing with pre-existing kept apart",
			out:         "NEWLY FAILING (1): a::b\nPRE-EXISTING (2): c::d, e::f\nVERDICT: REGRESSION in 1 test: a::b",
			wantStatus:  RerunNewFailures,
			wantFailing: 1,
			wantPre:     2,
		},
		{
			name:       "newly passing is not a regression",
			out:        "NEWLY PASSING (2): a::b, c::d\nVERDICT: IMPROVEMENT",
			wantStatus: RerunPass,
		},
		{
			name:       "empty output cannot adjudicate",
			out:        "",
			wantStatus: RerunSkipped,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r := ParseVerify(c.out)
			if r.Status != c.wantStatus {
				t.Errorf("Status = %q, want %q", r.Status, c.wantStatus)
			}
			if len(r.NewFailures) != c.wantFailing {
				t.Errorf("NewFailures = %v, want %d", r.NewFailures, c.wantFailing)
			}
			if len(r.PreExisting) != c.wantPre {
				t.Errorf("PreExisting = %v, want %d", r.PreExisting, c.wantPre)
			}
		})
	}
}

// A pre-existing failure must never be reported as one the checkpoint caused.
func TestParseVerifyKeepsPreExistingOutOfNewFailures(t *testing.T) {
	t.Parallel()
	r := ParseVerify("PRE-EXISTING (2): old::one, old::two\nVERDICT: NO CHANGE in test outcomes")
	if r.Status != RerunPass {
		t.Errorf("Status = %q, want pass; pre-existing failures are not the checkpoint's fault", r.Status)
	}
	if len(r.NewFailures) != 0 {
		t.Errorf("NewFailures = %v, want none", r.NewFailures)
	}
}

// verify caps its id lists and appends a note. The note must not become a
// test id.
func TestParseVerifyIgnoresTruncationNote(t *testing.T) {
	t.Parallel()
	r := ParseVerify("NEWLY FAILING (25): a::b, c::d, and 23 more\nVERDICT: REGRESSION in 25 tests")
	if !reflect.DeepEqual(r.NewFailures, []string{"a::b", "c::d"}) {
		t.Errorf("NewFailures = %v, want the two ids without the note", r.NewFailures)
	}
}

func TestVerifierNoTestCommandIsNotRun(t *testing.T) {
	t.Parallel()
	v := &Verifier{Runner: runner.NewFake()}
	r := v.Run(context.Background(), RerunOptions{})
	if r.Status != RerunNotRun {
		t.Errorf("Status = %q, want %q", r.Status, RerunNotRun)
	}
	if r.Reason == "" {
		t.Error("Reason is empty, want an explanation")
	}
}

// The full baseline flow: record on the parent, adjudicate the head against
// it. Without this a pre-existing failure would be blamed on the checkpoint.
func TestVerifierRecordsBaselineThenAdjudicates(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	baseline := filepath.Join(dir, "baseline.json")
	test := "cd app && pytest -v"

	f := runner.NewFake(
		runner.Call{
			Name:   "entire",
			Args:   []string{"graph", "verify", "--repo", "/wt/base", "--test", test, "--record-baseline", baseline},
			Stdout: "BASELINE RECORDED: " + baseline + " (pytest; 18 passing, 0 failing, exit 0)\n",
		},
		runner.Call{
			Name:   "entire",
			Args:   []string{"graph", "verify", "--repo", "/wt/head", "--test", test, "--pre-edit-baseline", baseline},
			Stdout: "NEWLY FAILING (1): tests/test_service.py::test_round_money_half_up\nVERDICT: REGRESSION in 1 test\n",
		},
	)
	v := &Verifier{Runner: f}

	r := v.Run(context.Background(), RerunOptions{
		Test: test, HeadRepo: "/wt/head", BaseRepo: "/wt/base", BaselinePath: baseline,
	})
	if r.Status != RerunNewFailures {
		t.Fatalf("Status = %q, want %q (reason: %s)", r.Status, RerunNewFailures, r.Reason)
	}
	if len(r.NewFailures) != 1 {
		t.Errorf("NewFailures = %v, want 1", r.NewFailures)
	}
	if len(r.Commands) != 2 {
		t.Errorf("Commands = %v, want both verify invocations recorded", r.Commands)
	}
	reqs := f.Requests()
	if len(reqs) != 2 || !strings.Contains(reqs[0], "--record-baseline") {
		t.Errorf("baseline was not recorded first: %v", reqs)
	}
}

// A root commit has no parent, so no baseline is possible. The rerun still
// happens and the report must say the result is a state, not a delta.
func TestVerifierWithoutBaselineSaysSo(t *testing.T) {
	t.Parallel()
	test := "pytest -v"
	f := runner.NewFake(runner.Call{
		Name:   "entire",
		Args:   []string{"graph", "verify", "--repo", "/wt/head", "--test", test},
		Stdout: "VERDICT: NO CHANGE in test outcomes\n",
	})
	v := &Verifier{Runner: f}

	r := v.Run(context.Background(), RerunOptions{Test: test, HeadRepo: "/wt/head"})
	if r.Status != RerunPass {
		t.Errorf("Status = %q, want pass", r.Status)
	}
	if !strings.Contains(r.Reason, "state rather than a delta") {
		t.Errorf("Reason = %q, want it to say no baseline was recorded", r.Reason)
	}
}

// A failure to record the baseline must not be silently ignored, because
// adjudicating without one changes what the result means.
func TestVerifierBaselineFailureIsReported(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	baseline := filepath.Join(dir, "baseline.json")
	test := "pytest -v"
	f := runner.NewFake(runner.Call{
		Name:   "entire",
		Args:   []string{"graph", "verify", "--repo", "/wt/base", "--test", test, "--record-baseline", baseline},
		Stderr: "setup failed\n", Exit: 1,
	})
	v := &Verifier{Runner: f}

	r := v.Run(context.Background(), RerunOptions{
		Test: test, HeadRepo: "/wt/head", BaseRepo: "/wt/base", BaselinePath: baseline,
	})
	if r.Status != RerunSkipped {
		t.Errorf("Status = %q, want %q", r.Status, RerunSkipped)
	}
	if !strings.Contains(r.Reason, "baseline") {
		t.Errorf("Reason = %q, want it to name the baseline", r.Reason)
	}
}

// Setup exists so a fresh worktree can build a virtualenv; its output never
// contributes test ids.
func TestVerifierPassesSetupCommand(t *testing.T) {
	t.Parallel()
	test := "pytest -v"
	setup := "python3 -m venv .venv"
	f := runner.NewFake(runner.Call{
		Name:   "entire",
		Args:   []string{"graph", "verify", "--repo", "/wt/head", "--test", test, "--setup", setup},
		Stdout: "VERDICT: NO CHANGE in test outcomes\n",
	})
	v := &Verifier{Runner: f}
	r := v.Run(context.Background(), RerunOptions{Test: test, Setup: setup, HeadRepo: "/wt/head"})
	if r.Status != RerunPass {
		t.Errorf("Status = %q (reason %s), want pass", r.Status, r.Reason)
	}
}

// The exit-code-only degradation observed while recording the baseline must
// reach the final result, since that is where a reader sees it.
func TestVerifierCarriesBaselineDegradationForward(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	baseline := filepath.Join(dir, "baseline.json")
	test := "pytest -q"
	f := runner.NewFake(
		runner.Call{
			Name:   "entire",
			Args:   []string{"graph", "verify", "--repo", "/wt/base", "--test", test, "--record-baseline", baseline},
			Stdout: "BASELINE RECORDED: " + baseline + " (exit 0; output format not recognised, so the baseline is exit-code only)\n",
		},
		runner.Call{
			Name:   "entire",
			Args:   []string{"graph", "verify", "--repo", "/wt/head", "--test", test, "--pre-edit-baseline", baseline},
			Stdout: "VERDICT: NO CHANGE in test outcomes\n",
		},
	)
	v := &Verifier{Runner: f}
	r := v.Run(context.Background(), RerunOptions{
		Test: test, HeadRepo: "/wt/head", BaseRepo: "/wt/base", BaselinePath: baseline,
	})
	if !r.ExitCodeOnly {
		t.Error("ExitCodeOnly = false, want the baseline degradation carried forward")
	}
	if r.Reason == "" {
		t.Error("Reason is empty, want the degradation explained")
	}
}

// verify exits 0 even on a regression, which the probe confirmed. A non-zero
// exit with no output is a real failure and must be reported as skipped.
func TestVerifierNonZeroExitWithNoOutputIsSkipped(t *testing.T) {
	t.Parallel()
	test := "pytest -v"
	f := runner.NewFake(runner.Call{
		Name:   "entire",
		Args:   []string{"graph", "verify", "--repo", "/wt/head", "--test", test},
		Stderr: "graph: plugin not installed\n", Exit: 127,
	})
	v := &Verifier{Runner: f}
	r := v.Run(context.Background(), RerunOptions{Test: test, HeadRepo: "/wt/head"})
	if r.Status != RerunSkipped {
		t.Errorf("Status = %q, want %q", r.Status, RerunSkipped)
	}
}

func TestBaselineFixturesAreRealJSON(t *testing.T) {
	t.Parallel()
	// The two baseline shapes the probe recorded, kept so a format change is
	// visible. The pytest one carries per-test results; the degraded one does
	// not.
	green := readTestdata(t, "verify", "baseline_pytest_green.json")
	if !strings.Contains(string(green), `"parser": "pytest"`) {
		t.Error("green baseline should record the pytest parser")
	}
	if !strings.Contains(string(green), "tests/test_api.py::") {
		t.Error("green baseline should carry per-test ids")
	}
	degraded := readTestdata(t, "verify", "baseline_exitcode_only.json")
	if !strings.Contains(string(degraded), `"parser": "exit-code-only"`) {
		t.Error("degraded baseline should record the exit-code-only parser")
	}
}

func TestDataDirIsCreatableForBaselines(t *testing.T) {
	t.Parallel()
	// The plugin data dir is not pre-created by the dispatcher, so anything
	// writing into it must be able to make it.
	dir := filepath.Join(t.TempDir(), "nested", "impeach")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("stat: %v", err)
	}
}

// A failing runner must never be reported as a pass. verify answers a command
// that could not run with a FAIL verdict and no ids, which says nothing about
// the tests, so the only honest status is skipped.
func TestStatusFromVerdictNeverCallsAFailureAPass(t *testing.T) {
	t.Parallel()
	cases := []struct {
		verdict string
		want    RerunStatus
	}{
		{"REGRESSION in 3 tests: a::b", RerunNewFailures},
		{"NO CHANGE in test outcomes", RerunPass},
		{"IMPROVEMENT", RerunPass},
		{"FAIL (exit 127)", RerunSkipped},
		{"ERROR: could not run", RerunSkipped},
		{"something unrecognised", RerunPass},
	}
	for _, c := range cases {
		got := statusFromVerdict(c.verdict, RerunPass)
		if got != c.want {
			t.Errorf("statusFromVerdict(%q) = %q, want %q", c.verdict, got, c.want)
		}
	}
}

// The exact shape the real run produced when the test command could not run
// in the worktree. Before this was fixed it reported pass.
func TestParseVerifyFailingRunnerIsNotAPass(t *testing.T) {
	t.Parallel()
	r := ParseVerify("BASELINE RECORDED: /tmp/b.json (exit 127; output format not recognised, so the baseline is exit-code only)\nVERDICT: FAIL (exit 127)\n")
	if r.Status == RerunPass {
		t.Fatal("Status = pass for a runner that exited 127; that would corroborate a claim on no evidence")
	}
	if r.Status != RerunSkipped {
		t.Errorf("Status = %q, want %q", r.Status, RerunSkipped)
	}
	if !r.ExitCodeOnly {
		t.Error("ExitCodeOnly = false, want true")
	}
}

// An explicit NEWLY FAILING list must not be downgraded by a later verdict
// line that happens to contain the word fail.
func TestNewlyFailingListWinsOverVerdictWording(t *testing.T) {
	t.Parallel()
	r := ParseVerify("NEWLY FAILING (1): a::b\nVERDICT: 1 test failing\n")
	if r.Status != RerunNewFailures {
		t.Errorf("Status = %q, want %q", r.Status, RerunNewFailures)
	}
}
