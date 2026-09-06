package record

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/entireio/cli/impeach/internal/runner"
)

// RerunStatus is what a rerun showed.
type RerunStatus string

const (
	// RerunPass means nothing changed state for the worse.
	RerunPass RerunStatus = "pass"
	// RerunNewFailures means tests that passed on the parent now fail.
	RerunNewFailures RerunStatus = "new_failures"
	// RerunNotRun means no rerun was attempted.
	RerunNotRun RerunStatus = "not_run"
	// RerunSkipped means a rerun was attempted and could not produce a
	// verdict, for instance because verify itself failed.
	RerunSkipped RerunStatus = "skipped"
)

// Rerun is the adjudicated result of re-running the user's test command.
type Rerun struct {
	Status RerunStatus
	// NewFailures are the test ids that changed from passing to failing.
	NewFailures []string
	// NewPasses are the ids that changed the other way.
	NewPasses []string
	// PreExisting are ids that were already failing before the change, which
	// must never be blamed on the checkpoint.
	PreExisting []string
	// Verdict is the verdict clause verify printed, kept verbatim.
	Verdict string
	// ExitCodeOnly records the degradation the Step 0 probe found: verify's
	// parser falls back to exit codes when the test command emits no per-test
	// ids, so there are no ids to report. `pytest -q` does exactly this.
	ExitCodeOnly bool
	// Adjudicated is true when verify actually returned a verdict. It is the
	// only reliable success signal: the Step 0 probe confirmed verify exits 0
	// even when it reports a regression, so the exit code says nothing, and
	// stderr can carry a note even on a good run.
	Adjudicated bool
	// Reason explains a not_run or skipped status.
	Reason string
	// Commands are the exact verify invocations, for the reproduction list.
	Commands []string
}

var (
	newlyFailingRe = regexp.MustCompile(`(?i)^NEWLY FAILING\s*\((\d+)\):\s*(.*)$`)
	newlyPassingRe = regexp.MustCompile(`(?i)^NEWLY PASSING\s*\((\d+)\):\s*(.*)$`)
	preExistingRe  = regexp.MustCompile(`(?i)^PRE-EXISTING\s*(?:FAILING)?\s*\((\d+)\):\s*(.*)$`)
	verdictRe      = regexp.MustCompile(`(?i)^VERDICT:\s*(.*)$`)
	baselineRe     = regexp.MustCompile(`(?i)^BASELINE RECORDED:\s*(.*)$`)
)

// ParseVerify parses `entire graph verify`'s adjudicated text.
//
// A text parser is unavoidable here: the Step 0 probe confirmed verify has
// neither --json nor --format, and that it exits 0 even when it reports a
// regression. So the text is the only carrier of the verdict, and the exit
// code must not be trusted to signal one.
func ParseVerify(out string) *Rerun {
	r := &Rerun{Status: RerunPass}
	sawVerdict := false

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch {
		case baselineRe.MatchString(line):
			r.Adjudicated = true
		case newlyFailingRe.MatchString(line):
			m := newlyFailingRe.FindStringSubmatch(line)
			r.NewFailures = splitIDs(m[2])
			if n, err := strconv.Atoi(m[1]); err == nil && n > 0 && len(r.NewFailures) == 0 {
				// verify caps id lists and reports a count. A count with no
				// ids still means a regression.
				r.NewFailures = nil
			}
			r.Status = RerunNewFailures
			r.Adjudicated = true
		case newlyPassingRe.MatchString(line):
			r.NewPasses = splitIDs(newlyPassingRe.FindStringSubmatch(line)[2])
			r.Adjudicated = true
		case preExistingRe.MatchString(line):
			r.PreExisting = splitIDs(preExistingRe.FindStringSubmatch(line)[2])
			r.Adjudicated = true
		case verdictRe.MatchString(line):
			r.Verdict = strings.TrimSpace(verdictRe.FindStringSubmatch(line)[1])
			sawVerdict = true
			r.Adjudicated = true
			r.Status = statusFromVerdict(r.Verdict, r.Status)
		}
		if strings.Contains(strings.ToLower(line), "exit-code only") ||
			strings.Contains(strings.ToLower(line), "exit code only") ||
			strings.Contains(strings.ToLower(line), "output format not recognised") ||
			strings.Contains(strings.ToLower(line), "output format not recognized") {
			r.ExitCodeOnly = true
		}
	}

	if !sawVerdict && !r.Adjudicated {
		r.Status = RerunSkipped
		r.Reason = "entire graph verify returned no verdict"
	}
	return r
}

var (
	regressionRe = regexp.MustCompile(`(?i)\bREGRESSION\b`)
	goodRe       = regexp.MustCompile(`(?i)\b(NO CHANGE|IMPROVEMENT|ALL PASS|PASSED|PASSING|OK)\b`)
	badRe        = regexp.MustCompile(`(?i)\b(FAIL|FAILED|FAILING|FAILURE|ERROR|BROKEN|CRASH)\b`)
)

// statusFromVerdict maps verify's verdict clause onto a rerun status.
//
// The order matters. A regression is the strongest signal and wins, and an
// explicit NEWLY FAILING list already seen is never downgraded. A clearly
// good verdict is a pass. Anything saying the run failed is deliberately
// neither: with no baseline delta and no test ids, a failing command cannot
// tell a broken suite apart from a broken runner, and reporting either would
// be a claim the evidence does not carry. The honest answer is that the rerun
// could not adjudicate.
func statusFromVerdict(verdict string, current RerunStatus) RerunStatus {
	switch {
	case regressionRe.MatchString(verdict):
		return RerunNewFailures
	case current == RerunNewFailures:
		return RerunNewFailures
	case goodRe.MatchString(verdict) && !badRe.MatchString(verdict):
		return RerunPass
	case badRe.MatchString(verdict):
		return RerunSkipped
	default:
		return current
	}
}

func splitIDs(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		// verify caps its lists and appends a note such as "and 4 more".
		if part == "" || strings.HasPrefix(strings.ToLower(part), "and ") {
			continue
		}
		out = append(out, part)
	}
	return out
}

// Verifier runs the rerun through `entire graph verify`.
type Verifier struct {
	Runner runner.Runner
}

// RerunOptions describes one rerun.
type RerunOptions struct {
	// Test is the user's command. It is the only command Impeach runs on the
	// user's behalf, and it comes from a flag or a committed .impeach.json,
	// never from a transcript.
	Test string
	// Setup runs before the tests in each worktree. Its output never
	// contributes test ids, which is how a fresh worktree gets a virtualenv.
	Setup string
	// HeadRepo is the worktree at the checkpoint's commit.
	HeadRepo string
	// BaseRepo is the worktree at the parent. Empty means no baseline is
	// possible, so the result is a state rather than a delta.
	BaseRepo string
	// BaselinePath is where the recorded baseline is written.
	BaselinePath string
}

// Run records a baseline on the parent worktree and adjudicates the head
// worktree against it.
//
// Without a baseline a pre-existing failure cannot be told apart from one the
// checkpoint caused, so the baseline is recorded first whenever a parent
// exists. When it does not, the run still happens and the report says the
// result is a state rather than a delta.
func (v *Verifier) Run(ctx context.Context, opts RerunOptions) *Rerun {
	if strings.TrimSpace(opts.Test) == "" {
		return &Rerun{Status: RerunNotRun, Reason: "no test command; pass --test or commit an .impeach.json"}
	}

	var commands []string
	haveBaseline := false
	baselineExitCodeOnly := false

	if opts.BaseRepo != "" && opts.BaselinePath != "" {
		if err := os.MkdirAll(filepath.Dir(opts.BaselinePath), 0o755); err == nil {
			args := v.args(opts.BaseRepo, opts, "--record-baseline", opts.BaselinePath)
			commands = append(commands, runner.Format("entire", args))
			out, stderr, exit, err := v.call(ctx, args)
			switch {
			case err != nil:
				return &Rerun{Status: RerunSkipped, Commands: commands,
					Reason: fmt.Sprintf("recording the baseline failed: %v", err)}
			case exit != 0:
				return &Rerun{Status: RerunSkipped, Commands: commands,
					Reason: fmt.Sprintf("recording the baseline exited %d: %s", exit, firstLine(stderr))}
			default:
				haveBaseline = true
				// The BASELINE RECORDED line is where the parser degradation
				// shows up first, so it is carried forward to the verdict.
				baselineExitCodeOnly = ParseVerify(out).ExitCodeOnly
			}
		}
	}

	args := v.args(opts.HeadRepo, opts)
	if haveBaseline {
		args = append(args, "--pre-edit-baseline", opts.BaselinePath)
	}
	commands = append(commands, runner.Format("entire", args))

	out, stderr, exit, err := v.call(ctx, args)
	if err != nil {
		return &Rerun{Status: RerunSkipped, Commands: commands,
			Reason: fmt.Sprintf("the rerun failed to run: %v", err)}
	}
	r := ParseVerify(out)
	if exit != 0 && !r.Adjudicated {
		return &Rerun{Status: RerunSkipped, Commands: commands,
			Reason: fmt.Sprintf("entire graph verify exited %d: %s", exit, firstLine(stderr))}
	}
	r.Commands = commands
	r.ExitCodeOnly = r.ExitCodeOnly || baselineExitCodeOnly
	if !haveBaseline {
		r.Reason = strings.TrimSpace(r.Reason + " no baseline was recorded, so this is a state rather than a delta")
	}
	if r.ExitCodeOnly && r.Reason == "" {
		r.Reason = "the test command emits no per-test ids, so verify adjudicated on the exit code alone"
	}
	if r.Status == RerunSkipped && r.Verdict != "" {
		r.Reason = strings.TrimSpace(fmt.Sprintf("the rerun could not be attributed: verify said %q. %s", r.Verdict, r.Reason))
	}
	return r
}

func (v *Verifier) args(repo string, opts RerunOptions, extra ...string) []string {
	args := []string{"graph", "verify", "--repo", repo, "--test", opts.Test}
	if strings.TrimSpace(opts.Setup) != "" {
		args = append(args, "--setup", opts.Setup)
	}
	return append(args, extra...)
}

func (v *Verifier) call(ctx context.Context, args []string) (string, []byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, runner.TimeoutRerun)
	defer cancel()
	stdout, stderr, exit, err := v.Runner.Run(ctx, "entire", args, nil)
	// verify writes its verdict to stdout, but a degradation note can land on
	// stderr, so both are parsed.
	return string(stdout) + "\n" + string(stderr), stderr, exit, err
}
