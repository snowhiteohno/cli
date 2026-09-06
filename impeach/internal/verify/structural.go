package verify

import (
	"fmt"
	"strings"

	"github.com/entireio/cli/impeach/internal/claims"
	"github.com/entireio/cli/impeach/internal/record"
)

// Structural verifies claims that an entity was added, removed or renamed.
type Structural struct{}

// Family implements Verifier.
func (Structural) Family() claims.Family { return claims.Structural }

// wantedKinds maps a claim's verb onto the entity change kinds that would
// support it.
var wantedKinds = map[claims.Kind][]record.ChangeKind{
	"added":   {record.Added},
	"removed": {record.Removed},
	"renamed": {record.Renamed},
}

// Verify implements Verifier.
func (s Structural) Verify(c claims.Claim, r *Record) Verdict {
	v := Verdict{ClaimID: c.ID, Rerun: rerunStatus(r)}

	// The entity diff is the only channel here. Without it the claim is
	// unverifiable, not wrong.
	if r.Changes == nil {
		v.Status = Unverifiable
		v.Summary = "Entire Graph produced no entity diff for this commit, so there is no record of what was added or removed."
		return v
	}

	subjects := c.Subjects
	if len(subjects) == 0 {
		v.Status = Uncorroborated
		v.Summary = "The claim names no identifier that could be looked up in the entity diff."
		return v
	}

	want := wantedKinds[c.Kind]

	for _, subj := range subjects {
		name := symbolName(subj)
		changes := r.Changes.FindSymbol(name)
		if len(changes) == 0 {
			continue
		}
		for _, ch := range changes {
			if matchesKind(ch.Kind, want) {
				v.Status = Corroborated
				v.Summary = fmt.Sprintf("Entire Graph lists %s %s as %s in %s.",
					ch.SymbolKind, ch.Name, ch.Kind, ch.Path)
				v.Evidence = append(v.Evidence, entityEvidence(ch))
				return v
			}
		}
		// The name is in the diff but with a different kind of change. That
		// is a contradiction, not an absence.
		v.Status = Impeached
		v.addReason(ReasonNotInDiff)
		v.Summary = fmt.Sprintf("The claim says %s was %s, but Entire Graph lists it as %s.",
			name, c.Kind, changes[0].Kind)
		v.Evidence = append(v.Evidence, entityEvidence(changes[0]))
		return v
	}

	// Nothing matched. Whether that impeaches the claim depends on whether
	// Graph actually parsed the files involved: an absence in a file Graph
	// never read is not evidence of anything.
	if len(r.Changes.Files) == 0 {
		v.Status = Unverifiable
		v.Summary = "Entire Graph parsed no files in this commit, so an absent entity proves nothing."
		return v
	}

	v.Status = Impeached
	v.addReason(ReasonNotInDiff)
	v.Summary = fmt.Sprintf("%s does not appear in the entity diff for this commit, and Entire Graph parsed %s.",
		strings.Join(subjects, ", "), filesPhrase(len(r.Changes.Files)))
	for _, f := range r.Changes.Files {
		v.Evidence = append(v.Evidence, Evidence{
			Type: EvidenceEntity, Text: f, Detail: "parsed by Entire Graph, no matching entity",
		})
	}
	return v
}

// matchesKind reports whether an observed change supports the claimed verb.
//
// An empty want list means the claim's verb was not one of the three the
// library recognises, so any change to the named entity supports it.
func matchesKind(got record.ChangeKind, want []record.ChangeKind) bool {
	if len(want) == 0 {
		return true
	}
	for _, w := range want {
		if got == w {
			return true
		}
	}
	// An added symbol satisfies a claim to have written a test for it, and a
	// signature change satisfies a claim to have changed it.
	return false
}

// symbolName reduces a path-qualified or dotted reference to the bare symbol
// name Graph reports.
func symbolName(subject string) string {
	s := subject
	if i := strings.LastIndex(s, "::"); i >= 0 {
		s = s[i+2:]
	}
	if i := strings.LastIndexByte(s, '.'); i >= 0 && !strings.ContainsAny(s[i:], "/") {
		// Keep a file extension, drop a dotted attribute path.
		if ext := s[i:]; ext != ".py" && ext != ".go" && ext != ".js" && ext != ".ts" && ext != ".rb" && ext != ".rs" {
			s = s[i+1:]
		}
	}
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		s = s[i+1:]
	}
	return s
}

func entityEvidence(ch record.EntityChange) Evidence {
	detail := string(ch.Kind)
	if ch.OldSig != "" && ch.NewSig != "" {
		detail = fmt.Sprintf("%s: %s -> %s", ch.Kind, ch.OldSig, ch.NewSig)
	}
	return Evidence{
		Type:   EvidenceEntity,
		Text:   fmt.Sprintf("%s %s (%s:%d)", ch.SymbolKind, ch.Name, ch.Path, ch.Line),
		Detail: detail,
	}
}

func filesPhrase(n int) string {
	if n == 1 {
		return "the one file it changed"
	}
	return fmt.Sprintf("all %d changed files", n)
}

func rerunStatus(r *Record) record.RerunStatus {
	if r.Rerun == nil {
		return record.RerunNotRun
	}
	return r.Rerun.Status
}
