package transcript

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ClaudeCode parses Claude Code's stored JSONL transcript.
//
// The record shapes here were established by the Step 0 probe against a real
// checkpoint, not from documentation. Notably the observed top-level types are
// user, assistant, attachment, queue-operation and last-prompt. There is no
// system or summary record. Every attachment in the probe was harness
// bookkeeping and carried no file content, so attachments are ignored.
type ClaudeCode struct {
	// RepoRoot is stripped from absolute tool paths so paths can be compared
	// with Graph output, which is repository-relative. When empty, the cwd
	// recorded on each record is used instead.
	RepoRoot string
}

// Name implements Adapter.
func (ClaudeCode) Name() string { return "claude-code" }

// ccRecord is one JSONL line, limited to the fields Impeach reads.
type ccRecord struct {
	Type        string          `json:"type"`
	Timestamp   string          `json:"timestamp"`
	SessionID   string          `json:"sessionId"`
	UUID        string          `json:"uuid"`
	IsSidechain bool            `json:"isSidechain"`
	CWD         string          `json:"cwd"`
	Message     *ccMessage      `json:"message"`
	ToolUse     json.RawMessage `json:"toolUseResult"`
}

type ccMessage struct {
	Role string `json:"role"`
	// Content is either a string or an array of blocks.
	Content json.RawMessage `json:"content"`
}

type ccBlock struct {
	Type string `json:"type"`
	// text
	Text string `json:"text"`
	// tool_use
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
	// tool_result
	ToolUseID string          `json:"tool_use_id"`
	IsError   *bool           `json:"is_error"`
	Content   json.RawMessage `json:"content"`
}

// ccToolUseResult is the structured result the probe found alongside a
// tool_result block. On success it is an object; on failure it is a plain
// string, which is why the raw form is kept and decoded defensively.
type ccToolUseResult struct {
	Stdout      string `json:"stdout"`
	Stderr      string `json:"stderr"`
	Interrupted bool   `json:"interrupted"`
}

// Detect implements Adapter. It looks for the record and block vocabulary
// rather than for a version string, because the vocabulary is what the parser
// actually depends on.
func (ClaudeCode) Detect(raw []byte) bool {
	lines := splitLines(raw)
	checked := 0
	for _, line := range lines {
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var probe struct {
			Type      string `json:"type"`
			SessionID string `json:"sessionId"`
			Message   *struct {
				Role string `json:"role"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &probe); err != nil {
			continue
		}
		switch probe.Type {
		case "assistant", "user", "attachment", "queue-operation", "last-prompt", "system", "summary":
			if probe.SessionID != "" || probe.Message != nil {
				return true
			}
		}
		checked++
		if checked > 50 {
			break
		}
	}
	return false
}

// Parse implements Adapter.
func (a ClaudeCode) Parse(raw []byte) ([]Event, error) {
	s, err := a.ParseStream(raw)
	if err != nil {
		return nil, err
	}
	return s.Events, nil
}

// ParseStream is Parse plus the bookkeeping the report header needs.
func (a ClaudeCode) ParseStream(raw []byte) (*Stream, error) {
	out := &Stream{Adapter: a.Name()}
	seq := 0
	turn := 0

	// A tool_use is seen before its tool_result, so calls are remembered and
	// resolved when the result arrives.
	type pending struct {
		name  string
		input json.RawMessage
		seq   int
		turn  int
		ts    time.Time
	}
	open := map[string]pending{}
	// Command events are emitted at call time and completed at result time.
	cmdIndex := map[string]int{}

	lines := splitLines(raw)
	if len(lines) == 0 {
		return nil, fmt.Errorf("transcript is empty")
	}

	for _, line := range lines {
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var rec ccRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			out.SkippedRecords++
			continue
		}

		// Subagent records are out of scope for v1 and counted, not dropped
		// silently.
		if rec.IsSidechain {
			out.SubagentRecords++
			continue
		}

		switch rec.Type {
		case "attachment", "queue-operation", "last-prompt", "summary", "system":
			// Harness bookkeeping. The probe confirmed attachments carry no
			// file content, so nothing here is evidence.
			continue
		case "user", "assistant":
		default:
			out.SkippedRecords++
			continue
		}

		if rec.Message == nil {
			continue
		}
		ts := parseTime(rec.Timestamp)
		root := a.root(rec.CWD)

		// A user message whose content is a plain string is a prompt. A user
		// message carrying tool_result blocks is the tool channel, not a
		// prompt.
		if rec.Type == "user" {
			if text, ok := decodeString(rec.Message.Content); ok {
				if strings.TrimSpace(text) != "" {
					seq++
					out.Events = append(out.Events, Event{
						Seq: seq, TS: ts, Kind: Prompt, Turn: turn, Text: text,
					})
				}
				continue
			}
		}

		blocks, err := decodeBlocks(rec.Message.Content)
		if err != nil {
			out.SkippedRecords++
			continue
		}

		if rec.Type == "assistant" {
			turn++
		}

		for _, b := range blocks {
			switch b.Type {
			case "text":
				if strings.TrimSpace(b.Text) == "" {
					continue
				}
				seq++
				out.Events = append(out.Events, Event{
					Seq: seq, TS: ts, Kind: AssistantText, Turn: turn, Text: b.Text,
				})

			case "thinking", "redacted_thinking":
				// Reasoning is not testimony. The agent has not told anyone
				// anything yet, so nothing here is a claim.
				continue

			case "tool_use":
				open[b.ID] = pending{name: b.Name, input: b.Input, seq: seq, turn: turn, ts: ts}
				seq++
				ev, isCmd := a.eventForToolUse(b, root, seq, turn, ts)
				out.Events = append(out.Events, ev)
				if isCmd {
					cmdIndex[b.ID] = len(out.Events) - 1
				}

			case "tool_result":
				content := decodeResultContent(b.Content)
				isErr := b.IsError != nil && *b.IsError

				if idx, ok := cmdIndex[b.ToolUseID]; ok {
					// Prefer the structured result when it is an object,
					// since it separates stdout from stderr.
					output := content
					if s, ok := decodeToolUseResult(rec.ToolUse); ok {
						output = joinOutput(s.Stdout, s.Stderr)
					}
					c := out.Events[idx].Cmd
					c.Output = output
					c.ExitKnown, c.ExitCode = exitFrom(isErr, content)
					continue
				}

				seq++
				out.Events = append(out.Events, Event{
					Seq: seq, TS: ts, Kind: ToolResult, Turn: turn,
					Result: &ToolResultInfo{ID: b.ToolUseID, IsError: isErr, Content: content},
				})
			}
		}
	}

	if len(out.Events) == 0 {
		return nil, fmt.Errorf("transcript carried no readable records")
	}
	return out, nil
}

// eventForToolUse maps one tool call onto the normalized model.
func (a ClaudeCode) eventForToolUse(b ccBlock, root string, seq, turn int, ts time.Time) (Event, bool) {
	ev := Event{Seq: seq, TS: ts, Turn: turn}

	switch b.Name {
	case "Read", "NotebookRead":
		ev.Kind = FileRead
		ev.Path = a.rel(root, inputPath(b.Input))
		return ev, false

	case "Glob", "Grep":
		// The path input is the directory searched, which is weaker evidence
		// than a file read but is still a read of something named.
		ev.Kind = FileRead
		ev.Path = a.rel(root, inputPath(b.Input))
		return ev, false

	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		ev.Kind = FileEdit
		ev.Path = a.rel(root, inputPath(b.Input))
		return ev, false

	case "Bash", "BashOutput":
		cmd := inputString(b.Input, "command")
		ev.Kind = Command
		ev.Cmd = &CommandInfo{Cmd: cmd, Targets: CommandTargets(cmd)}
		return ev, true

	default:
		ev.Kind = ToolCall
		ev.Call = &ToolCallInfo{ID: b.ID, Name: b.Name}
		return ev, false
	}
}

// root picks the repository root to relativize against.
func (a ClaudeCode) root(cwd string) string {
	if a.RepoRoot != "" {
		return a.RepoRoot
	}
	return cwd
}

// rel makes an absolute tool path repository-relative, because Graph and
// `checkpoint explain --json` both report repository-relative paths and the
// probe found tool inputs are absolute.
func (a ClaudeCode) rel(root, p string) string {
	if p == "" {
		return ""
	}
	p = filepath.Clean(p)
	if root == "" {
		return strings.TrimPrefix(p, "./")
	}
	root = filepath.Clean(root)
	// Prefix stripping first, because it works whether or not the root is a
	// real absolute path. Recorded fixtures carry a scrub placeholder such as
	// <repo> in place of the original root, and those must relativize too or
	// the replay tests would compare paths the real run never produces.
	if p == root {
		return "."
	}
	if strings.HasPrefix(p, root+string(filepath.Separator)) {
		return p[len(root)+1:]
	}
	if filepath.IsAbs(p) && filepath.IsAbs(root) {
		if rel, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return p
}

// exitFrom decides whether the transcript settles the exit status.
//
// A result that is not an error means the command succeeded. A result that is
// an error usually opens with "Exit code N", which the probe confirmed. Both
// cases are knowable, so ExitKnown is true far more often than the original
// design assumed.
func exitFrom(isErr bool, content string) (bool, int) {
	if !isErr {
		return true, 0
	}
	if m := exitCodeRe.FindStringSubmatch(content); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return true, n
		}
	}
	// An error with no code is still known to be a failure. Report 1 as a
	// stand-in and let the caller rely on the failure, not the number.
	return true, 1
}

var exitCodeRe = regexp.MustCompile(`(?i)\bexit code[: ]\s*(\d+)`)

func joinOutput(stdout, stderr string) string {
	switch {
	case stdout != "" && stderr != "":
		return stdout + "\n" + stderr
	case stderr != "":
		return stderr
	default:
		return stdout
	}
}

func decodeToolUseResult(raw json.RawMessage) (ccToolUseResult, bool) {
	if len(raw) == 0 {
		return ccToolUseResult{}, false
	}
	var s ccToolUseResult
	if err := json.Unmarshal(raw, &s); err != nil {
		// On failure this field is a plain string, not an object.
		return ccToolUseResult{}, false
	}
	if s.Stdout == "" && s.Stderr == "" {
		return ccToolUseResult{}, false
	}
	return s, true
}

// decodeString reports whether content was a bare JSON string.
func decodeString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

func decodeBlocks(raw json.RawMessage) ([]ccBlock, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return []ccBlock{{Type: "text", Text: s}}, nil
	}
	var blocks []ccBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, err
	}
	return blocks, nil
}

// decodeResultContent flattens a tool_result's content, which is a string in
// some records and an array of text blocks in others.
func decodeResultContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if s, ok := decodeString(raw); ok {
		return s
	}
	var blocks []ccBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var b strings.Builder
	for _, blk := range blocks {
		if blk.Text != "" {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(blk.Text)
		}
	}
	return b.String()
}

func inputPath(raw json.RawMessage) string {
	if p := inputString(raw, "file_path"); p != "" {
		return p
	}
	if p := inputString(raw, "notebook_path"); p != "" {
		return p
	}
	return inputString(raw, "path")
}

func inputString(raw json.RawMessage, key string) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func splitLines(raw []byte) [][]byte {
	var out [][]byte
	for _, l := range strings.Split(string(raw), "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, []byte(l))
		}
	}
	return out
}
