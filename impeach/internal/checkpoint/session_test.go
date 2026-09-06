package checkpoint

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/entireio/cli/impeach/internal/runner"
)

// The list view is newest first; a session reads better oldest first, because
// that is the order the work happened in.
func TestSessionCheckpointsAreOldestFirst(t *testing.T) {
	t.Parallel()
	const session = "1c439358-7307-4721-8422-949e897dbfba"
	f := runner.NewFake(runner.Call{
		Name: "entire",
		Args: []string{"checkpoint", "list", "--json", "--session", session},
		Stdout: `[
		  {"checkpoint_id":"01CCCCCCCCCCCCCCCCCCCCCCCC","session_id":"` + session + `","message":"third"},
		  {"checkpoint_id":"01BBBBBBBBBBBBBBBBBBBBBBBB","session_id":"` + session + `","message":"second"},
		  {"checkpoint_id":"01AAAAAAAAAAAAAAAAAAAAAAAA","session_id":"` + session + `","message":"first"}
		]`,
	})
	r := &Resolver{Run: f, Repo: "/repo"}

	got, err := r.SessionCheckpoints(context.Background(), session)
	if err != nil {
		t.Fatalf("SessionCheckpoints() error = %v", err)
	}
	want := []string{
		"01AAAAAAAAAAAAAAAAAAAAAAAA",
		"01BBBBBBBBBBBBBBBBBBBBBBBB",
		"01CCCCCCCCCCCCCCCCCCCCCCCC",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SessionCheckpoints() = %v, want %v", got, want)
	}
}

func TestSessionCheckpointsDeduplicates(t *testing.T) {
	t.Parallel()
	f := runner.NewFake(runner.Call{
		Name: "entire",
		Args: []string{"checkpoint", "list", "--json", "--session", "s"},
		Stdout: `[{"checkpoint_id":"01AAAAAAAAAAAAAAAAAAAAAAAA"},
		          {"checkpoint_id":"01AAAAAAAAAAAAAAAAAAAAAAAA"}]`,
	})
	r := &Resolver{Run: f, Repo: "/repo"}
	got, err := r.SessionCheckpoints(context.Background(), "s")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("SessionCheckpoints() = %v, want one entry", got)
	}
}

func TestSessionCheckpointsEmptyIsAnError(t *testing.T) {
	t.Parallel()
	f := runner.NewFake(runner.Call{
		Name:   "entire",
		Args:   []string{"checkpoint", "list", "--json", "--session", "s"},
		Stdout: `[]`,
	})
	r := &Resolver{Run: f, Repo: "/repo"}
	_, err := r.SessionCheckpoints(context.Background(), "s")
	if err == nil {
		t.Fatal("error = nil, want an error naming the empty session")
	}
	if !strings.Contains(err.Error(), "no checkpoints") {
		t.Errorf("error = %v", err)
	}
}

func TestSessionCheckpointsRequiresASession(t *testing.T) {
	t.Parallel()
	r := &Resolver{Run: runner.NewFake(), Repo: "/repo"}
	if _, err := r.SessionCheckpoints(context.Background(), ""); err == nil {
		t.Fatal("error = nil, want an error")
	}
}

func TestSessionCheckpointsSurfacesCLIFailure(t *testing.T) {
	t.Parallel()
	f := runner.NewFake(runner.Call{
		Name:   "entire",
		Args:   []string{"checkpoint", "list", "--json", "--session", "s"},
		Stderr: "not logged in\n", Exit: 1,
	})
	r := &Resolver{Run: f, Repo: "/repo"}
	_, err := r.SessionCheckpoints(context.Background(), "s")
	if err == nil {
		t.Fatal("error = nil, want the CLI failure surfaced")
	}
	if !strings.Contains(err.Error(), "not logged in") {
		t.Errorf("error = %v, want the CLI's own message", err)
	}
}

func TestSessionCheckpointsRejectsGarbage(t *testing.T) {
	t.Parallel()
	f := runner.NewFake(runner.Call{
		Name:   "entire",
		Args:   []string{"checkpoint", "list", "--json", "--session", "s"},
		Stdout: "not json",
	})
	r := &Resolver{Run: f, Repo: "/repo"}
	if _, err := r.SessionCheckpoints(context.Background(), "s"); err == nil {
		t.Fatal("error = nil, want a parse error")
	}
}
