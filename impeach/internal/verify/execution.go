package verify

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/entireio/cli/impeach/internal/claims"
	"github.com/entireio/cli/impeach/internal/record"
	"github.com/entireio/cli/impeach/internal/transcript"
)

// Execution verifies claims that something ran and succeeded.
type Execution struct {
	// TestCommand is the user's --test command. It is added to the candidate
	// patterns so a project with an unusual runner is still recognised.
	TestCommand string
}

// Family implements Verifier.
func (Execution) Family() claims.Family { return claims.Execution }

// runnerPatterns match a command to the claim kind it could support, so a
// claim about the linter is never corroborated by a test run.
var runnerPatterns = map[claims.Kind]*regexp.Regexp{
	claims.KindTest: regexp.MustCompile(
		`(?i)(^|[\s/;&|])(pytest|py\.test|tox|nox|jest|vitest|mocha|rspec|minitest|phpunit|ctest|gotestsum)\b` +
			`|(?i)\bgo\s+test\b|(?i)\b(npm|yarn|pnpm)\s+(run\s+)?test\b|(?i)\bcargo\s+test\b|(?i)\bmake\s+test\b` +
			`|(?i)\bpython\d?\s+-m\s+(pytest|unittest)\b|(?i)\bmvn\s+test\b|(?i)\bgradle\s+test\b|(?i)\brake\s+test\b`),
	claims.KindBuild: regexp.MustCompile(
		`(?i)\bgo\s+build\b|(?i)\b(npm|yarn|pnpm)\s+run\s+build\b|(?i)\bcargo\s+build\b|(?i)\bmake\s+(build|all)\b` +
			`|(?i)\btsc\b|(?i)\bmvn\s+(compile|package)\b|(?i)\bgradle\s+(build|assemble)\b`),
	claims.KindLint: regexp.MustCompile(
		`(?i)\b(golangci-lint|ruff|flake8|eslint|mypy|pylint|rubocop|shellcheck)\b|(?i)\bgo\s+vet\b` +
			`|(?i)\b(npm|yarn|pnpm)\s+run\s+lint\b|(?i)\bcargo\s+clippy\b|(?i)\bmake\s+lint\b`),
}

// successPatterns recognise a successful run from its output. An output that
// matches none of them is unparsed, which makes the command non-supporting
// rather than failing.
var successPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b\d+\s+passed\b`),                    // pytest
	regexp.MustCompile(`(?i)^\s*ok\s+\S+`),                        // go test
	regexp.MustCompile(`(?i)\bTests:\s+.*\b\d+\s+passed\b`),       // jest
	regexp.MustCompile(`(?i)\btest result:\s*ok\b`),               // cargo
	regexp.MustCompile(`(?i)\b\d+\s+examples?,\s+0\s+failures\b`), // rspec
	regexp.MustCompile(`(?i)\bOK\s*\(\d+\s+tests?\)`),             // phpunit
	regexp.MustCompile(`(?i)\bBUILD SUCCESS(FUL)?\b`),             // maven, gradle
	regexp.MustCompile(`(?i)\bno issues found\b`),                 // mypy
}

// failurePatterns recognise a failed run. These are checked first, because an
// output can contain both a passed count and a failure count.
var failurePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b\d+\s+failed\b`),
	regexp.MustCompile(`(?i)\bFAILED\b`),
	regexp.MustCompile(`(?i)^\s*FAIL\b`),
	regexp.MustCompile(`(?i)\bTests:\s+.*\b\d+\s+failed\b`),
	regexp.MustCompile(`(?i)\btest result:\s*FAILED\b`),
	regexp.MustCompile(`(?i)\b\d+\s+examples?,\s+[1-9]\d*\s+failures\b`),
	regexp.MustCompile(`(?i)\bBUILD FAILED\b`),
	regexp.MustCompile(`(?i)\berror:`),
}

// outcome is what a command's output shows.
type outcome int

const (
	outcomeUnparsed outcome = iota
	outcomeSuccess
	outcomeFailure
)

// Verify implements Verifier.
//
// The order of the checks is the order of the design's step list: find the
// relevant files, find candidate commands, pick a supporting one, then test it
// for staleness and scope, then fold in the rerun.
func (e Execution) Verify(c claims.Claim, r *Record) Verdict {
	v := Verdict{ClaimID: c.ID, Rerun: record.RerunNotRun}
	if r.Rerun != nil {
		v.Rerun = r.Rerun.Status
	}

	// The channel itself may be missing, which is a state and not a failure.
	if !r.Stream.Channels().Commands {
		v.Status = Unverifiable
		v.Summary = "This transcript carries no command records, so there is no way to check whether anything ran."
		e.attachRerun(&v, r)
		return v
	}

	relevant := r.RelevantEditedFiles()
	candidates := e.candidates(c, r)

	if len(candidates) == 0 {
		v.Status = Uncorroborated
		v.Summary = fmt.Sprintf("No %s command appears in the session's tool log, so nothing supports this claim.", c.Kind)
		e.attachRerun(&v, r)
		e.applyRerunContradiction(&v, c, r)
		return gate(v, r, transcript.ChannelCommands)
	}

	supporting, unparsed, failing := pickSupporting(candidates)

	if supporting == nil {
		switch {
		case failing != nil:
			// The record contradicts the claim outright.
			v.Status = Impeached
			v.addReason(ReasonContradictedOutput)
			v.Summary = fmt.Sprintf("The last %s command in the session failed, and the claim says it succeeded.", c.Kind)
			v.Evidence = append(v.Evidence, commandEvidence(failing, outcomeFailure))
		case unparsed != nil:
			v.Status = Uncorroborated
			v.Summary = fmt.Sprintf("A %s command ran but its output could not be parsed, so it neither supports nor contradicts the claim.", c.Kind)
			v.Evidence = append(v.Evidence, commandEvidence(unparsed, outcomeUnparsed))
		default:
			v.Status = Uncorroborated
			v.Summary = fmt.Sprintf("No %s command in the session shows a successful run.", c.Kind)
		}
		e.attachRerun(&v, r)
		e.applyRerunContradiction(&v, c, r)
		return gate(v, r, transcript.ChannelCommands)
	}

	v.Evidence = append(v.Evidence, commandEvidence(supporting, outcomeSuccess))

	// Staleness. Ordering is by Seq so a transcript with no timestamps still
	// orders correctly.
	if seq, ok := r.Stream.LastEditSeq(relevant); ok && seq > supporting.Seq {
		v.addReason(ReasonStale)
		for _, ev := range r.Stream.Events {
			if ev.Kind == transcript.FileEdit && ev.Seq == seq {
				v.Evidence = append(v.Evidence, Evidence{
					Type: EvidenceEdit, Seq: ev.Seq, Text: ev.Path,
					Detail: "edited after the last supporting run",
				})
				break
			}
		}
	}

	// Scope.
	cmdScope, cmdTargets := commandScope(supporting)
	if mismatch, detail := scopeMismatch(c, cmdScope, cmdTargets, r); mismatch {
		v.addReason(ReasonScopeMismatch)
		// The scope finding is about the command already in evidence, so it
		// annotates that item rather than repeating the whole command line.
		for i := range v.Evidence {
			if v.Evidence[i].Type == EvidenceCommand && v.Evidence[i].Seq == supporting.Seq {
				v.Evidence[i].Detail += "; " + detail
				break
			}
		}
	}

	e.attachRerun(&v, r)
	e.applyRerunContradiction(&v, c, r)

	if len(v.Reasons) > 0 {
		v.Status = Impeached
		v.Summary = summarize(c, supporting, v.Reasons, r)
		return v
	}

	v.Status = Corroborated
	v.Summary = corroboratedSummary(c, supporting, cmdScope)
	// An execution claim rests on the command log. If that channel was
	// redacted or truncated, the run that would have contradicted the claim
	// may be exactly the part that is missing.
	return gate(v, r, transcript.ChannelCommands)
}

// candidates returns the commands that could support the claim, in order.
func (e Execution) candidates(c claims.Claim, r *Record) []*transcript.Event {
	re := runnerPatterns[c.Kind]
	var out []*transcript.Event
	for i := range r.Stream.Events {
		ev := &r.Stream.Events[i]
		if ev.Kind != transcript.Command || ev.Cmd == nil {
			continue
		}
		match := re != nil && re.MatchString(ev.Cmd.Cmd)
		// The user's own test command counts as a test runner even when it
		// matches none of the built-in patterns, which is how a project with
		// an unusual runner is still recognised.
		//
		// It is deliberately skipped when the test command already matches a
		// built-in pattern. Matching on its first word was wrong: a compound
		// command like `cd app && pytest` has "cd" as its first word, which
		// appears in almost every shell command, so a failing `git status`
		// was being read as a failing test run and reported as
		// contradicted-output. When the built-in patterns already recognise
		// the runner, they are the precise answer and the fallback can only
		// add noise.
		if !match && c.Kind == claims.KindTest && e.usesCustomRunner() &&
			strings.Contains(ev.Cmd.Cmd, e.runnerToken()) {
			match = true
		}
		if match {
			out = append(out, ev)
		}
	}
	return out
}

// pickSupporting returns the last successful command, and the last unparsed
// and failing ones for reporting when there is no successful one.
//
// The last successful command is the one to test for staleness: an earlier
// green run followed by a later green run means the later one is what the
// claim rests on.
func pickSupporting(candidates []*transcript.Event) (supporting, unparsed, failing *transcript.Event) {
	for _, ev := range candidates {
		switch classify(ev.Cmd) {
		case outcomeSuccess:
			supporting = ev
		case outcomeFailure:
			failing = ev
		default:
			unparsed = ev
		}
	}
	return supporting, unparsed, failing
}

// classify reads a command's output.
//
// A known non-zero exit is a failure whatever the text says. Otherwise
// failure patterns are checked before success patterns, because a pytest
// summary can carry both counts and the failure is the load-bearing half.
func classify(c *transcript.CommandInfo) outcome {
	if c.ExitKnown && c.ExitCode != 0 {
		return outcomeFailure
	}
	if strings.TrimSpace(c.Output) == "" {
		// A successful exit with no output is still a success when the
		// transcript settled the exit status.
		if c.ExitKnown && c.ExitCode == 0 {
			return outcomeSuccess
		}
		return outcomeUnparsed
	}
	for _, re := range failurePatterns {
		if matchAnyLine(re, c.Output) {
			return outcomeFailure
		}
	}
	for _, re := range successPatterns {
		if matchAnyLine(re, c.Output) {
			return outcomeSuccess
		}
	}
	return outcomeUnparsed
}

// matchAnyLine applies a pattern per line, so line-anchored patterns work
// against multi-line output.
func matchAnyLine(re *regexp.Regexp, out string) bool {
	if re.MatchString(out) {
		return true
	}
	for _, line := range strings.Split(out, "\n") {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

// commandScope classifies how wide a command's run was.
func commandScope(ev *transcript.Event) (claims.Scope, []string) {
	if len(ev.Cmd.Targets) == 0 {
		return claims.ScopeAll, nil
	}
	return claims.ScopeSubset, ev.Cmd.Targets
}

// scopeMismatch reports whether the command was narrower than the claim.
func scopeMismatch(c claims.Claim, cmdScope claims.Scope, targets []string, r *Record) (bool, string) {
	if cmdScope == claims.ScopeAll {
		// A full run supports any scope.
		return false, ""
	}

	switch c.Scope {
	case claims.ScopeAll:
		return true, fmt.Sprintf("the run named %s, so it did not cover everything the claim asserts",
			strings.Join(targets, ", "))
	case claims.ScopeSubset:
		// The claim names things too. A mismatch is only real when the
		// command's targets do not cover the claim's subjects.
		var uncovered []string
		for _, subj := range claims.ScopeSubjects(c.Subjects) {
			if !covered(subj, targets) {
				uncovered = append(uncovered, subj)
			}
		}
		if len(uncovered) > 0 {
			return true, fmt.Sprintf("the run named %s, which does not cover %s",
				strings.Join(targets, ", "), strings.Join(uncovered, ", "))
		}
		return false, ""
	default:
		// An unspecified claim rests on a narrowed run, with a note.
		return false, ""
	}
}

// covered reports whether a claim's subject is inside a command's targets.
func covered(subject string, targets []string) bool {
	for _, t := range targets {
		if t == subject ||
			strings.HasPrefix(t, subject) ||
			strings.HasPrefix(subject, t) ||
			strings.Contains(t, subject) {
			return true
		}
	}
	return false
}

// attachRerun records the rerun as its own evidence item, always, so the
// column is filled independently of the verdict.
func (e Execution) attachRerun(v *Verdict, r *Record) {
	if r.Rerun == nil {
		return
	}
	ev := Evidence{Type: EvidenceRerun, Detail: string(r.Rerun.Status)}
	switch {
	case len(r.Rerun.NewFailures) > 0:
		ev.Text = fmt.Sprintf("%d new failures: %s", len(r.Rerun.NewFailures),
			strings.Join(r.Rerun.NewFailures, ", "))
	case r.Rerun.Verdict != "":
		ev.Text = r.Rerun.Verdict
	case r.Rerun.Reason != "":
		ev.Text = r.Rerun.Reason
	}
	if len(r.Rerun.Commands) > 0 {
		ev.Command = r.Rerun.Commands[len(r.Rerun.Commands)-1]
	}
	v.Evidence = append(v.Evidence, ev)
}

// applyRerunContradiction adds contradicted-rerun when a fresh run shows new
// failures and the claim says things pass.
//
// This is additive: a claim can be impeached for scope or staleness even when
// the suite passes today, and impeached for the rerun even when the tool log
// looked clean.
func (e Execution) applyRerunContradiction(v *Verdict, c claims.Claim, r *Record) {
	if r.Rerun == nil || r.Rerun.Status != record.RerunNewFailures {
		return
	}
	if c.Kind != claims.KindTest {
		return
	}
	v.addReason(ReasonContradictedRerun)
	if v.Status != Impeached {
		v.Status = Impeached
		v.Summary = fmt.Sprintf("Re-running the tests now shows %d failures that passed before this change.",
			len(r.Rerun.NewFailures))
	}
}

func commandEvidence(ev *transcript.Event, o outcome) Evidence {
	detail := "output not parsed"
	switch o {
	case outcomeSuccess:
		detail = "pass"
	case outcomeFailure:
		detail = "fail"
	}
	if ev.Cmd.ExitKnown {
		detail = fmt.Sprintf("%s (exit %d)", detail, ev.Cmd.ExitCode)
	}
	return Evidence{
		Type:    EvidenceCommand,
		Seq:     ev.Seq,
		Text:    ev.Cmd.Cmd,
		Excerpt: excerpt(strings.TrimSpace(ev.Cmd.Output)),
		Detail:  detail,
	}
}

func summarize(c claims.Claim, supporting *transcript.Event, reasons []string, r *Record) string {
	var parts []string
	for _, code := range reasons {
		switch code {
		case ReasonScopeMismatch:
			n := len(supporting.Cmd.Targets)
			parts = append(parts, fmt.Sprintf("the run covered %d named target%s rather than everything claimed",
				n, plural(n)))
		case ReasonStale:
			parts = append(parts, "a relevant file was edited after the last run and nothing ran again")
		case ReasonContradictedRerun:
			parts = append(parts, fmt.Sprintf("re-running now shows %d new failures", len(r.Rerun.NewFailures)))
		case ReasonContradictedOutput:
			parts = append(parts, "the command's own output shows a failure")
		}
	}
	return capitalize(strings.Join(parts, ", and ")) + "."
}

func corroboratedSummary(c claims.Claim, supporting *transcript.Event, cmdScope claims.Scope) string {
	base := fmt.Sprintf("%q ran after the last relevant edit and its output shows success", supporting.Cmd.Cmd)
	if cmdScope == claims.ScopeSubset && c.Scope == claims.ScopeUnspecified {
		return base + ", supported for the named subset only."
	}
	return base + "."
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// usesCustomRunner reports whether the configured test command is something
// the built-in patterns do not already recognise. Only then is the fallback
// needed.
func (e Execution) usesCustomRunner() bool {
	if strings.TrimSpace(e.TestCommand) == "" {
		return false
	}
	for _, re := range runnerPatterns {
		if re.MatchString(e.TestCommand) {
			return false
		}
	}
	return true
}

// runnerToken picks a distinctive token from a custom test command to match
// transcript commands against.
//
// Shell builtins and operators are skipped, because matching on those matches
// everything. An empty result disables the fallback rather than matching
// loosely, since a loose match here manufactures evidence.
func (e Execution) runnerToken() string {
	for _, seg := range transcript.Segments(e.TestCommand) {
		for _, tok := range strings.Fields(seg) {
			switch tok {
			case "cd", "&&", "||", ";", "{", "}", "(", ")", "test", "-d", "sh", "-c", "env", "then", "else", "fi", "do", "done":
				continue
			}
			if strings.HasPrefix(tok, "-") || len(tok) < 3 {
				continue
			}
			return strings.Trim(tok, "\"'")
		}
	}
	// Nothing distinctive, so match nothing. A token this weak would match
	// unrelated commands and invent a verdict.
	return "\x00"
}
