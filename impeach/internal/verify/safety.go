package verify

import (
	"fmt"
	"strings"

	"github.com/entireio/cli/impeach/internal/claims"
	"github.com/entireio/cli/impeach/internal/record"
	"github.com/entireio/cli/impeach/internal/transcript"
)

// Safety verifies claims that a change is contained or compatible.
type Safety struct{}

// Family implements Verifier.
func (Safety) Family() claims.Family { return claims.Safety }

// Verify implements Verifier.
//
// Two readings are checked. A containment claim ("no other callers") is about
// callers. A compatibility claim ("backward compatible") is about signatures.
// A sentence can be both, and then both are checked.
func (s Safety) Verify(c claims.Claim, r *Record) Verdict {
	v := Verdict{ClaimID: c.ID, Rerun: rerunStatus(r)}

	subject, impact := s.resolve(c, r)
	if subject == "" {
		v.Status = Uncorroborated
		v.Summary = "The claim names no symbol that could be looked up, so there is nothing to trace."
		return v
	}
	if impact == nil {
		v.Status = Unverifiable
		v.Summary = fmt.Sprintf("Entire Graph was not asked about %s, so its blast radius is unknown.", subject)
		return v
	}
	if impact.Ambiguous {
		v.Status = Unverifiable
		v.Summary = fmt.Sprintf("%s matches several definitions, so Entire Graph returned the definition list rather than a blast radius.", subject)
		return v
	}
	if !impact.Resolved {
		v.Status = Unverifiable
		v.Summary = fmt.Sprintf("Entire Graph could not resolve %s, so nothing can be said about what calls it.", subject)
		return v
	}

	v.Evidence = append(v.Evidence, impactEvidence(subject, impact))
	change, changed := s.signatureChange(subject, r)
	if changed {
		v.Evidence = append(v.Evidence, entityEvidence(change))
	}

	// Which question to ask depends on what the claim actually asserted.
	switch c.SafetyKind {
	case claims.SafetyCompatibility:
		// A compatibility claim is about whether dependents still work. Only
		// a signature change on a symbol that has callers contradicts it.
		// Callers alone do not: a symbol being called is the normal case, and
		// impeaching a claim of "no behaviour change" for having callers is a
		// false accusation.
		if changed && len(impact.Callers) > 0 {
			v.Status = Impeached
			v.addReason(ReasonSignatureChanged)
			v.Summary = fmt.Sprintf("The signature of %s changed to %s and Entire Graph finds %d %s, so dependents cannot be assumed unaffected.",
				subject, change.NewSig, len(impact.Callers), callerWord(len(impact.Callers)))
			return v
		}
		v.Status = Corroborated
		if changed {
			v.Summary = fmt.Sprintf("The signature of %s changed, and Entire Graph finds no callers outside the changed set.", subject)
		} else {
			v.Summary = fmt.Sprintf("The signature of %s did not change, so its %d %s keep the same contract.",
				subject, len(impact.Callers), callerWord(len(impact.Callers)))
		}
		return gate(v, r, transcript.ChannelGraph)

	default:
		// A containment claim is about who depends on the symbol, so any
		// caller contradicts it.
		if len(impact.Callers) > 0 {
			v.Status = Impeached
			v.addReason(ReasonCallersExist)
			if changed {
				v.addReason(ReasonSignatureChanged)
			}
			v.Summary = s.summarize(subject, impact, v.Reasons)
			return v
		}
		v.Status = Corroborated
		v.Summary = fmt.Sprintf("Entire Graph finds no callers of %s outside the changed set.", subject)
		return gate(v, r, transcript.ChannelGraph)
	}
}

// resolve picks the symbol a safety claim is about and returns its impact.
//
// A claim usually names its subject. When it does not, the symbols the
// checkpoint actually changed are used, because "this change is isolated"
// with no subject is a claim about the change itself.
func (s Safety) resolve(c claims.Claim, r *Record) (string, *record.Impact) {
	for _, subj := range c.Subjects {
		name := symbolName(subj)
		if imp, ok := r.Impacts[name]; ok {
			return name, imp
		}
	}
	// Named but never looked up.
	if len(c.Subjects) > 0 {
		return symbolName(c.Subjects[0]), nil
	}
	// Fall back to the changed symbols, preferring a signature change since
	// that is what a compatibility claim is about.
	if r.Changes != nil {
		for _, ch := range r.Changes.Changes {
			if ch.Kind == record.SignatureChanged {
				if imp, ok := r.Impacts[ch.Name]; ok {
					return ch.Name, imp
				}
				return ch.Name, nil
			}
		}
		for _, ch := range r.Changes.Changes {
			if imp, ok := r.Impacts[ch.Name]; ok {
				return ch.Name, imp
			}
		}
	}
	return "", nil
}

// signatureChange finds a signature change for the subject, if there was one.
func (s Safety) signatureChange(subject string, r *Record) (record.EntityChange, bool) {
	if r.Changes == nil {
		return record.EntityChange{}, false
	}
	for _, ch := range r.Changes.FindSymbol(subject) {
		if ch.Kind == record.SignatureChanged {
			return ch, true
		}
	}
	return record.EntityChange{}, false
}

func (s Safety) summarize(subject string, impact *record.Impact, reasons []string) string {
	var parts []string
	for _, code := range reasons {
		switch code {
		case ReasonCallersExist:
			parts = append(parts, fmt.Sprintf("Entire Graph finds %d %s of %s (%s)",
				len(impact.Callers), callerWord(len(impact.Callers)), subject, callerList(impact)))
		case ReasonSignatureChanged:
			parts = append(parts, "and its signature changed")
		}
	}
	return capitalize(strings.Join(parts, " ")) + "."
}

func callerWord(n int) string {
	if n == 1 {
		return "caller"
	}
	return "callers"
}

func callerList(impact *record.Impact) string {
	names := make([]string, 0, len(impact.Callers))
	for _, c := range impact.Callers {
		names = append(names, fmt.Sprintf("%s at %s:%d", c.Name, c.Path, c.Line))
	}
	if len(names) > 4 {
		names = append(names[:4], fmt.Sprintf("and %d more", len(impact.Callers)-4))
	}
	return strings.Join(names, ", ")
}

func impactEvidence(subject string, impact *record.Impact) Evidence {
	detail := fmt.Sprintf("%d callers (%d direct, %d transitive), %d type consumers",
		len(impact.Callers), impact.DirectCallers, impact.TransitiveCallers, impact.TypeConsumers)
	return Evidence{
		Type:    EvidenceImpact,
		Text:    fmt.Sprintf("%s (%s:%d)", subject, impact.Path, impact.Line),
		Detail:  detail,
		Command: fmt.Sprintf("entire graph impact --symbol %s --format json --exclude-tests", subject),
	}
}
