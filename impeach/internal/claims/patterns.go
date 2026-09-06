package claims

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/entireio/cli/impeach/internal/transcript"
)

// pattern is one entry in the library.
//
// Patterns are anchored on verbs and objects, not on tone. An agent hedging
// ("I think the tests pass") is making the same checkable claim as one that
// does not, and confidence is not the thing being audited.
type pattern struct {
	name string
	re   *regexp.Regexp
	// kind is the command family that could support the claim.
	kind Kind
	// negated marks a pattern that denies rather than asserts, so
	// "the tests do not pass" is not read as a claim that they do.
	negated bool
	// safety is set on safety patterns and says which question to ask.
	safety SafetyKind
}

// executionPatterns is the starter library for claims that something ran and
// succeeded.
var executionPatterns = []pattern{
	{name: "all-tests-pass", kind: KindTest, re: regexp.MustCompile(
		`(?i)\b(all|the|every|both)\s+(of\s+the\s+)?(unit\s+|integration\s+|remaining\s+)?tests?\s+(now\s+|still\s+)?(pass|passes|passing|are\s+passing|are\s+green|green)\b`)},
	{name: "suite-green", kind: KindTest, re: regexp.MustCompile(
		`(?i)\b(the\s+)?(test\s+)?suite\s+(now\s+)?(passes|is\s+green|is\s+passing)\b`)},
	{name: "tests-pass-bare", kind: KindTest, re: regexp.MustCompile(
		`(?i)^\s*tests?\s+(now\s+)?(pass|passing|are\s+green)\b`)},
	{name: "ran-tests", kind: KindTest, re: regexp.MustCompile(
		`(?i)\b(i\s+)?(ran|run|re-?ran|executed)\s+(the\s+|all\s+the\s+)?(unit\s+|integration\s+)?tests?\b`)},
	{name: "n-tests-passed", kind: KindTest, re: regexp.MustCompile(
		`(?i)\b\d+\s+(tests?\s+)?(passed|passing)\b`)},
	{name: "tests-still-pass", kind: KindTest, re: regexp.MustCompile(
		`(?i)\btests?\s+(still|continue\s+to)\s+pass\b`)},
	{name: "named-target-passes", kind: KindTest, re: regexp.MustCompile(
		`(?i)\b(pass|passes|passed|passing)\b[^.]{0,40}?\b\d+\s+tests?\b|\b\d+\s+tests?\b[^.]{0,40}?\b(pass|passes|passed|passing)\b`)},
	{name: "no-failures", kind: KindTest, re: regexp.MustCompile(
		`(?i)\b(no|zero)\s+(test\s+)?(failures|failing\s+tests)\b`)},
	{name: "verified-tests", kind: KindTest, re: regexp.MustCompile(
		`(?i)\b(verified|confirmed)\s+(that\s+)?(the\s+)?tests?\s+(pass|passing)\b`)},

	{name: "build-succeeds", kind: KindBuild, re: regexp.MustCompile(
		`(?i)\b(the\s+)?build\s+(succeeds|passes|is\s+clean|works|is\s+green)\b`)},
	{name: "compiles", kind: KindBuild, re: regexp.MustCompile(
		`(?i)\b(it\s+|this\s+|the\s+code\s+)?(compiles|builds)\s+(cleanly|successfully|without\s+errors?|fine)\b`)},

	{name: "lint-clean", kind: KindLint, re: regexp.MustCompile(
		`(?i)\b(the\s+)?lint(er|ing)?\s+(passes|is\s+clean|is\s+happy|reports\s+nothing)\b`)},
	{name: "no-lint-errors", kind: KindLint, re: regexp.MustCompile(
		`(?i)\b(no|zero)\s+lint(ing)?\s+(errors?|warnings?|issues?)\b`)},
	{name: "vet-clean", kind: KindLint, re: regexp.MustCompile(
		`(?i)\b(go\s+)?vet\s+(passes|is\s+clean|reports\s+nothing)\b`)},
}

// structuralPatterns match claims that an entity was added, removed or
// renamed. The subject is an identifier, so these lean on backticked names
// and on CamelCase or snake_case words rather than on prose.
var structuralPatterns = []pattern{
	{name: "added", re: regexp.MustCompile(
		`(?i)\b(added|created|introduced|wrote|implemented)\b`)},
	{name: "removed", re: regexp.MustCompile(
		`(?i)\b(removed|deleted|dropped|took\s+out)\b`)},
	{name: "renamed", re: regexp.MustCompile(
		`(?i)\brenamed\b`)},
}

// safetyPatterns match claims that a change is contained or compatible. The
// safety field decides which question the verifier asks of the record.
var safetyPatterns = []pattern{
	{name: "no-other-callers", safety: SafetyContainment, re: regexp.MustCompile(
		`(?i)\b(no|not\s+any|zero)\s+other\s+(callers?|call\s+sites?|usages?|users?)\b`)},
	{name: "nothing-else-calls", safety: SafetyContainment, re: regexp.MustCompile(
		`(?i)\bnothing\s+else\s+(calls|uses|references|depends\s+on)\b`)},
	{name: "only-used-in", safety: SafetyContainment, re: regexp.MustCompile(
		`(?i)\bonly\s+(used|called|referenced)\s+(in|by|from)\b`)},
	{name: "no-callers", safety: SafetyContainment, re: regexp.MustCompile(
		`(?i)\b(has|have)\s+no\s+(callers?|call\s+sites?|usages?)\b`)},
	{name: "backward-compatible", safety: SafetyCompatibility, re: regexp.MustCompile(
		`(?i)\bbackward[s]?\s+compatible\b|\bfully\s+compatible\b`)},
	{name: "no-behaviour-change", safety: SafetyCompatibility, re: regexp.MustCompile(
		`(?i)\bno\s+behaviou?r(al)?\s+change\b|\bbehaviou?r\s+is\s+unchanged\b`)},
	{name: "isolated", safety: SafetyContainment, re: regexp.MustCompile(
		`(?i)\b(fully|completely|entirely)?\s*isolated\b|\bself[\s-]contained\b|\bcontained\s+change\b`)},
	{name: "safe-to-change", safety: SafetyCompatibility, re: regexp.MustCompile(
		`(?i)\bsafe\s+to\s+(merge|change|ship|land|refactor)\b`)},
	{name: "unaffected", safety: SafetyCompatibility, re: regexp.MustCompile(
		`(?i)\b(is|are|remain|remains|stay|stays)\s+unaffected\b|\bnot\s+affected\b|\bunaffected\s+by\b`)},
	{name: "nothing-breaks", safety: SafetyCompatibility, re: regexp.MustCompile(
		`(?i)\bnothing\s+(else\s+)?(breaks|will\s+break)\b|\bwon't\s+break\s+anything\b`)},
}

// readingPatterns match claims to have looked at something.
var readingPatterns = []pattern{
	{name: "reviewed", re: regexp.MustCompile(
		`(?i)\b(i\s+)?(reviewed|checked|inspected|examined|looked\s+at|read|went\s+through|studied)\b`)},
	{name: "confirmed-by-reading", re: regexp.MustCompile(
		`(?i)\b(verified|confirmed)\s+(that\s+)?(the\s+)?(callers?|call\s+sites?|tests?|usages?)\b`)},
}

// readingObjects are the things a reading claim has to name to be checkable.
// A claim to have "looked at the code" names nothing resolvable and is
// uncorroborated rather than impeached.
var readingObjects = regexp.MustCompile(
	`(?i)\b(callers?|call\s+sites?|usages?|tests?|test\s+files?)\b`)

// negations disqualify a sentence before any pattern is tried. A sentence that
// denies the claim, states a condition, or asks the reader to do the work is
// not testimony that the thing happened.
var negations = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(do|does|did|will|would|should|could|might|may)\s+not\b`),
	regexp.MustCompile(`(?i)\b(don't|doesn't|didn't|won't|can't|cannot|couldn't|shouldn't|wouldn't)\b`),
	regexp.MustCompile(`(?i)\b(no|not)\s+(longer|yet)\b`),
	// A leading or quantifier "not" flips the claim: "Not all tests pass" is
	// the opposite of the claim the pattern would otherwise match.
	regexp.MustCompile(`(?i)^\s*not\b`),
	regexp.MustCompile(`(?i)\bnot\s+(all|every|everything)\b`),
	// An instruction or a suggestion, not a report.
	regexp.MustCompile(`(?i)\b(you\s+should|please|make\s+sure|worth|before\s+this|remember\s+to|need\s+to|make\s+certain)\b`),
	// A question is not a claim.
	regexp.MustCompile(`\?\s*$`),
	// A conditional is not a report of what happened.
	regexp.MustCompile(`(?i)^\s*(if|once|unless|when|assuming|provided)\b`),
	regexp.MustCompile(`(?i)\b(i\s+did\s+not|i\s+have\s+not|i\s+haven't|i\s+didn't)\b`),
}

// failureWordRe spots words that usually mean something went wrong.
var failureWordRe = regexp.MustCompile(`(?i)\b(fail|fails|failed|failing|failure|failures|broke|broken|breaks)\b`)

// noFailureRe spots the affirmative use of a failure word. "Zero failing
// tests" and "no test failures" are claims that things passed, so a blanket
// ban on the word "failing" would silently drop them.
var noFailureRe = regexp.MustCompile(`(?i)\b(no|zero|without|nothing)\s+(\w+\s+){0,2}(failures?|failing|errors?|issues?|warnings?)\b`)

// scopeAllRe marks a claim as covering everything.
var scopeAllRe = regexp.MustCompile(`(?i)\b(all|every|entire|whole|full|complete|everything)\b`)

// scopeSubsetRe captures a named subset, as in "for the refund path" or
// "in tests/test_api.py".
var scopeSubsetRe = regexp.MustCompile(`(?i)\b(?:for|in|on|of|within)\s+(?:the\s+)?` + "`?" + `([\w./:-]+)` + "`?")

// backtickedRe pulls out identifiers and paths the agent quoted, which are the
// most reliable subjects available.
var backtickedRe = regexp.MustCompile("`([^`]+)`")

// pathLikeRe finds bare paths and node ids in a sentence.
var pathLikeRe = regexp.MustCompile(`\b([\w./-]+\.(?:py|go|js|jsx|ts|tsx|rb|rs|java|php)(?:::[\w:]+)?)\b`)

// Pattern is the deterministic extractor. It is the default, and the only one
// that runs offline with no model.
type Pattern struct{}

// Name implements Extractor.
func (Pattern) Name() string { return "pattern" }

// Extract implements Extractor.
//
// Only assistant text is read. Prompts are what the user asked for, not what
// the agent claimed, and thinking blocks were dropped by the adapter because
// reasoning is not testimony.
func (p Pattern) Extract(events []transcript.Event) ([]Claim, error) {
	var out []Claim
	n := 0
	for i := range events {
		e := &events[i]
		if e.Kind != transcript.AssistantText {
			continue
		}
		for _, sentence := range Sentences(e.Text) {
			if isNegated(sentence) {
				continue
			}
			c, ok := p.classify(sentence)
			if !ok {
				continue
			}
			n++
			c.ID = fmt.Sprintf("c%d", n)
			c.Text = sentence
			c.Turn = e.Turn
			c.Seq = e.Seq
			c.Extractor = p.Name()
			out = append(out, c)
		}
	}
	return out, nil
}

// classify decides which family a sentence belongs to, if any.
//
// One claim per sentence, and the families are tried in a fixed order.
// Execution comes first because "the tests pass" is a stronger, more
// checkable statement than the reading verb that may sit in the same
// sentence. Reading comes last for the same reason in reverse: "I checked the
// callers and nothing else uses it" is really a safety claim, and verifying it
// against impact is worth more than verifying that some file was opened.
func (p Pattern) classify(sentence string) (Claim, bool) {
	for _, pat := range executionPatterns {
		if pat.re.MatchString(sentence) {
			return Claim{
				Family:   Execution,
				Kind:     pat.kind,
				Scope:    ClassifyScope(sentence),
				Subjects: Subjects(sentence),
			}, true
		}
	}
	for _, pat := range safetyPatterns {
		if pat.re.MatchString(sentence) {
			return Claim{
				Family:     Safety,
				SafetyKind: pat.safety,
				Scope:      ClassifyScope(sentence),
				Subjects:   Identifiers(sentence),
			}, true
		}
	}
	for _, pat := range structuralPatterns {
		if !pat.re.MatchString(sentence) {
			continue
		}
		// A structural claim without an identifier names nothing that can be
		// looked up in the entity diff, so it is not extracted at all rather
		// than extracted and then reported as unresolvable.
		ids := Identifiers(sentence)
		if len(ids) == 0 {
			continue
		}
		return Claim{
			Family:   Structural,
			Kind:     Kind(pat.name),
			Scope:    ClassifyScope(sentence),
			Subjects: ids,
		}, true
	}
	for _, pat := range readingPatterns {
		if pat.re.MatchString(sentence) {
			return Claim{
				Family:   Reading,
				Scope:    ClassifyScope(sentence),
				Subjects: Identifiers(sentence),
			}, true
		}
	}
	return Claim{}, false
}

// NamesReadingObject reports whether a reading claim names a class of thing
// the record can check, such as callers or tests, rather than only a file.
func NamesReadingObject(sentence string) bool {
	return readingObjects.MatchString(sentence)
}

// identifierRe matches a bare code identifier: snake_case, CamelCase, or a
// dotted or double-colon path. Ordinary prose words are excluded by requiring
// a separator or an internal capital.
var identifierRe = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*(?:_[A-Za-z0-9_]+)+|[a-z]+[A-Z][A-Za-z0-9]*|[A-Z][a-z0-9]+[A-Z][A-Za-z0-9]*)\b`)

// Identifiers returns the code identifiers and paths a sentence names.
//
// Backticked words come first and are trusted most, because an agent quoting
// a name is naming a symbol. Bare words only count when they look like code,
// so "removed the legacy shim" yields nothing and is not extracted, while
// "removed `_legacy_shim`" yields the symbol.
func Identifiers(sentence string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(strings.Trim(s, "`\"'.,;:()[]{}"))
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	for _, m := range backtickedRe.FindAllStringSubmatch(sentence, -1) {
		if strings.ContainsAny(m[1], " ") {
			for _, t := range transcript.CommandTargets(m[1]) {
				add(t)
			}
			continue
		}
		add(m[1])
	}
	for _, m := range pathLikeRe.FindAllStringSubmatch(sentence, -1) {
		add(m[1])
	}
	for _, m := range identifierRe.FindAllStringSubmatch(sentence, -1) {
		add(m[1])
	}
	return out
}

// isNegated reports whether a sentence denies, conditions or advises rather
// than asserting.
func isNegated(sentence string) bool {
	for _, re := range negations {
		if re.MatchString(sentence) {
			return true
		}
	}
	// A failure word normally disqualifies a sentence, but not when it is
	// being denied. "Zero failing tests" is a claim that things passed.
	if failureWordRe.MatchString(sentence) && !noFailureRe.MatchString(sentence) {
		return true
	}
	return false
}

// targetLikeRe matches the things a test runner can actually be pointed at:
// a file with a source extension, optionally with a node id.
var targetLikeRe = regexp.MustCompile(`\.(?:py|go|js|jsx|ts|tsx|rb|rs|java|php)(?:::[\w:]+)?$`)

// ScopeSubjects narrows a claim's subjects to the ones that can bound its
// scope.
//
// A sentence often names a directory or a module as context, as in "from
// `app/fixtures`: 4 passed". That is not something a runner was pointed at,
// and treating it as an uncovered target would impeach a claim for a scope it
// never asserted. Only runner-addressable targets count.
func ScopeSubjects(subjects []string) []string {
	var out []string
	for _, s := range subjects {
		if targetLikeRe.MatchString(s) || strings.Contains(s, "::") {
			out = append(out, s)
		}
	}
	return out
}

// ClassifyScope decides how wide a claim is.
//
// A named subset wins over a scope word, because "all tests in test_api.py"
// is a claim about one file however it is phrased. Only an unqualified scope
// word makes a claim about everything.
func ClassifyScope(sentence string) Scope {
	if len(ScopeSubjects(Subjects(sentence))) > 0 {
		return ScopeSubset
	}
	if scopeAllRe.MatchString(sentence) {
		return ScopeAll
	}
	return ScopeUnspecified
}

// Subjects returns the things a sentence names: backticked identifiers, bare
// paths and node ids.
func Subjects(sentence string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(strings.Trim(s, "`\"'.,;:()"))
		if s == "" || seen[s] {
			return
		}
		// A scope word is not a subject.
		if scopeAllRe.MatchString(s) && !strings.Contains(s, "/") && !strings.Contains(s, ".") {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	for _, m := range backtickedRe.FindAllStringSubmatch(sentence, -1) {
		// A backticked command is not a subject; its targets are.
		if strings.ContainsAny(m[1], " ") {
			for _, t := range transcript.CommandTargets(m[1]) {
				add(t)
			}
			continue
		}
		add(m[1])
	}
	for _, m := range pathLikeRe.FindAllStringSubmatch(sentence, -1) {
		add(m[1])
	}
	return out
}
