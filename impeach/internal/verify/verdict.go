// Package verify holds one verifier per claim family. A verifier reads a claim
// and the record and returns a verdict with the evidence attached. Nothing
// here reaches the outside world: verifiers are pure functions over data the
// record layer already gathered, which is what makes them deterministic.
package verify

import (
	"fmt"

	"github.com/entireio/cli/impeach/internal/claims"
	"github.com/entireio/cli/impeach/internal/record"
	"github.com/entireio/cli/impeach/internal/transcript"
)

// Status is one of the four verdicts.
type Status string

const (
	// Corroborated means the record supports the claim.
	Corroborated Status = "corroborated"
	// Impeached means the record contradicts it.
	Impeached Status = "impeached"
	// Uncorroborated means the evidence channel exists but says nothing
	// either way.
	Uncorroborated Status = "uncorroborated"
	// Unverifiable means the evidence channel is missing. Missing data is a
	// state, never an error.
	Unverifiable Status = "unverifiable"
)

// Reason codes. Every impeachment carries at least one.
const (
	ReasonStale              = "stale"
	ReasonScopeMismatch      = "scope-mismatch"
	ReasonContradictedOutput = "contradicted-output"
	ReasonContradictedRerun  = "contradicted-rerun"
	ReasonCallersExist       = "callers-exist"
	ReasonSignatureChanged   = "signature-changed"
	ReasonNotInDiff          = "not-in-diff"
	ReasonNeverRead          = "never-read"
)

// EvidenceType names where a piece of evidence came from.
type EvidenceType string

const (
	EvidenceCommand EvidenceType = "command"
	EvidenceEdit    EvidenceType = "edit"
	EvidenceRead    EvidenceType = "read"
	EvidenceEntity  EvidenceType = "entity"
	EvidenceImpact  EvidenceType = "impact"
	EvidenceRerun   EvidenceType = "rerun"
)

// maxExcerpt bounds an output excerpt. The security policy caps evidence
// excerpts so a report pasted into a pull request cannot carry a transcript.
const maxExcerpt = 400

// Evidence is one thing the verdict rests on.
type Evidence struct {
	Type EvidenceType
	Seq  int
	// Text is the command, path or description.
	Text string
	// Excerpt is bounded and scrubbed output, empty when there is none.
	Excerpt string
	// Detail is a short parsed conclusion, such as "pass" or "3 callers".
	Detail string
	// Command is the exact command a reader can run to see the same thing.
	Command string
}

// Verdict is the answer for one claim.
type Verdict struct {
	ClaimID string
	Status  Status
	// Reasons are the reason codes, in the order they were found.
	Reasons []string
	// Summary is one plain sentence a reader can act on.
	Summary  string
	Evidence []Evidence
	// Rerun is filled independently of the verdict, because the verdict is
	// about the testimony at commit time and the rerun is about the code now.
	Rerun record.RerunStatus
}

// Verifier checks one family of claims.
type Verifier interface {
	Family() claims.Family
	Verify(c claims.Claim, r *Record) Verdict
}

// Record is everything the verifiers may read. It is assembled once by the
// record layer and then never mutated, so every verdict in a run rests on the
// same evidence.
type Record struct {
	// Stream is the parsed transcript.
	Stream *transcript.Stream
	// Changes is the entity-level diff, nil when Graph could not produce one.
	Changes *record.CommitChanges
	// Rerun is the adjudicated test rerun.
	Rerun *record.Rerun
	// FilesTouched is the checkpoint's changed-file list from
	// `checkpoint explain --json`, repository-relative.
	FilesTouched []string
	// Impacts caches `graph impact` results by symbol name.
	Impacts map[string]*record.Impact
	// RepoRoot is the repository the audit ran against.
	RepoRoot string
}

// RelevantEditedFiles returns the files the session edited that also appear in
// the checkpoint's changed-file list.
//
// The intersection matters: a file the agent edited and then reverted is not
// part of what the checkpoint changed, so an edit to it cannot make a test run
// stale. When the checkpoint's file list is unavailable, every edited file
// counts, which is the conservative reading.
func (r *Record) RelevantEditedFiles() []string {
	edited := r.Stream.EditedPaths()
	if len(r.FilesTouched) == 0 {
		return edited
	}
	touched := make(map[string]bool, len(r.FilesTouched))
	for _, f := range r.FilesTouched {
		touched[f] = true
	}
	var out []string
	for _, e := range edited {
		if touched[e] {
			out = append(out, e)
		}
	}
	return out
}

// addReason appends a reason code once.
func (v *Verdict) addReason(code string) {
	for _, r := range v.Reasons {
		if r == code {
			return
		}
	}
	v.Reasons = append(v.Reasons, code)
}

// excerpt bounds a string for display.
func excerpt(s string) string {
	if len(s) <= maxExcerpt {
		return s
	}
	return s[:maxExcerpt] + fmt.Sprintf("... [%d bytes truncated]", len(s)-maxExcerpt)
}
