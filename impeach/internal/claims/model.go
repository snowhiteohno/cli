package claims

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/entireio/cli/impeach/internal/runner"
	"github.com/entireio/cli/impeach/internal/transcript"
)

// Model is the opt-in extractor. It is off by default and adds nothing to the
// default path, which stays deterministic and offline.
//
// It exists so a vaguely phrased claim the pattern library misses can still be
// caught, and it deliberately produces the same Claim type as the pattern
// extractor, so the verifiers cannot tell where a claim came from and cannot
// treat a model's suggestion as evidence of anything. The record still decides
// every verdict.
//
// What leaves the machine is bounded by design: the assistant text of one turn
// at a time, never a prompt, never a tool output, never a file. The report
// header names the exact command so a reader knows a third party saw that
// much.
type Model struct {
	// Command is the user's command line, executed as argv with no shell.
	Command string
	// Runner runs it. Nothing here starts a process directly.
	Runner runner.Runner
	// MaxTurns caps how many turns are sent, so a long session cannot quietly
	// become a large amount of outbound text. Zero means no cap.
	MaxTurns int

	// Warnings collects what was dropped and why. A model that returns
	// nonsense must be visible, not silently ignored.
	Warnings []string
}

// Name implements Extractor. The command is part of the name so the report and
// the JSON both record which model produced a claim.
func (m *Model) Name() string { return "model:" + m.Command }

// modelPrompt is the instruction sent on stdin ahead of the turn's text.
//
// It asks for strict JSON and nothing else. Anything that is not valid JSON is
// dropped rather than guessed at, because a half-parsed claim is worse than a
// missing one.
const modelPrompt = `You are extracting checkable claims from one turn of a coding agent's message.

Return ONLY a JSON array, with no prose, no code fence and no explanation. Each element must be an object with exactly these keys:
  "text"    the claim, quoted verbatim from the message as a single sentence
  "family"  one of: execution, structural, safety, reading
  "subject" the file path or symbol name the claim is about, or "" if none

A claim is a statement of fact about work that was done: that tests ran and passed, that an entity was added or removed, that a change is contained or compatible, or that something was read. Do not include intentions, plans, questions, suggestions or predictions. Do not invent claims that are not in the text. If there are none, return [].

The message follows.
`

// modelClaim is the shape the command must return.
type modelClaim struct {
	Text    string `json:"text"`
	Family  string `json:"family"`
	Subject string `json:"subject"`
}

// Extract implements Extractor.
func (m *Model) Extract(events []transcript.Event) ([]Claim, error) {
	argv, err := splitCommand(m.Command)
	if err != nil {
		return nil, err
	}
	if len(argv) == 0 {
		return nil, fmt.Errorf("--model was given an empty command")
	}

	turns := assistantTurns(events)
	if m.MaxTurns > 0 && len(turns) > m.MaxTurns {
		m.Warnings = append(m.Warnings, fmt.Sprintf(
			"only the last %d of %d turns were sent to the model command", m.MaxTurns, len(turns)))
		turns = turns[len(turns)-m.MaxTurns:]
	}

	var out []Claim
	n := 0
	for _, t := range turns {
		claims, err := m.extractTurn(argv, t)
		if err != nil {
			// One turn failing is not the run failing. Record it and carry on.
			m.Warnings = append(m.Warnings, fmt.Sprintf("turn %d: %v", t.turn, err))
			continue
		}
		for _, c := range claims {
			n++
			c.ID = fmt.Sprintf("m%d", n)
			c.Turn = t.turn
			c.Seq = t.seq
			c.Extractor = m.Name()
			out = append(out, c)
		}
	}
	return out, nil
}

// turnText is one assistant turn's text and where it sat in the stream.
type turnText struct {
	turn int
	seq  int
	text string
}

// assistantTurns groups assistant text by turn. Only assistant text is ever
// sent: a prompt is what the user asked for, not what the agent claimed, and
// tool output is evidence rather than testimony.
func assistantTurns(events []transcript.Event) []turnText {
	byTurn := map[int]*turnText{}
	var order []int
	for i := range events {
		e := &events[i]
		if e.Kind != transcript.AssistantText || strings.TrimSpace(e.Text) == "" {
			continue
		}
		t, ok := byTurn[e.Turn]
		if !ok {
			t = &turnText{turn: e.Turn, seq: e.Seq}
			byTurn[e.Turn] = t
			order = append(order, e.Turn)
		}
		if t.text != "" {
			t.text += "\n"
		}
		t.text += e.Text
		// The last text event in a turn is what a claim from it is ordered by,
		// which keeps staleness comparisons conservative.
		t.seq = e.Seq
	}
	out := make([]turnText, 0, len(order))
	for _, turn := range order {
		out = append(out, *byTurn[turn])
	}
	return out
}

// extractTurn sends one turn and parses the reply.
func (m *Model) extractTurn(argv []string, t turnText) ([]Claim, error) {
	ctx, cancel := context.WithTimeout(context.Background(), runner.TimeoutModel)
	defer cancel()

	stdin := []byte(modelPrompt + "\n" + t.text + "\n")
	stdout, stderr, exit, err := m.Runner.Run(ctx, argv[0], argv[1:], stdin)
	if err != nil {
		return nil, err
	}
	if exit != 0 {
		return nil, fmt.Errorf("exited %d: %s", exit, firstLine(stderr))
	}

	raw, err := parseModelJSON(stdout)
	if err != nil {
		return nil, err
	}

	var out []Claim
	for _, r := range raw {
		text := strings.TrimSpace(r.Text)
		if text == "" {
			continue
		}
		family, ok := parseFamily(r.Family)
		if !ok {
			m.Warnings = append(m.Warnings, fmt.Sprintf(
				"turn %d: dropped a claim with unknown family %q", t.turn, r.Family))
			continue
		}
		c := Claim{
			Family: family,
			Text:   text,
			Scope:  ClassifyScope(text),
		}
		if s := strings.TrimSpace(r.Subject); s != "" {
			c.Subjects = []string{s}
		} else {
			c.Subjects = subjectsFor(family, text)
		}
		if family == Execution {
			c.Kind = execKindFor(text)
		}
		if family == Safety {
			c.SafetyKind = safetyKindFor(text)
		}
		out = append(out, c)
	}
	return out, nil
}

// parseModelJSON reads the array a model command returned.
//
// A fenced code block is tolerated, because models emit them constantly, but
// nothing else is. Anything unparseable is an error and the turn is dropped.
func parseModelJSON(stdout []byte) ([]modelClaim, error) {
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return nil, fmt.Errorf("returned no output")
	}
	s = stripFence(s)

	// Take the outermost array, so a stray sentence before or after it does
	// not defeat an otherwise usable answer.
	start := strings.Index(s, "[")
	end := strings.LastIndex(s, "]")
	if start < 0 || end < start {
		return nil, fmt.Errorf("returned no JSON array")
	}
	s = s[start : end+1]

	var out []modelClaim
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("returned invalid JSON: %w", err)
	}
	return out, nil
}

func stripFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func parseFamily(s string) (Family, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "execution":
		return Execution, true
	case "structural":
		return Structural, true
	case "safety":
		return Safety, true
	case "reading":
		return Reading, true
	default:
		return 0, false
	}
}

// subjectsFor recovers subjects from the text when the model named none, so a
// model claim goes to the verifiers with the same information a pattern claim
// would carry.
func subjectsFor(f Family, text string) []string {
	if f == Execution {
		return Subjects(text)
	}
	return Identifiers(text)
}

// execKindFor decides whether an execution claim is about tests, a build or a
// linter, using the same pattern library the default extractor uses. A model
// is not asked to make this distinction, because the pattern set already
// encodes it.
func execKindFor(text string) Kind {
	for _, pat := range executionPatterns {
		if pat.re.MatchString(text) {
			return pat.kind
		}
	}
	return KindTest
}

// safetyKindFor decides containment against compatibility the same way.
func safetyKindFor(text string) SafetyKind {
	for _, pat := range safetyPatterns {
		if pat.re.MatchString(text) {
			return pat.safety
		}
	}
	return SafetyContainment
}

// splitCommand splits a command line into argv without a shell.
//
// There is no shell anywhere in Impeach, so the command is tokenized here and
// passed as argv. Quotes group arguments; nothing is expanded, substituted or
// interpreted.
func splitCommand(cmd string) ([]string, error) {
	var out []string
	var cur strings.Builder
	var quote rune
	started := false

	flush := func() {
		if started {
			out = append(out, cur.String())
			cur.Reset()
			started = false
		}
	}
	for _, r := range cmd {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
			started = true
		case r == '\'' || r == '"':
			quote = r
			started = true
		case r == ' ' || r == '\t':
			flush()
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unbalanced quote in --model command %q", cmd)
	}
	flush()
	return out, nil
}

func firstLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return "no output"
	}
	return s
}
