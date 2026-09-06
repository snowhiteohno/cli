package verify

import (
	"fmt"
	"strings"

	"github.com/entireio/cli/impeach/internal/claims"
	"github.com/entireio/cli/impeach/internal/transcript"
)

// Reading verifies claims to have looked at something.
type Reading struct{}

// Family implements Verifier.
func (Reading) Family() claims.Family { return claims.Reading }

// Verify implements Verifier.
//
// Two shapes are checked. A claim naming a file is corroborated by a read of
// that file. A claim about "the callers" is corroborated by a read of any file
// Entire Graph says contains a caller, which is why the record caches impact
// results.
func (rd Reading) Verify(c claims.Claim, r *Record) Verdict {
	v := Verdict{ClaimID: c.ID, Rerun: rerunStatus(r)}

	if !r.Stream.Channels().Reads {
		v.Status = Unverifiable
		v.Summary = "This transcript carries no read records, so there is no way to check what was looked at."
		return v
	}

	readPaths := rd.reads(r)

	// A claim naming files or symbols.
	if len(c.Subjects) > 0 {
		var matched, unmatched []string
		for _, subj := range c.Subjects {
			if p, ok := matchRead(subj, readPaths); ok {
				matched = append(matched, p)
			} else {
				unmatched = append(unmatched, subj)
			}
		}
		if len(matched) > 0 {
			v.Status = Corroborated
			v.Summary = fmt.Sprintf("The session read %s.", strings.Join(matched, ", "))
			for _, p := range matched {
				v.Evidence = append(v.Evidence, rd.readEvidence(r, p))
			}
			// A reading claim rests entirely on the read events, so an
			// incomplete read channel cannot confirm one.
			return gate(v, r, transcript.ChannelReads)
		}
		// Named something resolvable and never read it.
		if rd.anyResolvable(unmatched, r) {
			v.Status = Impeached
			v.addReason(ReasonNeverRead)
			v.Summary = fmt.Sprintf("Nothing in the session's read records matches %s.", strings.Join(unmatched, ", "))
			v.Evidence = append(v.Evidence, Evidence{
				Type: EvidenceRead, Text: strings.Join(readPaths, ", "), Detail: "every file the session read",
			})
			return v
		}
	}

	// A claim about the callers, checked against the caller files Graph found.
	if callerFiles := rd.callerFiles(r); len(callerFiles) > 0 && claims.NamesReadingObject(c.Text) {
		for _, cf := range callerFiles {
			if p, ok := matchRead(cf, readPaths); ok {
				v.Status = Corroborated
				v.Summary = fmt.Sprintf("The session read %s, which Entire Graph lists as containing a caller.", p)
				v.Evidence = append(v.Evidence, rd.readEvidence(r, p))
				return gate(v, r, transcript.ChannelReads)
			}
		}
		v.Status = Impeached
		v.addReason(ReasonNeverRead)
		v.Summary = fmt.Sprintf("Entire Graph lists %s as containing callers, and the session read none of them.",
			strings.Join(callerFiles, ", "))
		v.Evidence = append(v.Evidence, Evidence{
			Type: EvidenceRead, Text: strings.Join(readPaths, ", "), Detail: "every file the session read",
		})
		return v
	}

	v.Status = Uncorroborated
	v.Summary = "The claim names nothing the record can resolve, so there is nothing to match against the read records."
	return v
}

func (rd Reading) reads(r *Record) []string {
	seen := map[string]bool{}
	var out []string
	for i := range r.Stream.Events {
		e := &r.Stream.Events[i]
		if e.Kind == transcript.FileRead && e.Path != "" && !seen[e.Path] {
			seen[e.Path] = true
			out = append(out, e.Path)
		}
	}
	return out
}

func (rd Reading) callerFiles(r *Record) []string {
	seen := map[string]bool{}
	var out []string
	for _, imp := range r.Impacts {
		for _, f := range imp.CallerFiles() {
			if !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
	}
	return out
}

// anyResolvable reports whether at least one unmatched subject is something
// the record could have shown, so a claim about a file that exists is
// impeachable while a claim about "the code" is merely uncorroborated.
func (rd Reading) anyResolvable(subjects []string, r *Record) bool {
	for _, s := range subjects {
		if strings.ContainsAny(s, "/.") {
			return true
		}
		if r.Changes != nil && len(r.Changes.FindSymbol(symbolName(s))) > 0 {
			return true
		}
		for _, imp := range r.Impacts {
			if imp.Symbol == symbolName(s) {
				return true
			}
		}
	}
	return false
}

func (rd Reading) readEvidence(r *Record, path string) Evidence {
	for i := range r.Stream.Events {
		e := &r.Stream.Events[i]
		if e.Kind == transcript.FileRead && e.Path == path {
			return Evidence{Type: EvidenceRead, Seq: e.Seq, Text: path, Detail: "read in the session"}
		}
	}
	return Evidence{Type: EvidenceRead, Text: path, Detail: "read in the session"}
}

// matchRead reports whether a subject corresponds to something read.
//
// Matching is by suffix in both directions, because a claim may name a bare
// file name while the record holds a repository-relative path, or the other
// way round.
func matchRead(subject string, reads []string) (string, bool) {
	subj := strings.TrimPrefix(subject, "./")
	for _, p := range reads {
		if p == subj || strings.HasSuffix(p, "/"+subj) || strings.HasSuffix(subj, "/"+p) {
			return p, true
		}
		// A bare symbol name is matched against the file that holds it only
		// when the claim named a path; a symbol alone is handled by the
		// caller-files branch.
		if base := baseName(p); base == subj {
			return p, true
		}
	}
	return "", false
}

func baseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}
