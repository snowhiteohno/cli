// Package report renders a completed audit. The renderers are the fourth
// boundary: table, json and html all read the same Report struct, so the
// surfaces cannot disagree about what was found.
package report

import (
	"github.com/entireio/cli/impeach/internal/claims"
	"github.com/entireio/cli/impeach/internal/record"
	"github.com/entireio/cli/impeach/internal/verify"
)

// Version is the report schema and tool version.
const Version = "0.1.0"

// Report is one audit.
type Report struct {
	Version    string
	Checkpoint Checkpoint
	Inputs     Inputs
	Rows       []Row
	// Unrequested is row type five: added or signature-changed symbols no
	// prompt asked for.
	Unrequested *verify.UnrequestedResult
	Counts      Counts
	// Notes are conditions a reader must know to read the table correctly:
	// a missing channel, a degraded rerun, a merge commit.
	Notes []string
	// Limitations are the standing caveats, always shown.
	Limitations []string
	// CommandsRun is every external command the audit ran, so a reader can
	// reproduce any line without trusting the report.
	CommandsRun []string
}

// Checkpoint identifies what was audited.
type Checkpoint struct {
	ID         string
	Commit     string
	Parent     string
	Agent      string
	SessionIDs []string
	IsMerge    bool
}

// Inputs records how the audit was configured.
type Inputs struct {
	Adapter      string
	Extractors   []string
	TestCommand  string
	Rerun        bool
	ModelCommand string
	Channels     map[string]bool
}

// Row is one claim and its verdict.
type Row struct {
	Claim   claims.Claim
	Verdict verify.Verdict
}

// Counts is the summary strip.
type Counts struct {
	Corroborated   int
	Impeached      int
	Uncorroborated int
	Unverifiable   int
	Unrequested    int
}

// New assembles a report from the parts an audit produced.
func New(cp Checkpoint, in Inputs, rows []Row, un *verify.UnrequestedResult,
	notes, limitations, commands []string) *Report {
	r := &Report{
		Version:     Version,
		Checkpoint:  cp,
		Inputs:      in,
		Rows:        rows,
		Unrequested: un,
		Notes:       notes,
		Limitations: limitations,
		CommandsRun: commands,
	}
	if un != nil {
		r.Counts.Unrequested = len(un.Items)
	}
	for _, row := range rows {
		switch row.Verdict.Status {
		case verify.Corroborated:
			r.Counts.Corroborated++
		case verify.Impeached:
			r.Counts.Impeached++
		case verify.Uncorroborated:
			r.Counts.Uncorroborated++
		case verify.Unverifiable:
			r.Counts.Unverifiable++
		}
	}
	r.Sort()
	return r
}

// FamilyLabel names a claim's family, marking anything a model produced.
//
// The PRD requires model-generated claims to be identifiable in the report.
// A reader has to be able to tell at a glance which rows a third party
// suggested, because the deterministic library and a model are not equally
// trustworthy about what was even claimed. The verdict itself is unaffected:
// the record decides that either way.
func FamilyLabel(c claims.Claim) string {
	if c.Extractor == "" || c.Extractor == "pattern" {
		return c.Family.String()
	}
	return c.Family.String() + " (model)"
}

// statusRank orders the table so the impeachments are read first.
func statusRank(s verify.Status) int {
	switch s {
	case verify.Impeached:
		return 0
	case verify.Uncorroborated:
		return 1
	case verify.Unverifiable:
		return 2
	default:
		return 3
	}
}

// Sort puts impeached rows first, then uncorroborated, unverifiable and
// corroborated; within a group, by turn then by sequence.
func (r *Report) Sort() {
	rows := r.Rows
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && less(rows[j], rows[j-1]); j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}

func less(a, b Row) bool {
	ra, rb := statusRank(a.Verdict.Status), statusRank(b.Verdict.Status)
	if ra != rb {
		return ra < rb
	}
	if a.Claim.Turn != b.Claim.Turn {
		return a.Claim.Turn < b.Claim.Turn
	}
	return a.Claim.Seq < b.Claim.Seq
}

// Lead returns the claim that should headline the report: the impeached claim
// with the most reasons, ties broken by a rerun with new failures, then by the
// earliest turn.
func (r *Report) Lead() (Row, bool) {
	var best Row
	found := false
	for _, row := range r.Rows {
		if row.Verdict.Status != verify.Impeached {
			continue
		}
		if !found || leadBetter(row, best) {
			best, found = row, true
		}
	}
	return best, found
}

func leadBetter(a, b Row) bool {
	if len(a.Verdict.Reasons) != len(b.Verdict.Reasons) {
		return len(a.Verdict.Reasons) > len(b.Verdict.Reasons)
	}
	an := a.Verdict.Rerun == record.RerunNewFailures
	bn := b.Verdict.Rerun == record.RerunNewFailures
	if an != bn {
		return an
	}
	return a.Claim.Turn < b.Claim.Turn
}
