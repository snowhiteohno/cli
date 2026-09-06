package checkpoint

import (
	"context"
	"fmt"

	"github.com/entireio/cli/impeach/internal/runner"
)

// Testimony is a checkpoint's stored transcript plus which session it came
// from. A checkpoint can hold several sessions, so the reader records the one
// it read rather than leaving it implicit.
type Testimony struct {
	Raw []byte
	// SessionIndex is the 0-based index passed to the CLI, or -1 when the
	// default (latest) session was taken.
	SessionIndex int
	// Command is the exact command used, for the report's reproduction list.
	Command string
}

// FetchTranscript reads the stored transcript for a checkpoint.
//
// `--raw-transcript` is used rather than `--transcript` because the Step 0
// probe confirmed the two are byte-identical while checkpoints v1 is the
// store, and `--raw-transcript` is the older documented spelling. Either would
// do; this boundary owns the choice.
//
// A checkpoint with no stored transcript is not an error. It is a missing
// channel, and the caller turns it into unverifiable rows.
func (r *Resolver) FetchTranscript(ctx context.Context, id string, sessionIndex int) (*Testimony, error) {
	if id == "" {
		return nil, fmt.Errorf("no checkpoint id to read a transcript for")
	}
	args := []string{"checkpoint", "explain", id, "--raw-transcript"}
	if sessionIndex >= 0 {
		args = append(args, "--session-index", fmt.Sprintf("%d", sessionIndex))
	}

	ctx, cancel := context.WithTimeout(ctx, runner.TimeoutGraph)
	defer cancel()

	stdout, stderr, exit, err := r.Run.Run(ctx, "entire", args, nil)
	cmd := runner.Format("entire", args)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", cmd, err)
	}
	if exit != 0 {
		return nil, fmt.Errorf("%s exited %d: %s", cmd, exit, firstLine(stderr))
	}
	if len(stdout) == 0 {
		return nil, fmt.Errorf("%s returned no transcript", cmd)
	}
	return &Testimony{Raw: stdout, SessionIndex: sessionIndex, Command: cmd}, nil
}
