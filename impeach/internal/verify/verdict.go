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
	// EvidenceChannel records the state of an evidence channel, so a gated
	// verdict carries the reason it was gated as evidence in its own right.
	EvidenceChannel EvidenceType = "channel"
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
	// Ledger is the state of every evidence channel. It gates what a verdict
	// may conclude, which is why it lives on the Record every verifier reads
	// rather than being checked in one place.
	Ledger *transcript.Ledger
}

// ReasonChannelIncomplete is the reason code for a verdict that could not be
// corroborated because the channel it rests on was not intact.
const ReasonChannelIncomplete = "channel-incomplete"

// gate downgrades a corroborated verdict when the channel it rests on is not
// present.
//
// This is the asymmetry the privacy constraint turns on. A partial or redacted
// channel can still impeach, because a contradiction survives redaction: if a
// command's surviving output shows a failure, the claim is false whatever was
// removed. It can never corroborate, because the evidence that would have
// contradicted the claim may be exactly the evidence that was removed.
// Absence of evidence is not evidence of honesty.
//
// Impeached, uncorroborated and unverifiable verdicts pass through untouched.
// Only corroboration is gated, and only on the channel the verifier actually
// rested on.
func gate(v Verdict, r *Record, c transcript.Channel) Verdict {
	// Impeached passes through untouched. A contradiction from a channel that
	// survived is still a contradiction, and letting redaction erase a
	// verdict would make redaction a way to escape one.
	//
	// Uncorroborated is gated as well as corroborated, and that distinction
	// matters. Uncorroborated means the channel was readable and held nothing
	// either way. On a redacted or partial channel that is the wrong
	// statement: it was not readable, so nothing can be concluded about what
	// it held. That is unverifiable by this tool's own vocabulary.
	if v.Status != Corroborated && v.Status != Uncorroborated {
		return v
	}
	state := r.Ledger.State(c)
	if state.Corroborating() {
		return v
	}
	// An absent channel is already reported as unverifiable by the verifier
	// itself, with a summary naming the channel. Nothing to add.
	if state == transcript.ChannelAbsent && v.Status == Uncorroborated {
		return v
	}

	was := v.Status
	v.Status = Unverifiable
	v.addReason(ReasonChannelIncomplete)
	reason := r.Ledger.Reason(c)
	if reason == "" {
		reason = fmt.Sprintf("the %s channel is %s", c, state)
	}
	if was == Corroborated {
		v.Summary = fmt.Sprintf(
			"The record supports this claim, but the %s channel is %s, so it cannot be corroborated: %s. "+
				"What was removed could be what would have contradicted it.",
			c, state, reason)
	} else {
		v.Summary = fmt.Sprintf(
			"Nothing in the record supports or contradicts this claim, and the %s channel is %s, "+
				"so that silence proves nothing: %s.",
			c, state, reason)
	}
	v.Evidence = append(v.Evidence, Evidence{
		Type:   EvidenceChannel,
		Text:   string(c),
		Detail: string(state),
	})
	return v
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
