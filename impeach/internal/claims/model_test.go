package claims

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/entireio/cli/impeach/internal/runner"
	"github.com/entireio/cli/impeach/internal/transcript"
)

// modelRun builds a Fake that answers any invocation of "extract" with the
// given stdout, so the extractor is tested without a real model.
func modelRun(stdout string, exit int) *runner.Fake {
	f := runner.NewFake(runner.Call{Name: "extract", Stdout: stdout, Exit: exit})
	return f
}

func turns(texts ...string) []transcript.Event {
	var out []transcript.Event
	for i, t := range texts {
		out = append(out, transcript.Event{
			Seq: i + 1, Kind: transcript.AssistantText, Turn: i + 1, Text: t,
		})
	}
	return out
}

func TestModelExtractsClaims(t *testing.T) {
	t.Parallel()
	f := modelRun(`[{"text":"All tests pass.","family":"execution","subject":""},
	                {"text":"Added parse_refund.","family":"structural","subject":"parse_refund"}]`, 0)
	m := &Model{Command: "extract", Runner: f}

	got, err := m.Extract(turns("some prose"))
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d claims, want 2: %+v", len(got), got)
	}
	if got[0].Family != Execution || got[1].Family != Structural {
		t.Errorf("families = %v, %v", got[0].Family, got[1].Family)
	}
	// Every claim records which extractor produced it, so the report can mark
	// it and a reader knows a model was involved.
	for _, c := range got {
		if c.Extractor != "model:extract" {
			t.Errorf("Extractor = %q, want model:extract", c.Extractor)
		}
	}
	if got[1].Subjects[0] != "parse_refund" {
		t.Errorf("Subjects = %v, want the subject the model named", got[1].Subjects)
	}
	// The kind and scope are decided by the deterministic library, not by the
	// model, because the pattern set already encodes those distinctions.
	if got[0].Kind != KindTest {
		t.Errorf("Kind = %q, want test", got[0].Kind)
	}
	if got[0].Scope != ScopeAll {
		t.Errorf("Scope = %v, want all", got[0].Scope)
	}
}

// Only the assistant text of one turn is sent. Never a prompt, never a tool
// output, never a file. This is the whole basis of the claim in the security
// document about what leaves the machine.
func TestModelSendsOnlyAssistantTextOneTurnAtATime(t *testing.T) {
	t.Parallel()
	f := runner.NewFake(
		runner.Call{Name: "extract", Stdout: "[]"},
		runner.Call{Name: "extract", Stdout: "[]"},
	)
	m := &Model{Command: "extract", Runner: f}

	events := []transcript.Event{
		{Seq: 1, Kind: transcript.Prompt, Turn: 0, Text: "SECRET USER PROMPT"},
		{Seq: 2, Kind: transcript.AssistantText, Turn: 1, Text: "first turn"},
		{Seq: 3, Kind: transcript.Command, Turn: 1,
			Cmd: &transcript.CommandInfo{Cmd: "pytest", Output: "SECRET TOOL OUTPUT"}},
		{Seq: 4, Kind: transcript.FileRead, Turn: 1, Path: "secret.py"},
		{Seq: 5, Kind: transcript.AssistantText, Turn: 2, Text: "second turn"},
	}
	if _, err := m.Extract(events); err != nil {
		t.Fatalf("Extract() error = %v", err)
	}

	// One call per assistant turn, so a turn is the unit of exposure.
	if n := len(f.Requests()); n != 2 {
		t.Errorf("made %d calls, want one per assistant turn (2)", n)
	}
}

// Stdin content is asserted directly, because this is the security boundary.
func TestModelStdinCarriesOnlyTheTurn(t *testing.T) {
	t.Parallel()
	var seen []byte
	spy := spyRunner{onRun: func(stdin []byte) { seen = append(seen, stdin...) }, stdout: "[]"}
	m := &Model{Command: "extract", Runner: &spy}

	events := []transcript.Event{
		{Seq: 1, Kind: transcript.Prompt, Turn: 0, Text: "SECRET USER PROMPT"},
		{Seq: 2, Kind: transcript.AssistantText, Turn: 1, Text: "the agent said this"},
		{Seq: 3, Kind: transcript.Command, Turn: 1,
			Cmd: &transcript.CommandInfo{Cmd: "pytest", Output: "SECRET TOOL OUTPUT"}},
	}
	if _, err := m.Extract(events); err != nil {
		t.Fatal(err)
	}
	s := string(seen)
	if !strings.Contains(s, "the agent said this") {
		t.Error("the turn's assistant text was not sent")
	}
	for _, forbidden := range []string{"SECRET USER PROMPT", "SECRET TOOL OUTPUT", "secret.py"} {
		if strings.Contains(s, forbidden) {
			t.Errorf("stdin leaked %q to the model command", forbidden)
		}
	}
}

// A cap on turns means enabling --model on a long session cannot quietly send
// a large amount of text to a third party.
func TestModelCapsTurns(t *testing.T) {
	t.Parallel()
	f := runner.NewFake(runner.Call{Name: "extract", Stdout: "[]"})
	m := &Model{Command: "extract", Runner: f, MaxTurns: 2}

	if _, err := m.Extract(turns("a", "b", "c", "d", "e")); err != nil {
		t.Fatal(err)
	}
	if n := len(f.Requests()); n != 2 {
		t.Errorf("made %d calls, want the cap of 2", n)
	}
	if len(m.Warnings) == 0 {
		t.Error("dropping turns must be reported, not silent")
	}
}

// Anything that is not valid JSON is dropped with a warning. A half-parsed
// claim is worse than a missing one.
func TestModelDropsUnusableOutput(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"prose":           "Sure! Here are the claims I found.",
		"broken json":     `[{"text":"a","family":`,
		"empty":           "",
		"object not list": `{"text":"a","family":"execution"}`,
	}
	for name, stdout := range cases {
		f := modelRun(stdout, 0)
		m := &Model{Command: "extract", Runner: f}
		got, err := m.Extract(turns("prose"))
		if err != nil {
			t.Fatalf("%s: Extract() error = %v, want a dropped turn not a failure", name, err)
		}
		if len(got) != 0 {
			t.Errorf("%s: got %d claims, want none", name, len(got))
		}
		if len(m.Warnings) == 0 {
			t.Errorf("%s: dropping output must be reported", name)
		}
	}
}

// Models emit fenced code blocks constantly, so a fence is tolerated. Nothing
// else is.
func TestModelToleratesCodeFence(t *testing.T) {
	t.Parallel()
	f := modelRun("```json\n[{\"text\":\"All tests pass.\",\"family\":\"execution\",\"subject\":\"\"}]\n```", 0)
	m := &Model{Command: "extract", Runner: f}
	got, err := m.Extract(turns("prose"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("got %d claims, want 1 (a fence should be tolerated)", len(got))
	}
}

// An unknown family is dropped rather than guessed into one of the four.
func TestModelDropsUnknownFamily(t *testing.T) {
	t.Parallel()
	f := modelRun(`[{"text":"something","family":"vibes","subject":""}]`, 0)
	m := &Model{Command: "extract", Runner: f}
	got, err := m.Extract(turns("prose"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got %d claims, want none", len(got))
	}
	if len(m.Warnings) == 0 {
		t.Error("an unknown family must be reported")
	}
}

// A failing model command is a dropped turn, not a failed audit. The
// deterministic path must survive it.
func TestModelCommandFailureIsATurnNotTheRun(t *testing.T) {
	t.Parallel()
	f := runner.NewFake(runner.Call{Name: "extract", Stderr: "rate limited", Exit: 1})
	m := &Model{Command: "extract", Runner: f}
	got, err := m.Extract(turns("prose"))
	if err != nil {
		t.Fatalf("Extract() error = %v, want the turn dropped", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d claims", len(got))
	}
	if len(m.Warnings) == 0 || !strings.Contains(strings.Join(m.Warnings, " "), "rate limited") {
		t.Errorf("Warnings = %v, want the command's own error surfaced", m.Warnings)
	}
}

func TestModelEmptyCommandIsAnError(t *testing.T) {
	t.Parallel()
	m := &Model{Command: "   ", Runner: runner.NewFake()}
	if _, err := m.Extract(turns("prose")); err == nil {
		t.Fatal("Extract() error = nil, want an error for an empty command")
	}
}

// There is no shell anywhere in Impeach, so the command is tokenized here and
// passed as argv. Nothing is expanded or substituted.
func TestSplitCommand(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want []string
	}{
		{"claude", []string{"claude"}},
		{"claude -p", []string{"claude", "-p"}},
		{`claude -p --model "sonnet latest"`, []string{"claude", "-p", "--model", "sonnet latest"}},
		{"  spaced   out  ", []string{"spaced", "out"}},
		{`sh -c 'echo hi'`, []string{"sh", "-c", "echo hi"}},
		{"", nil},
	}
	for _, c := range cases {
		got, err := splitCommand(c.in)
		if err != nil {
			t.Errorf("splitCommand(%q) error = %v", c.in, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitCommand(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if _, err := splitCommand(`claude "unbalanced`); err == nil {
		t.Error("an unbalanced quote should be an error, not a silent guess")
	}
}

// The instruction sent to the model must ask for facts about work done, and
// must exclude intentions and plans, or the extractor would manufacture
// claims that were never made.
func TestModelPromptExcludesIntentions(t *testing.T) {
	t.Parallel()
	for _, want := range []string{"JSON array", "verbatim", "Do not invent", "intentions"} {
		if !strings.Contains(modelPrompt, want) {
			t.Errorf("the model instruction should mention %q", want)
		}
	}
}

// spyRunner records the stdin it was given.
type spyRunner struct {
	onRun  func([]byte)
	stdout string
}

func (s *spyRunner) Run(_ context.Context, _ string, _ []string, stdin []byte) ([]byte, []byte, int, error) {
	if s.onRun != nil {
		s.onRun(stdin)
	}
	return []byte(s.stdout), nil, 0, nil
}
