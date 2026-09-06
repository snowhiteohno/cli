package verify

import (
	"strings"
	"testing"

	"github.com/entireio/cli/impeach/internal/claims"
	"github.com/entireio/cli/impeach/internal/record"
	"github.com/entireio/cli/impeach/internal/transcript"
)

// builder assembles a hand-built Record, which is how each verifier is tested
// in isolation from Entire, git and any agent.
type builder struct {
	events []transcript.Event
	seq    int
	rerun  *record.Rerun
	files  []string
}

func newBuilder() *builder {
	return &builder{rerun: &record.Rerun{Status: record.RerunPass}}
}

func (b *builder) prompt(text string) *builder {
	b.seq++
	b.events = append(b.events, transcript.Event{Seq: b.seq, Kind: transcript.Prompt, Text: text})
	return b
}

func (b *builder) say(text string) *builder {
	b.seq++
	b.events = append(b.events, transcript.Event{
		Seq: b.seq, Kind: transcript.AssistantText, Turn: 1, Text: text,
	})
	return b
}

func (b *builder) edit(path string) *builder {
	b.seq++
	b.events = append(b.events, transcript.Event{Seq: b.seq, Kind: transcript.FileEdit, Path: path})
	return b
}

func (b *builder) run(cmd, output string, exit int) *builder {
	b.seq++
	b.events = append(b.events, transcript.Event{
		Seq: b.seq, Kind: transcript.Command,
		Cmd: &transcript.CommandInfo{
			Cmd: cmd, Output: output, ExitKnown: true, ExitCode: exit,
			Targets: transcript.CommandTargets(cmd),
		},
	})
	return b
}

func (b *builder) touched(files ...string) *builder {
	b.files = files
	return b
}

func (b *builder) withRerun(r *record.Rerun) *builder {
	b.rerun = r
	return b
}

func (b *builder) record() *Record {
	return &Record{
		Stream:       &transcript.Stream{Events: b.events, Adapter: "claude-code"},
		Rerun:        b.rerun,
		FilesTouched: b.files,
		Impacts:      map[string]*record.Impact{},
	}
}

// claimFrom extracts the single claim from a sentence, so the verifier is
// tested against real extractor output rather than a hand-made Claim.
func claimFrom(t *testing.T, sentence string, seq int) claims.Claim {
	t.Helper()
	got, err := claims.Pattern{}.Extract([]transcript.Event{
		{Seq: seq, Kind: transcript.AssistantText, Turn: 1, Text: sentence},
	})
	if err != nil {
		t.Fatalf("Extract(%q) error = %v", sentence, err)
	}
	if len(got) != 1 {
		t.Fatalf("Extract(%q) gave %d claims, want 1", sentence, len(got))
	}
	return got[0]
}

const pytestPass = "collected 18 items\n\ntests/test_api.py ....\n\n============ 18 passed in 0.02s ============"
const pytestSubset = "collected 4 items\n\ntests/test_api.py ....\n\n============ 4 passed in 0.00s ============"
const pytestFail = "collected 18 items\n\nFAILED tests/test_service.py::test_rounding\n\n====== 2 failed, 16 passed in 0.03s ======"

// The honest case: a full run after the last edit, output shows success.
func TestExecutionCorroborated(t *testing.T) {
	t.Parallel()
	r := newBuilder().
		prompt("fix the rounding").
		edit("app/service.py").
		run("pytest -q", pytestPass, 0).
		say("All tests pass.").
		touched("app/service.py").
		record()

	c := claimFrom(t, "All tests pass.", 99)
	v := Execution{}.Verify(c, r)

	if v.Status != Corroborated {
		t.Fatalf("Status = %q, want %q (reasons %v, summary %q)", v.Status, Corroborated, v.Reasons, v.Summary)
	}
	if len(v.Reasons) != 0 {
		t.Errorf("Reasons = %v, want none", v.Reasons)
	}
	// The command it rests on must be in evidence.
	if !hasEvidence(v, EvidenceCommand, "pytest -q") {
		t.Errorf("evidence missing the supporting command: %+v", v.Evidence)
	}
}

// The stale scenario from the design: a suite run, then one more edit to a
// relevant file, and the claim repeated with nothing run afterwards.
func TestExecutionStale(t *testing.T) {
	t.Parallel()
	r := newBuilder().
		prompt("fix the rounding").
		edit("app/service.py").
		run("pytest -q", pytestPass, 0).
		edit("app/service.py").
		say("All tests pass.").
		touched("app/service.py").
		record()

	c := claimFrom(t, "All tests pass.", 99)
	v := Execution{}.Verify(c, r)

	if v.Status != Impeached {
		t.Fatalf("Status = %q, want %q", v.Status, Impeached)
	}
	if !hasReason(v, ReasonStale) {
		t.Errorf("Reasons = %v, want %q", v.Reasons, ReasonStale)
	}
	if !hasEvidence(v, EvidenceEdit, "app/service.py") {
		t.Errorf("evidence missing the later edit: %+v", v.Evidence)
	}
}

// An edit to a file the checkpoint did not change cannot make a run stale.
// The agent edited it and reverted it, so it is not part of the change.
func TestExecutionEditToIrrelevantFileIsNotStale(t *testing.T) {
	t.Parallel()
	r := newBuilder().
		edit("app/service.py").
		run("pytest -q", pytestPass, 0).
		edit("scratch/notes.py").
		say("All tests pass.").
		touched("app/service.py").
		record()

	v := Execution{}.Verify(claimFrom(t, "All tests pass.", 99), r)
	if hasReason(v, ReasonStale) {
		t.Errorf("Reasons = %v; an edit outside the checkpoint's files must not be stale", v.Reasons)
	}
	if v.Status != Corroborated {
		t.Errorf("Status = %q, want %q", v.Status, Corroborated)
	}
}

// The scope scenario: only one of four test files ran, and the claim covers
// all tests.
func TestExecutionScopeMismatch(t *testing.T) {
	t.Parallel()
	r := newBuilder().
		edit("app/service.py").
		run("pytest tests/test_api.py", pytestSubset, 0).
		say("All tests pass.").
		touched("app/service.py").
		record()

	c := claimFrom(t, "All tests pass.", 99)
	if c.Scope != claims.ScopeAll {
		t.Fatalf("claim scope = %v, want all", c.Scope)
	}
	v := Execution{}.Verify(c, r)

	if v.Status != Impeached {
		t.Fatalf("Status = %q, want %q", v.Status, Impeached)
	}
	if !hasReason(v, ReasonScopeMismatch) {
		t.Errorf("Reasons = %v, want %q", v.Reasons, ReasonScopeMismatch)
	}
	if !strings.Contains(v.Summary, "rather than everything claimed") {
		t.Errorf("Summary = %q, want it to explain the scope gap", v.Summary)
	}
}

// A claim that names the file it ran is honest, even though the run was
// narrow. Impeaching it would be a false positive, which is the failure mode
// that matters most for a tool whose whole point is accuracy.
func TestExecutionScopedClaimOnNarrowRunIsCorroborated(t *testing.T) {
	t.Parallel()
	r := newBuilder().
		edit("app/service.py").
		run("pytest tests/test_api.py", pytestSubset, 0).
		say("`tests/test_api.py` passes.").
		touched("app/service.py").
		record()

	v := Execution{}.Verify(claimFrom(t, "4 passed in `tests/test_api.py`.", 99), r)
	if v.Status != Corroborated {
		t.Errorf("Status = %q, want %q (reasons %v)", v.Status, Corroborated, v.Reasons)
	}
}

// An unspecified claim resting on a narrow run is corroborated with a note,
// not impeached. The agent did not claim more than it ran.
func TestExecutionUnspecifiedClaimOnNarrowRunNotesTheSubset(t *testing.T) {
	t.Parallel()
	r := newBuilder().
		edit("app/service.py").
		run("pytest tests/test_api.py", pytestSubset, 0).
		touched("app/service.py").
		record()

	c := claimFrom(t, "The tests now pass.", 99)
	if c.Scope != claims.ScopeUnspecified {
		t.Fatalf("claim scope = %v, want unspecified", c.Scope)
	}
	v := Execution{}.Verify(c, r)
	if v.Status != Corroborated {
		t.Fatalf("Status = %q, want %q (reasons %v)", v.Status, Corroborated, v.Reasons)
	}
	if !strings.Contains(v.Summary, "subset only") {
		t.Errorf("Summary = %q, want it to note the subset", v.Summary)
	}
}

func TestExecutionContradictedByOutput(t *testing.T) {
	t.Parallel()
	r := newBuilder().
		edit("app/service.py").
		run("pytest -q", pytestFail, 1).
		say("All tests pass.").
		touched("app/service.py").
		record()

	v := Execution{}.Verify(claimFrom(t, "All tests pass.", 99), r)
	if v.Status != Impeached {
		t.Fatalf("Status = %q, want %q", v.Status, Impeached)
	}
	if !hasReason(v, ReasonContradictedOutput) {
		t.Errorf("Reasons = %v, want %q", v.Reasons, ReasonContradictedOutput)
	}
}

// The rerun is additive: a claim can be impeached by a fresh run even when
// the tool log looked clean at commit time.
func TestExecutionContradictedByRerun(t *testing.T) {
	t.Parallel()
	r := newBuilder().
		edit("app/service.py").
		run("pytest -q", pytestPass, 0).
		touched("app/service.py").
		withRerun(&record.Rerun{
			Status:      record.RerunNewFailures,
			NewFailures: []string{"tests/test_service.py::test_rounding", "tests/test_rounding.py::test_negative"},
			Verdict:     "REGRESSION in 2 tests",
		}).
		record()

	v := Execution{}.Verify(claimFrom(t, "All tests pass.", 99), r)
	if v.Status != Impeached {
		t.Fatalf("Status = %q, want %q", v.Status, Impeached)
	}
	if !hasReason(v, ReasonContradictedRerun) {
		t.Errorf("Reasons = %v, want %q", v.Reasons, ReasonContradictedRerun)
	}
	if v.Rerun != record.RerunNewFailures {
		t.Errorf("Rerun = %q, want %q", v.Rerun, record.RerunNewFailures)
	}
}

// Scope and stale and the rerun stack, which is what makes a row the lead
// impeachment.
func TestExecutionReasonsAccumulate(t *testing.T) {
	t.Parallel()
	r := newBuilder().
		edit("app/service.py").
		run("pytest tests/test_api.py", pytestSubset, 0).
		edit("app/service.py").
		touched("app/service.py").
		withRerun(&record.Rerun{
			Status:      record.RerunNewFailures,
			NewFailures: []string{"tests/test_service.py::test_rounding"},
		}).
		record()

	v := Execution{}.Verify(claimFrom(t, "All tests pass.", 99), r)
	if v.Status != Impeached {
		t.Fatalf("Status = %q, want %q", v.Status, Impeached)
	}
	for _, want := range []string{ReasonScopeMismatch, ReasonStale, ReasonContradictedRerun} {
		if !hasReason(v, want) {
			t.Errorf("Reasons = %v, missing %q", v.Reasons, want)
		}
	}
}

// A pass claim with no test command in the log is uncorroborated, not
// impeached. The channel exists but says nothing.
func TestExecutionUncorroboratedWhenNoCommandMatches(t *testing.T) {
	t.Parallel()
	r := newBuilder().
		edit("app/service.py").
		run("git status", " M app/service.py", 0).
		touched("app/service.py").
		record()

	v := Execution{}.Verify(claimFrom(t, "All tests pass.", 99), r)
	if v.Status != Uncorroborated {
		t.Errorf("Status = %q, want %q", v.Status, Uncorroborated)
	}
}

// Output that no parser recognises makes a command non-supporting, which is
// uncorroborated rather than a failure.
func TestExecutionUncorroboratedWhenOutputUnparsed(t *testing.T) {
	t.Parallel()
	r := newBuilder().
		edit("app/service.py").
		run("pytest -q", "some runner nobody has ever seen", 0).
		touched("app/service.py").
		record()

	// Exit 0 with unrecognised text still counts as success, because the
	// transcript settled the exit status. Drop that and it is unparsed.
	for i := range r.Stream.Events {
		if r.Stream.Events[i].Kind == transcript.Command {
			r.Stream.Events[i].Cmd.ExitKnown = false
		}
	}
	v := Execution{}.Verify(claimFrom(t, "All tests pass.", 99), r)
	if v.Status != Uncorroborated {
		t.Errorf("Status = %q, want %q", v.Status, Uncorroborated)
	}
}

// Missing data is a state. A transcript with no command records makes an
// execution claim unverifiable, never an error and never a verdict.
func TestExecutionUnverifiableWithoutCommandChannel(t *testing.T) {
	t.Parallel()
	r := newBuilder().say("All tests pass.").record()

	v := Execution{}.Verify(claimFrom(t, "All tests pass.", 99), r)
	if v.Status != Unverifiable {
		t.Fatalf("Status = %q, want %q", v.Status, Unverifiable)
	}
	if !strings.Contains(v.Summary, "no command records") {
		t.Errorf("Summary = %q, want it to name the missing channel", v.Summary)
	}
}

// A claim about the linter must not be corroborated by a test run.
func TestExecutionKindSeparatesRunners(t *testing.T) {
	t.Parallel()
	r := newBuilder().
		edit("app/service.py").
		run("pytest -q", pytestPass, 0).
		touched("app/service.py").
		record()

	v := Execution{}.Verify(claimFrom(t, "The linter is clean.", 99), r)
	if v.Status != Uncorroborated {
		t.Errorf("Status = %q, want %q; a test run says nothing about the linter", v.Status, Uncorroborated)
	}
}

// The user's own test command counts as a runner even when no built-in
// pattern recognises it.
func TestExecutionRecognisesUserTestCommand(t *testing.T) {
	t.Parallel()
	r := newBuilder().
		edit("app/service.py").
		run("bin/check-everything", "18 passed", 0).
		touched("app/service.py").
		record()

	v := Execution{TestCommand: "bin/check-everything"}.Verify(claimFrom(t, "All tests pass.", 99), r)
	if v.Status != Corroborated {
		t.Errorf("Status = %q, want %q (reasons %v)", v.Status, Corroborated, v.Reasons)
	}
}

// The last successful run is what the claim rests on, not the first.
func TestExecutionUsesTheLastSuccessfulRun(t *testing.T) {
	t.Parallel()
	r := newBuilder().
		edit("app/service.py").
		run("pytest -q", pytestPass, 0).
		edit("app/service.py").
		run("pytest -q", pytestPass, 0).
		touched("app/service.py").
		record()

	v := Execution{}.Verify(claimFrom(t, "All tests pass.", 99), r)
	if v.Status != Corroborated {
		t.Errorf("Status = %q, want %q; the second run came after the last edit", v.Status, Corroborated)
	}
	if hasReason(v, ReasonStale) {
		t.Errorf("Reasons = %v, want no stale", v.Reasons)
	}
}

// The rerun column is filled whatever the verdict, because the verdict is
// about commit time and the rerun is about now.
func TestExecutionRerunColumnAlwaysFilled(t *testing.T) {
	t.Parallel()
	for _, status := range []record.RerunStatus{
		record.RerunPass, record.RerunNotRun, record.RerunSkipped,
	} {
		r := newBuilder().
			edit("app/service.py").
			run("pytest -q", pytestPass, 0).
			touched("app/service.py").
			withRerun(&record.Rerun{Status: status}).
			record()
		v := Execution{}.Verify(claimFrom(t, "All tests pass.", 99), r)
		if v.Rerun != status {
			t.Errorf("Rerun = %q, want %q", v.Rerun, status)
		}
	}
}

// Ordering uses Seq, so a transcript with no timestamps still detects
// staleness correctly.
func TestExecutionStalenessUsesSeqNotTime(t *testing.T) {
	t.Parallel()
	r := newBuilder().
		edit("app/service.py").
		run("pytest -q", pytestPass, 0).
		edit("app/service.py").
		touched("app/service.py").
		record()
	for i := range r.Stream.Events {
		r.Stream.Events[i].TS = r.Stream.Events[i].TS.UTC() // all zero
	}
	if r.Stream.TimestampsPresent() {
		t.Fatal("test setup: timestamps should be absent")
	}
	v := Execution{}.Verify(claimFrom(t, "All tests pass.", 99), r)
	if !hasReason(v, ReasonStale) {
		t.Errorf("Reasons = %v, want stale detected from Seq alone", v.Reasons)
	}
}

// Evidence excerpts are bounded, because a report gets pasted into pull
// requests and must not carry a transcript.
func TestEvidenceExcerptIsBounded(t *testing.T) {
	t.Parallel()
	huge := strings.Repeat("x", 5000) + "\n18 passed"
	r := newBuilder().
		edit("app/service.py").
		run("pytest -q", huge, 0).
		touched("app/service.py").
		record()

	v := Execution{}.Verify(claimFrom(t, "All tests pass.", 99), r)
	for _, e := range v.Evidence {
		if len(e.Excerpt) > maxExcerpt+64 {
			t.Errorf("excerpt is %d bytes, want it bounded near %d", len(e.Excerpt), maxExcerpt)
		}
	}
}

// When the checkpoint's file list is unavailable, every edited file counts.
// That is the conservative reading: it can only add staleness, never hide it.
func TestRelevantEditedFilesFallsBackToAllEdits(t *testing.T) {
	t.Parallel()
	r := newBuilder().edit("a.py").edit("b.py").record()
	got := r.RelevantEditedFiles()
	if len(got) != 2 {
		t.Errorf("RelevantEditedFiles() = %v, want both edits when no file list is known", got)
	}
}

func TestRelevantEditedFilesIntersects(t *testing.T) {
	t.Parallel()
	r := newBuilder().edit("a.py").edit("b.py").touched("a.py").record()
	got := r.RelevantEditedFiles()
	if len(got) != 1 || got[0] != "a.py" {
		t.Errorf("RelevantEditedFiles() = %v, want [a.py]", got)
	}
}

func hasReason(v Verdict, code string) bool {
	for _, r := range v.Reasons {
		if r == code {
			return true
		}
	}
	return false
}

func hasEvidence(v Verdict, typ EvidenceType, contains string) bool {
	for _, e := range v.Evidence {
		if e.Type == typ && strings.Contains(e.Text, contains) {
			return true
		}
	}
	return false
}
