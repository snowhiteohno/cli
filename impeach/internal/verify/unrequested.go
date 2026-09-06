package verify

import (
	"regexp"
	"strings"

	"github.com/entireio/cli/impeach/internal/record"
)

// Severity ranks an unrequested symbol by how much depends on it.
type Severity string

const (
	SeverityNone Severity = "none"
	SeverityLow  Severity = "low"
	SeverityHigh Severity = "high"
)

// Unrequested is an added or signature-changed symbol that no prompt asked
// for. It is row type five: not a claim, and never checked by a model.
type Unrequested struct {
	Symbol string
	File   string
	Kind   record.ChangeKind
	// Dependents is Entire Graph's heuristic dependent count.
	Dependents int
	Severity   Severity
	// IsTest marks a symbol living in a test file, which is usually benign.
	IsTest bool
	// Tokens are the identifier tokens that were searched for, so a reader
	// can see why the match failed.
	Tokens []string
}

// UnrequestedResult is the detector's whole answer, including what it looked
// for, so the report can show its work.
type UnrequestedResult struct {
	Items []Unrequested
	// PromptTokens is the mention corpus, for display.
	PromptTokens []string
	// Skipped is set when the detector could not run at all, which happens
	// when there are no prompts to compare against. Missing data is a state.
	Skipped bool
	Reason  string
}

// stopwords are words too common to carry a mention. Deliberately short: the
// detector is meant to be conservative about flagging, which means being
// generous about what counts as a mention.
var stopwords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "that": true, "this": true,
	"from": true, "into": true, "get": true, "set": true, "new": true, "old": true,
	"add": true, "use": true, "run": true, "all": true, "any": true, "one": true,
	"not": true,
}

// tokenSplit splits an identifier into its parts on case boundaries,
// underscores, hyphens and dots.
var tokenSplit = regexp.MustCompile(`[_\-.]+|(?:[a-z0-9])(?:[A-Z])`)

// wordRe finds words in prompt text.
var wordRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// DetectUnrequested finds added or signature-changed symbols that no prompt
// mentions.
//
// Body-only changes are never candidates, which is the design's deliberate
// conservatism: unrequested behaviour hidden inside an existing function will
// not be caught, and that is stated as a limitation rather than guessed at.
func DetectUnrequested(changes *record.CommitChanges, prompts []string) *UnrequestedResult {
	out := &UnrequestedResult{}

	if changes == nil {
		out.Skipped = true
		out.Reason = "Entire Graph produced no entity diff, so there is nothing to check against the prompts"
		return out
	}
	if len(prompts) == 0 {
		// Without prompts there is no notion of what was asked for, so the
		// detector reports that rather than flagging everything.
		out.Skipped = true
		out.Reason = "the transcript carries no prompts, so there is no record of what was asked for"
		return out
	}

	corpus, corpusTokens := mentionCorpus(prompts)
	out.PromptTokens = corpusTokens

	for _, ch := range changes.Changes {
		if ch.Kind != record.Added && ch.Kind != record.SignatureChanged {
			continue
		}
		tokens := identifierTokens(ch.Name)
		if mentioned(ch, tokens, corpus) {
			continue
		}
		out.Items = append(out.Items, Unrequested{
			Symbol:     ch.Name,
			File:       ch.Path,
			Kind:       ch.Kind,
			Dependents: ch.Dependents,
			Severity:   severityFor(ch.Dependents),
			IsTest:     ch.IsTestFile(),
			Tokens:     tokens,
		})
	}
	// Non-test symbols first, then by descending dependent count. A test the
	// agent added is far less interesting than a production symbol nobody
	// asked for, and the ordering says so without hiding either.
	items := out.Items
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && lessUnrequested(items[j], items[j-1]); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
	return out
}

func lessUnrequested(a, b Unrequested) bool {
	if a.IsTest != b.IsTest {
		return !a.IsTest
	}
	if a.Dependents != b.Dependents {
		return a.Dependents > b.Dependents
	}
	return a.Symbol < b.Symbol
}

// mentioned reports whether a prompt asked for this symbol.
//
// Three ways count, in decreasing strength: the exact name appears; every one
// of its identifier tokens appears somewhere; or its file's base name is
// named. Any of them is enough, because the cost of a false flag is higher
// than the cost of a miss.
func mentioned(ch record.EntityChange, tokens []string, corpus map[string]bool) bool {
	if corpus[strings.ToLower(ch.Name)] {
		return true
	}
	if len(tokens) > 0 {
		all := true
		for _, t := range tokens {
			if !corpus[t] {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	base := baseName(ch.Path)
	if base != "" && corpus[strings.ToLower(base)] {
		return true
	}
	// Also the base name without its extension.
	if i := strings.LastIndexByte(base, '.'); i > 0 {
		if corpus[strings.ToLower(base[:i])] {
			return true
		}
	}
	return false
}

// mentionCorpus builds the set of words the prompts contain.
func mentionCorpus(prompts []string) (map[string]bool, []string) {
	corpus := map[string]bool{}
	var ordered []string
	for _, p := range prompts {
		lower := strings.ToLower(p)
		for _, w := range wordRe.FindAllString(lower, -1) {
			if !corpus[w] {
				corpus[w] = true
				ordered = append(ordered, w)
			}
			// An identifier in a prompt also contributes its parts, so
			// "compute_total" in a prompt mentions "compute" and "total".
			for _, t := range identifierTokens(w) {
				if !corpus[t] {
					corpus[t] = true
					ordered = append(ordered, t)
				}
			}
		}
	}
	return corpus, ordered
}

// identifierTokens splits a symbol name into lower-case parts worth matching,
// dropping parts shorter than three characters and common words.
func identifierTokens(name string) []string {
	name = strings.TrimLeft(name, "_")
	// Insert separators at case boundaries, then split on everything.
	var b strings.Builder
	runes := []rune(name)
	for i, r := range runes {
		if i > 0 && r >= 'A' && r <= 'Z' && (runes[i-1] < 'A' || runes[i-1] > 'Z') {
			b.WriteByte('_')
		}
		b.WriteRune(r)
	}
	parts := strings.FieldsFunc(b.String(), func(r rune) bool {
		return r == '_' || r == '-' || r == '.'
	})

	seen := map[string]bool{}
	var out []string
	for _, p := range parts {
		p = strings.ToLower(p)
		if len(p) < 3 || stopwords[p] || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// severityFor ranks by dependent count.
func severityFor(dependents int) Severity {
	switch {
	case dependents == 0:
		return SeverityNone
	case dependents < 5:
		return SeverityLow
	default:
		return SeverityHigh
	}
}
