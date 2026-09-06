// Package claims turns the agent's own words into checkable statements. It is
// the extractor boundary: the default is a deterministic pattern library, and
// an opt-in model extractor produces the same Claim type so the verifiers
// cannot tell them apart.
package claims

import (
	"fmt"
	"strings"

	"github.com/entireio/cli/impeach/internal/transcript"
)

// Family is the kind of claim, which decides which verifier reads it.
type Family int

const (
	// Execution is a claim that something ran and succeeded.
	Execution Family = iota
	// Structural is a claim that an entity was added, removed or renamed.
	Structural
	// Safety is a claim that a change is contained or compatible.
	Safety
	// Reading is a claim to have looked at something.
	Reading
)

// String names a Family for the report.
func (f Family) String() string {
	switch f {
	case Execution:
		return "execution"
	case Structural:
		return "structural"
	case Safety:
		return "safety"
	case Reading:
		return "reading"
	default:
		return fmt.Sprintf("family(%d)", int(f))
	}
}

// Scope is how wide a claim or a command is.
type Scope int

const (
	// ScopeUnspecified is a claim that does not say how much it covers.
	ScopeUnspecified Scope = iota
	// ScopeAll is a claim about everything: all tests, the whole suite.
	ScopeAll
	// ScopeSubset is a claim about named things.
	ScopeSubset
)

// String names a Scope.
func (s Scope) String() string {
	switch s {
	case ScopeAll:
		return "all"
	case ScopeSubset:
		return "subset"
	default:
		return "unspecified"
	}
}

// Kind narrows an execution claim to the command family that could support it,
// so a claim about the linter is not corroborated by a test run.
type Kind string

const (
	KindTest  Kind = "test"
	KindBuild Kind = "build"
	KindLint  Kind = "lint"
)

// Claim is one checkable statement the agent made.
type Claim struct {
	ID string
	// Text is the exact sentence, quoted verbatim in the report.
	Text   string
	Family Family
	// Kind is set for Execution claims.
	Kind Kind
	// Scope is how much the claim covers.
	Scope Scope
	// Subjects are the things the claim names: symbols, paths or scope words.
	Subjects []string
	// Turn is the assistant turn the claim came from.
	Turn int
	// Seq is the event sequence of the assistant text it came from, which is
	// what staleness ordering uses.
	Seq int
	// Extractor is "pattern" or "model:<cmd>", shown in the report so a
	// reader knows whether a model was involved.
	Extractor string
}

// Extractor produces claims from an event stream.
type Extractor interface {
	// Name identifies the extractor in the report.
	Name() string
	// Extract reads assistant text and returns the claims it found.
	Extract(events []transcript.Event) ([]Claim, error)
}

// Sentences splits assistant text into sentences.
//
// Splitting is deliberately conservative. A claim is quoted verbatim in the
// report, so a wrong split shows up as a mangled quote, which is worse than a
// slightly long one. Abbreviations and version numbers are left alone, and
// list items and newlines count as boundaries because agents write in bullets.
func Sentences(text string) []string {
	var out []string
	var cur strings.Builder

	flush := func() {
		s := strings.TrimSpace(cur.String())
		// Markdown emphasis is formatting, not content. Agents write in bold
		// and bullets, and a claim is quoted verbatim in the report, so a
		// stray ** in the quote reads as a parser bug rather than as
		// testimony.
		s = strings.ReplaceAll(s, "**", "")
		s = strings.ReplaceAll(s, "__", "")
		s = strings.Trim(s, "-*_# \t")
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}

	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		cur.WriteRune(c)

		if c == '\n' {
			flush()
			continue
		}
		if c != '.' && c != '!' && c != '?' {
			continue
		}
		// A terminator ends a sentence only when what follows looks like a
		// new one: whitespace then an upper-case letter, or end of text.
		if i+1 >= len(runes) {
			flush()
			continue
		}
		if !isSpace(runes[i+1]) {
			continue
		}
		// A decimal point inside a number is not a terminator.
		if c == '.' && i > 0 && isDigit(runes[i-1]) && i+2 < len(runes) && isDigit(runes[i+2]) {
			continue
		}
		j := i + 1
		for j < len(runes) && isSpace(runes[j]) {
			j++
		}
		if j >= len(runes) || isUpper(runes[j]) || runes[j] == '`' || runes[j] == '-' || runes[j] == '*' {
			flush()
		}
	}
	flush()
	return out
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }
func isDigit(r rune) bool { return r >= '0' && r <= '9' }
func isUpper(r rune) bool { return r >= 'A' && r <= 'Z' }
