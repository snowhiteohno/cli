package checkpoint

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/entireio/cli/impeach/internal/runner"
)

// listEntry is the shape of one element of `entire checkpoint list --json`, as
// observed by the Step 0 probe. Only the fields Impeach reads are named.
//
// Notably there is no commit sha here. The probe established that no
// checkpoint JSON carries one, which is why every resolution still goes
// through the commit trailer.
type listEntry struct {
	CheckpointID string   `json:"checkpoint_id"`
	SessionID    string   `json:"session_id"`
	SessionIDs   []string `json:"session_ids"`
	Agent        string   `json:"agent"`
	Date         string   `json:"date"`
	Message      string   `json:"message"`
}

// SessionCheckpoints lists the checkpoints belonging to a session, oldest
// first, so a session audit reads in the order the work happened.
//
// `--session` filters the list view, which is the documented way to ask this.
// The result is intersected with what the repository can actually resolve:
// a checkpoint whose commit is not present locally cannot be audited, and
// saying so is better than failing halfway through a session.
func (r *Resolver) SessionCheckpoints(ctx context.Context, sessionID string) ([]string, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("no session id to list checkpoints for")
	}

	ctx, cancel := context.WithTimeout(ctx, runner.TimeoutGraph)
	defer cancel()

	args := []string{"checkpoint", "list", "--json", "--session", sessionID}
	stdout, stderr, exit, err := r.Run.Run(ctx, "entire", args, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", runner.Format("entire", args), err)
	}
	if exit != 0 {
		return nil, fmt.Errorf("%s exited %d: %s", runner.Format("entire", args), exit, firstLine(stderr))
	}

	var entries []listEntry
	if err := json.Unmarshal(stdout, &entries); err != nil {
		return nil, fmt.Errorf("parse checkpoint list: %w", err)
	}

	// The CLI lists newest first; a session reads better oldest first.
	seen := map[string]bool{}
	ids := make([]string, 0, len(entries))
	for i := len(entries) - 1; i >= 0; i-- {
		id := entries[i].CheckpointID
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("session %s has no checkpoints in this repository", sessionID)
	}
	return ids, nil
}
