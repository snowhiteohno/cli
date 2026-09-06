// Package runner is the single boundary between Impeach and every external
// process. Nothing in Impeach starts a process except through a Runner, which
// is what makes the recorded fixtures and the offline tests possible.
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// Runner executes one external command and reports what it wrote and how it
// exited. Implementations must not interpret the command; argv is passed
// through untouched.
//
// A non-zero exit is not an error. err is reserved for the process failing to
// run at all, or for the context expiring. Callers that care about failure
// check exit.
type Runner interface {
	Run(ctx context.Context, name string, args []string, stdin []byte) (stdout, stderr []byte, exit int, err error)
}

// Timeouts from the security policy. Each call site picks one; nothing runs
// unbounded.
const (
	// TimeoutGraph bounds a single Entire or Graph call.
	TimeoutGraph = 60 * time.Second
	// TimeoutGit bounds a single git call.
	TimeoutGit = 60 * time.Second
	// TimeoutRerun bounds the test rerun, which is the user's own command.
	TimeoutRerun = 10 * time.Minute
	// TimeoutModel bounds one turn through the opt-in model extractor.
	TimeoutModel = 120 * time.Second
)

// ErrNotFound reports that the executable is not on the path. Callers turn
// this into a missing-channel state rather than a crash, because a missing
// Entire CLI is a condition Impeach reports on.
var ErrNotFound = errors.New("executable not found")

// Exec runs commands for real. It is the only implementation that starts a
// process.
//
// There is deliberately no working-directory field. Every command Impeach runs
// takes its own path argument (git's -C, Graph's --repo), so a worktree is
// selected by argv and never by ambient state. That keeps a recorded call
// fully described by name and args.
type Exec struct{}

// Run implements Runner.
func (Exec) Run(ctx context.Context, name string, args []string, stdin []byte) ([]byte, []byte, int, error) {
	if _, err := exec.LookPath(name); err != nil {
		return nil, nil, -1, fmt.Errorf("%w: %s", ErrNotFound, name)
	}

	cmd := exec.CommandContext(ctx, name, args...)
	if len(stdin) > 0 {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb

	err := cmd.Run()
	switch {
	case err == nil:
		return out.Bytes(), errb.Bytes(), 0, nil
	case ctx.Err() != nil:
		// Report the timeout rather than the exit status it produced, so the
		// caller can say "this call timed out" instead of "it failed".
		return out.Bytes(), errb.Bytes(), -1, fmt.Errorf("%s: %w", name, ctx.Err())
	default:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return out.Bytes(), errb.Bytes(), ee.ExitCode(), nil
		}
		return out.Bytes(), errb.Bytes(), -1, fmt.Errorf("%s: %w", name, err)
	}
}

// Logged wraps a Runner and records every command line it was asked to run, in
// order. The report's commands_run list comes from here, so a reader can
// reproduce a verdict without trusting the report.
type Logged struct {
	Inner Runner
	calls []string
}

// Run implements Runner.
func (l *Logged) Run(ctx context.Context, name string, args []string, stdin []byte) ([]byte, []byte, int, error) {
	l.calls = append(l.calls, Format(name, args))
	return l.Inner.Run(ctx, name, args, stdin)
}

// Calls returns the command lines run so far, in order.
func (l *Logged) Calls() []string {
	out := make([]string, len(l.calls))
	copy(out, l.calls)
	return out
}

// Format renders a command for display. It is for humans and for the report's
// reproduction list; it is never parsed and never executed.
func Format(name string, args []string) string {
	var b bytes.Buffer
	b.WriteString(name)
	for _, a := range args {
		b.WriteByte(' ')
		if a == "" {
			b.WriteString(`""`)
			continue
		}
		if bytes.ContainsAny([]byte(a), " \t\"'\\$`") {
			b.WriteString(fmt.Sprintf("%q", a))
			continue
		}
		b.WriteString(a)
	}
	return b.String()
}
