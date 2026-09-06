package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// probePath is the transcript captured by the Step 0 probe, scrubbed. Parsing
// it is the phase 3 acceptance test: the adapter is checked against a
// transcript Entire actually stored, not against a hand-written idea of one.
func probePath(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "..", "testdata", "transcript", "claudecode_probe.jsonl")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("probe transcript missing: %v", err)
	}
	return p
}

func loadProbe(t *testing.T) *Stream {
	t.Helper()
	raw, err := os.ReadFile(probePath(t))
	if err != nil {
		t.Fatalf("read probe transcript: %v", err)
	}
	// The scrubbed transcript uses <repo> where the absolute path was.
	a := ClaudeCode{RepoRoot: "<repo>"}
	s, err := a.ParseStream(raw)
	if err != nil {
		t.Fatalf("ParseStream() error = %v", err)
	}
	return s
}

func TestDetectAcceptsProbeTranscript(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(probePath(t))
	if err != nil {
		t.Fatal(err)
	}
	if !(ClaudeCode{}).Detect(raw) {
		t.Error("Detect() = false for a real Claude Code transcript")
	}
}

func TestDetectRejectsOtherContent(t *testing.T) {
	t.Parallel()
	cases := map[string][]byte{
		"empty":         []byte(""),
		"plain text":    []byte("this is not a transcript\n"),
		"json object":   []byte(`{"hello":"world"}`),
		"unknown types": []byte(`{"type":"weird","x":1}` + "\n" + `{"type":"other"}`),
	}
	for name, raw := range cases {
		if (ClaudeCode{}).Detect(raw) {
			t.Errorf("Detect(%s) = true, want false", name)
		}
	}
}

func TestParseProbeTranscriptShape(t *testing.T) {
	t.Parallel()
	s := loadProbe(t)

	counts := map[Kind]int{}
	for _, e := range s.Events {
		counts[e.Kind]++
	}

	// The probe session read two files, edited one file twice, and ran four
	// Bash commands. Those numbers come from inspecting the transcript by
	// hand during Step 0.
	if counts[FileRead] != 2 {
		t.Errorf("FileRead = %d, want 2", counts[FileRead])
	}
	if counts[FileEdit] != 2 {
		t.Errorf("FileEdit = %d, want 2", counts[FileEdit])
	}
	if counts[Command] != 4 {
		t.Errorf("Command = %d, want 4", counts[Command])
	}
	if counts[Prompt] != 1 {
		t.Errorf("Prompt = %d, want 1", counts[Prompt])
	}
	if counts[AssistantText] == 0 {
		t.Error("AssistantText = 0, want the agent's messages")
	}
	// No sidechain records in the probe, so nothing was skipped as a subagent.
	if s.SubagentRecords != 0 {
		t.Errorf("SubagentRecords = %d, want 0", s.SubagentRecords)
	}
	if s.Adapter != "claude-code" {
		t.Errorf("Adapter = %q", s.Adapter)
	}
}

// Every channel the four claim families depend on must be present, or the
// probe was not a valid go-ahead for the design.
func TestProbeTranscriptCarriesEveryChannel(t *testing.T) {
	t.Parallel()
	ch := loadProbe(t).Channels()
	if !ch.Commands {
		t.Error("Commands channel absent")
	}
	if !ch.Reads {
		t.Error("Reads channel absent")
	}
	if !ch.Edits {
		t.Error("Edits channel absent")
	}
	if !ch.Prompts {
		t.Error("Prompts channel absent")
	}
}

func TestProbeTranscriptPathsAreRepositoryRelative(t *testing.T) {
	t.Parallel()
	s := loadProbe(t)
	var got []string
	for _, e := range s.Events {
		if e.Kind == FileRead || e.Kind == FileEdit {
			got = append(got, e.Path)
			if filepath.IsAbs(e.Path) {
				t.Errorf("path %q is absolute; Graph reports repository-relative paths", e.Path)
			}
			if strings.Contains(e.Path, "<repo>") {
				t.Errorf("path %q still carries the scrub placeholder", e.Path)
			}
		}
	}
	want := "impeach/fixtures/app/app/service.py"
	var found bool
	for _, p := range got {
		if p == want {
			found = true
		}
	}
	if !found {
		t.Errorf("paths = %v, want one to be %q", got, want)
	}
}

// The edited path must match what `checkpoint explain --json` reports as
// files_touched, or the execution verifier cannot find its relevant files.
func TestProbeEditedPathsMatchCheckpointFilesTouched(t *testing.T) {
	t.Parallel()
	s := loadProbe(t)
	edited := s.EditedPaths()
	if len(edited) != 1 || edited[0] != "impeach/fixtures/app/app/service.py" {
		t.Errorf("EditedPaths() = %v, want the one service.py path", edited)
	}
}

func TestProbeCommandsCarryOutputAndExitStatus(t *testing.T) {
	t.Parallel()
	s := loadProbe(t)

	var pytest *CommandInfo
	var failed *CommandInfo
	for _, e := range s.Events {
		if e.Kind != Command || e.Cmd == nil {
			continue
		}
		if strings.Contains(e.Cmd.Cmd, "pytest") && pytest == nil {
			pytest = e.Cmd
		}
		if e.Cmd.ExitKnown && e.Cmd.ExitCode != 0 && failed == nil {
			failed = e.Cmd
		}
	}

	if pytest == nil {
		t.Fatal("no pytest command found in the probe transcript")
	}
	// Full output, not truncated: the summary line survived.
	if !strings.Contains(pytest.Output, "4 passed") {
		t.Errorf("pytest output missing its summary line:\n%s", pytest.Output)
	}
	if !pytest.ExitKnown || pytest.ExitCode != 0 {
		t.Errorf("pytest ExitKnown = %v, ExitCode = %d, want true and 0", pytest.ExitKnown, pytest.ExitCode)
	}
	// The probe session ran one command that failed with an explicit code,
	// which is what makes ExitKnown true in the failing direction too.
	if failed == nil {
		t.Fatal("expected one failing command with a recovered exit code")
	}
	if failed.ExitCode != 129 {
		t.Errorf("failed ExitCode = %d, want 129 as recorded in the probe", failed.ExitCode)
	}
}

// The pytest run named one of the four test files, which is exactly the scope
// mismatch the demo rests on.
func TestProbePytestCommandTargetsOneTestFile(t *testing.T) {
	t.Parallel()
	s := loadProbe(t)
	for _, e := range s.Events {
		if e.Kind == Command && e.Cmd != nil && strings.Contains(e.Cmd.Cmd, "pytest") {
			want := "tests/test_api.py"
			if len(e.Cmd.Targets) != 1 || e.Cmd.Targets[0] != want {
				t.Errorf("Targets = %v, want [%s]", e.Cmd.Targets, want)
			}
			return
		}
	}
	t.Fatal("no pytest command found")
}

func TestProbeTranscriptHasTimestampsAndOrdering(t *testing.T) {
	t.Parallel()
	s := loadProbe(t)
	if !s.TimestampsPresent() {
		t.Error("TimestampsPresent() = false, want true for a Claude Code transcript")
	}
	for i, e := range s.Events {
		if e.Seq != i+1 {
			t.Fatalf("event %d has Seq %d; Seq must be dense and ordered", i, e.Seq)
		}
	}
	// Turns increase monotonically so "as of turn N" reasoning is sound.
	last := 0
	for _, e := range s.Events {
		if e.Turn < last {
			t.Fatalf("turn went backwards: %d after %d", e.Turn, last)
		}
		last = e.Turn
	}
}

// Reasoning is not testimony. A thinking block must never become a claim.
func TestParseSkipsThinkingBlocks(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"type":"assistant","sessionId":"s","timestamp":"2026-09-06T04:14:21.356Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"I should claim all tests pass"},{"type":"text","text":"Done."}]}}`)
	s, err := ClaudeCode{}.ParseStream(raw)
	if err != nil {
		t.Fatalf("ParseStream() error = %v", err)
	}
	if len(s.Events) != 1 {
		t.Fatalf("got %d events, want 1", len(s.Events))
	}
	if s.Events[0].Kind != AssistantText || s.Events[0].Text != "Done." {
		t.Errorf("event = %+v, want the text block only", s.Events[0])
	}
}

func TestParseCountsSubagentRecords(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"type":"assistant","sessionId":"s","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"subagent talking"}]}}
{"type":"assistant","sessionId":"s","message":{"role":"assistant","content":[{"type":"text","text":"main agent"}]}}`)
	s, err := ClaudeCode{}.ParseStream(raw)
	if err != nil {
		t.Fatalf("ParseStream() error = %v", err)
	}
	if s.SubagentRecords != 1 {
		t.Errorf("SubagentRecords = %d, want 1", s.SubagentRecords)
	}
	if len(s.Events) != 1 || s.Events[0].Text != "main agent" {
		t.Errorf("events = %+v, want only the main agent's text", s.Events)
	}
}

// Attachment records are harness bookkeeping, confirmed by the probe. If they
// ever became evidence this test would catch it.
func TestParseIgnoresAttachmentsAndQueueOperations(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"type":"attachment","sessionId":"s","attachment":{"type":"total_tokens_reminder"}}
{"type":"queue-operation","operation":"x"}
{"type":"last-prompt","lastPrompt":"x"}
{"type":"assistant","sessionId":"s","message":{"role":"assistant","content":[{"type":"text","text":"only me"}]}}`)
	s, err := ClaudeCode{}.ParseStream(raw)
	if err != nil {
		t.Fatalf("ParseStream() error = %v", err)
	}
	if len(s.Events) != 1 {
		t.Fatalf("got %d events, want 1: %+v", len(s.Events), s.Events)
	}
	if s.SkippedRecords != 0 {
		t.Errorf("SkippedRecords = %d, want 0; known bookkeeping is not a skip", s.SkippedRecords)
	}
}

func TestParseEmptyAndUnreadableTranscripts(t *testing.T) {
	t.Parallel()
	if _, err := (ClaudeCode{}).ParseStream(nil); err == nil {
		t.Error("ParseStream(nil) error = nil, want an error")
	}
	if _, err := (ClaudeCode{}).ParseStream([]byte("not json\nalso not json\n")); err == nil {
		t.Error("ParseStream(garbage) error = nil, want an error")
	}
}

func TestLastEditSeq(t *testing.T) {
	t.Parallel()
	s := &Stream{Events: []Event{
		{Seq: 1, Kind: FileEdit, Path: "a.py"},
		{Seq: 2, Kind: Command},
		{Seq: 3, Kind: FileEdit, Path: "b.py"},
		{Seq: 4, Kind: FileEdit, Path: "a.py"},
	}}
	if seq, ok := s.LastEditSeq([]string{"a.py"}); !ok || seq != 4 {
		t.Errorf("LastEditSeq([a.py]) = %d, %v; want 4, true", seq, ok)
	}
	if seq, ok := s.LastEditSeq([]string{"b.py"}); !ok || seq != 3 {
		t.Errorf("LastEditSeq([b.py]) = %d, %v; want 3, true", seq, ok)
	}
	if _, ok := s.LastEditSeq([]string{"c.py"}); ok {
		t.Error("LastEditSeq([c.py]) found an edit that does not exist")
	}
}

func TestChannelsAbsentIsAState(t *testing.T) {
	t.Parallel()
	// A transcript with prose only: every tool channel is missing, which must
	// be reported as absent rather than causing a failure.
	s := &Stream{Events: []Event{{Seq: 1, Kind: AssistantText, Text: "All tests pass."}}}
	ch := s.Channels()
	if ch.Commands || ch.Reads || ch.Edits || ch.Prompts {
		t.Errorf("Channels() = %+v, want every channel absent", ch)
	}
}

func TestDetectPicksAnAdapter(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(probePath(t))
	if err != nil {
		t.Fatal(err)
	}
	a, err := Detect(raw, []Adapter{ClaudeCode{}})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if a.Name() != "claude-code" {
		t.Errorf("Detect() = %q", a.Name())
	}
	if _, err := Detect([]byte("nope"), []Adapter{ClaudeCode{}}); err == nil {
		t.Error("Detect(unknown) error = nil, want an error naming the adapters tried")
	}
}

func TestKindString(t *testing.T) {
	t.Parallel()
	names := map[Kind]string{
		Prompt: "prompt", AssistantText: "assistant_text", ToolCall: "tool_call",
		ToolResult: "tool_result", FileRead: "file_read", FileEdit: "file_edit", Command: "command",
	}
	for k, want := range names {
		if got := k.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", int(k), got, want)
		}
	}
}
