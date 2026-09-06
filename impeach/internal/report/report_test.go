package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/entireio/cli/impeach/internal/claims"
	"github.com/entireio/cli/impeach/internal/record"
	"github.com/entireio/cli/impeach/internal/verify"
)

func row(id string, status verify.Status, turn int, reasons []string, rerun record.RerunStatus, text string) Row {
	return Row{
		Claim: claims.Claim{ID: id, Text: text, Family: claims.Execution, Turn: turn, Seq: turn * 10},
		Verdict: verify.Verdict{
			ClaimID: id, Status: status, Reasons: reasons, Rerun: rerun,
			Summary: "because the record says so",
		},
	}
}

func testCheckpoint() Checkpoint {
	return Checkpoint{
		ID: "01M1TET4N33VMY0DTHNZKV5HT9", Commit: "458bb14287ec", Parent: "650a885fdf96",
		Agent: "Claude Code", SessionIDs: []string{"1c439358"},
	}
}

func testInputs() Inputs {
	return Inputs{Adapter: "claude-code", Extractors: []string{"pattern"}, TestCommand: "pytest -q", Rerun: true}
}

func TestNewCounts(t *testing.T) {
	t.Parallel()
	r := New(testCheckpoint(), testInputs(), []Row{
		row("c1", verify.Impeached, 1, []string{verify.ReasonStale}, record.RerunPass, "All tests pass."),
		row("c2", verify.Corroborated, 2, nil, record.RerunPass, "The build succeeds."),
		row("c3", verify.Uncorroborated, 3, nil, record.RerunNotRun, "The linter is clean."),
		row("c4", verify.Unverifiable, 4, nil, record.RerunNotRun, "I reviewed the callers."),
	}, nil, nil, nil)

	want := Counts{Corroborated: 1, Impeached: 1, Uncorroborated: 1, Unverifiable: 1}
	if r.Counts != want {
		t.Errorf("Counts = %+v, want %+v", r.Counts, want)
	}
}

// Impeachments are read first, so they sort first.
func TestSortPutsImpeachedFirst(t *testing.T) {
	t.Parallel()
	r := New(testCheckpoint(), testInputs(), []Row{
		row("c1", verify.Corroborated, 1, nil, record.RerunPass, "a"),
		row("c2", verify.Unverifiable, 2, nil, record.RerunNotRun, "b"),
		row("c3", verify.Impeached, 3, []string{verify.ReasonStale}, record.RerunPass, "c"),
		row("c4", verify.Uncorroborated, 4, nil, record.RerunNotRun, "d"),
	}, nil, nil, nil)

	got := make([]string, 0, len(r.Rows))
	for _, row := range r.Rows {
		got = append(got, string(row.Verdict.Status))
	}
	want := []string{"impeached", "uncorroborated", "unverifiable", "corroborated"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestSortWithinGroupByTurn(t *testing.T) {
	t.Parallel()
	r := New(testCheckpoint(), testInputs(), []Row{
		row("c1", verify.Impeached, 7, []string{verify.ReasonStale}, record.RerunPass, "later"),
		row("c2", verify.Impeached, 2, []string{verify.ReasonStale}, record.RerunPass, "earlier"),
	}, nil, nil, nil)
	if r.Rows[0].Claim.Turn != 2 {
		t.Errorf("first row turn = %d, want the earlier turn 2", r.Rows[0].Claim.Turn)
	}
}

// The lead is the impeached claim with the most reasons.
func TestLeadPicksMostReasons(t *testing.T) {
	t.Parallel()
	r := New(testCheckpoint(), testInputs(), []Row{
		row("c1", verify.Impeached, 1, []string{verify.ReasonStale}, record.RerunPass, "one reason"),
		row("c2", verify.Impeached, 2, []string{verify.ReasonStale, verify.ReasonScopeMismatch}, record.RerunPass, "two reasons"),
	}, nil, nil, nil)

	lead, ok := r.Lead()
	if !ok {
		t.Fatal("Lead() found nothing")
	}
	if lead.Claim.ID != "c2" {
		t.Errorf("Lead() = %q, want c2 with two reasons", lead.Claim.ID)
	}
}

// Ties break on a rerun with new failures.
func TestLeadTieBreaksOnRerun(t *testing.T) {
	t.Parallel()
	r := New(testCheckpoint(), testInputs(), []Row{
		row("c1", verify.Impeached, 1, []string{verify.ReasonStale}, record.RerunPass, "passes today"),
		row("c2", verify.Impeached, 2, []string{verify.ReasonStale}, record.RerunNewFailures, "fails today"),
	}, nil, nil, nil)

	lead, _ := r.Lead()
	if lead.Claim.ID != "c2" {
		t.Errorf("Lead() = %q, want the one with new failures", lead.Claim.ID)
	}
}

func TestLeadEmptyWhenNothingImpeached(t *testing.T) {
	t.Parallel()
	r := New(testCheckpoint(), testInputs(), []Row{
		row("c1", verify.Corroborated, 1, nil, record.RerunPass, "fine"),
	}, nil, nil, nil)
	if _, ok := r.Lead(); ok {
		t.Error("Lead() found an impeachment where there is none")
	}
}

func TestTableRendersHeaderRowsAndCounts(t *testing.T) {
	t.Parallel()
	r := New(testCheckpoint(), testInputs(), []Row{
		row("c1", verify.Impeached, 1, []string{verify.ReasonScopeMismatch, verify.ReasonStale},
			record.RerunNewFailures, "All tests pass."),
		row("c2", verify.Corroborated, 2, nil, record.RerunPass, "The build succeeds."),
	}, []string{"A note about a missing channel."},
		[]string{"Claim detection is pattern based."},
		[]string{"entire graph commit HEAD --json"})

	var buf bytes.Buffer
	if err := Table(&buf, r); err != nil {
		t.Fatalf("Table() error = %v", err)
	}
	out := buf.String()

	for _, want := range []string{
		"Impeach 0.1.0 report for checkpoint 01M1TET4N33VMY0DTHNZKV5HT9",
		"commit 458bb14, parent 650a885",
		"agent Claude Code",
		"Adapter claude-code",
		"A note about a missing channel.",
		"VERDICT", "FAMILY", "CLAIM", "REASON", "RERUN",
		"impeached", "corroborated",
		// Reason codes become readable phrases for people.
		"scope mismatch", "stale",
		"new failures",
		"1 corroborated   1 impeached   0 uncorroborated   0 unverifiable   0 unrequested",
		"Limitations:",
		"Claim detection is pattern based.",
		"Commands run:",
		"entire graph commit HEAD --json",
		"Nothing in this report was produced by a model",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q\n---\n%s", want, out)
		}
	}
}

// Every impeachment prints the evidence it rests on, because a verdict
// without evidence is just another claim.
func TestTablePrintsEvidenceForImpeachments(t *testing.T) {
	t.Parallel()
	rw := row("c1", verify.Impeached, 1, []string{verify.ReasonStale}, record.RerunNewFailures, "All tests pass.")
	rw.Verdict.Evidence = []verify.Evidence{
		{Type: verify.EvidenceCommand, Seq: 41, Text: "pytest tests/test_api.py",
			Excerpt: "4 passed in 0.00s", Detail: "pass (exit 0)"},
		{Type: verify.EvidenceEdit, Seq: 58, Text: "app/service.py", Detail: "edited after the last supporting run"},
		{Type: verify.EvidenceRerun, Detail: "new_failures", Text: "2 new failures",
			Command: "entire graph verify --repo /wt/head"},
	}
	r := New(testCheckpoint(), testInputs(), []Row{rw}, nil, nil, nil)

	var buf bytes.Buffer
	if err := Table(&buf, r); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"IMPEACHED [c1]",
		"pytest tests/test_api.py",
		"4 passed in 0.00s",
		"app/service.py",
		"edited after the last supporting run",
		"2 new failures",
		"reproduce: entire graph verify --repo /wt/head",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("evidence output missing %q\n---\n%s", want, out)
		}
	}
}

// A corroborated row does not get an evidence block, so the terminal output
// stays readable. The JSON and HTML carry everything.
func TestTableDoesNotExpandCorroboratedRows(t *testing.T) {
	t.Parallel()
	rw := row("c1", verify.Corroborated, 1, nil, record.RerunPass, "All tests pass.")
	rw.Verdict.Evidence = []verify.Evidence{
		{Type: verify.EvidenceCommand, Seq: 41, Text: "pytest -q", Detail: "pass"},
	}
	r := New(testCheckpoint(), testInputs(), []Row{rw}, nil, nil, nil)

	var buf bytes.Buffer
	if err := Table(&buf, r); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "CORROBORATED [c1]") {
		t.Error("corroborated rows should not print an evidence block in the terminal")
	}
}

func TestTableNoClaimsMessage(t *testing.T) {
	t.Parallel()
	r := New(testCheckpoint(), testInputs(), nil, nil, nil, nil)
	var buf bytes.Buffer
	if err := Table(&buf, r); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "No claims matched the pattern library") {
		t.Errorf("expected the no-claims message:\n%s", out)
	}
	// It must say how to look further rather than implying nothing was said.
	if !strings.Contains(out, "--model") || !strings.Contains(out, "--full") {
		t.Errorf("no-claims message should point at the next step:\n%s", out)
	}
}

func TestTableMergeCommitIsFlagged(t *testing.T) {
	t.Parallel()
	cp := testCheckpoint()
	cp.IsMerge = true
	r := New(cp, testInputs(), nil, nil, nil, nil)
	var buf bytes.Buffer
	if err := Table(&buf, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "merge commit") {
		t.Error("a merge commit must be flagged in the header")
	}
}

func TestTruncateIsTerminalOnly(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", 200)
	got := truncate(long, claimWidth)
	if len(got) != claimWidth {
		t.Errorf("truncate() length = %d, want %d", len(got), claimWidth)
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("truncate() = %q, want an ellipsis", got)
	}
	// Newlines collapse so a multi-line claim cannot break the table.
	if strings.Contains(truncate("a\nb", claimWidth), "\n") {
		t.Error("truncate() left a newline in a table cell")
	}
}

func TestRerunLabels(t *testing.T) {
	t.Parallel()
	cases := map[record.RerunStatus]string{
		record.RerunPass:        "pass",
		record.RerunNewFailures: "new failures",
		record.RerunNotRun:      "not run",
		record.RerunSkipped:     "skipped",
	}
	for in, want := range cases {
		if got := rerunLabel(in); got != want {
			t.Errorf("rerunLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPhrasesFallBackToTheCode(t *testing.T) {
	t.Parallel()
	if got := phrases([]string{verify.ReasonStale}); got != "stale" {
		t.Errorf("phrases() = %q", got)
	}
	// An unknown code still prints, rather than vanishing.
	if got := phrases([]string{"brand-new-reason"}); got != "brand-new-reason" {
		t.Errorf("phrases() = %q, want the raw code", got)
	}
	if got := phrases(nil); got != "" {
		t.Errorf("phrases(nil) = %q, want empty", got)
	}
}
