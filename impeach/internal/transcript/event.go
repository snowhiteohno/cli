// Package transcript turns an agent's stored transcript into a normalized
// event stream. It is the boundary that absorbs a change of agent: a new agent
// means a new Adapter and nothing else.
package transcript

import (
	"fmt"
	"strings"
	"time"
)

// Kind is what an event is.
type Kind int

const (
	// Prompt is something the user asked for.
	Prompt Kind = iota
	// AssistantText is the agent talking. Claims are extracted from these.
	AssistantText
	// ToolCall is a tool invocation that is not a read, an edit or a command.
	ToolCall
	// ToolResult is a tool's result that is not attached to a command.
	ToolResult
	// FileRead is a file the agent looked at.
	FileRead
	// FileEdit is a file the agent changed.
	FileEdit
	// Command is a shell command the agent ran.
	Command
)

// String names a Kind for reports and test failures.
func (k Kind) String() string {
	switch k {
	case Prompt:
		return "prompt"
	case AssistantText:
		return "assistant_text"
	case ToolCall:
		return "tool_call"
	case ToolResult:
		return "tool_result"
	case FileRead:
		return "file_read"
	case FileEdit:
		return "file_edit"
	case Command:
		return "command"
	default:
		return fmt.Sprintf("kind(%d)", int(k))
	}
}

// ToolCallInfo describes a tool invocation.
type ToolCallInfo struct {
	ID   string
	Name string
}

// ToolResultInfo describes a tool's result.
type ToolResultInfo struct {
	ID      string
	IsError bool
	Content string
}

// CommandInfo describes one shell command and what came back.
//
// ExitKnown is true whenever the transcript settles the question. The Step 0
// probe found that it usually does: a result carries is_error, and a failing
// result's content opens with an explicit "Exit code N" line. So a successful
// command is exit 0 and a failed one usually carries its code, which is better
// than the original design assumed.
type CommandInfo struct {
	Cmd       string
	Output    string
	ExitKnown bool
	ExitCode  int
	// Targets are the paths and selectors parsed off the command line. They
	// decide whether a command's scope covers a claim's scope.
	Targets []string
}

// Event is one thing that happened, normalized across agents.
type Event struct {
	// Seq orders events even when timestamps are missing. Every ordering
	// decision in Impeach uses Seq, never time.
	Seq int
	// TS is zero when the transcript has no timestamp for the event.
	TS time.Time
	// Kind is what happened.
	Kind Kind
	// Turn is the assistant turn index, for "as of turn N" reasoning.
	Turn int
	// Text carries Prompt and AssistantText content.
	Text string
	// Call is set on ToolCall.
	Call *ToolCallInfo
	// Result is set on ToolResult.
	Result *ToolResultInfo
	// Path is the repository-relative path for FileRead and FileEdit.
	Path string
	// Cmd is set on Command.
	Cmd *CommandInfo
}

// Adapter parses one agent's transcript format.
type Adapter interface {
	// Name is the adapter's identifier, reported in the report header.
	Name() string
	// Detect reports whether this adapter recognises the bytes.
	Detect(raw []byte) bool
	// Parse produces the normalized event stream.
	Parse(raw []byte) ([]Event, error)
}

// Stream is a parsed transcript plus what the adapter noticed while parsing.
// The counts feed the report header, and the channel flags decide which
// verdicts are even possible.
type Stream struct {
	Events []Event
	// Adapter is the adapter that produced the stream.
	Adapter string
	// SubagentRecords counts records skipped because they belong to a
	// subagent. v1 does not examine them and the report says how many.
	SubagentRecords int
	// SkippedRecords counts records the adapter did not recognise at all.
	SkippedRecords int
	// Warnings are non-fatal notes worth showing a reader.
	Warnings []string
}

// Channels reports which evidence channels the transcript actually carries.
// A channel that is absent makes its claim family unverifiable rather than
// wrong, which is the invariant the whole tool rests on.
type Channels struct {
	Commands bool
	Reads    bool
	Edits    bool
	Prompts  bool
}

// Channels inspects the stream.
func (s *Stream) Channels() Channels {
	var c Channels
	for i := range s.Events {
		switch s.Events[i].Kind {
		case Command:
			c.Commands = true
		case FileRead:
			c.Reads = true
		case FileEdit:
			c.Edits = true
		case Prompt:
			c.Prompts = true
		}
	}
	return c
}

// Prompts returns the prompt texts in order. The unrequested detector builds
// its mention corpus from these.
func (s *Stream) Prompts() []string {
	var out []string
	for i := range s.Events {
		if s.Events[i].Kind == Prompt {
			out = append(out, s.Events[i].Text)
		}
	}
	return out
}

// EditedPaths returns the distinct repository-relative paths that were edited,
// in first-edit order.
func (s *Stream) EditedPaths() []string {
	seen := map[string]bool{}
	var out []string
	for i := range s.Events {
		e := &s.Events[i]
		if e.Kind == FileEdit && e.Path != "" && !seen[e.Path] {
			seen[e.Path] = true
			out = append(out, e.Path)
		}
	}
	return out
}

// LastEditSeq returns the sequence number of the last edit to any of paths, and
// whether there was one. Staleness is decided with this.
func (s *Stream) LastEditSeq(paths []string) (int, bool) {
	want := make(map[string]bool, len(paths))
	for _, p := range paths {
		want[p] = true
	}
	seq, found := 0, false
	for i := range s.Events {
		e := &s.Events[i]
		if e.Kind == FileEdit && want[e.Path] && e.Seq > seq {
			seq, found = e.Seq, true
		}
	}
	return seq, found
}

// TimestampsPresent reports whether any event carried a timestamp. When false
// the report shows turn numbers instead of times.
func (s *Stream) TimestampsPresent() bool {
	for i := range s.Events {
		if !s.Events[i].TS.IsZero() {
			return true
		}
	}
	return false
}

// Detect picks the adapter that recognises raw, or reports that none did.
func Detect(raw []byte, adapters []Adapter) (Adapter, error) {
	for _, a := range adapters {
		if a.Detect(raw) {
			return a, nil
		}
	}
	names := make([]string, 0, len(adapters))
	for _, a := range adapters {
		names = append(names, a.Name())
	}
	return nil, fmt.Errorf("no adapter recognised the transcript; tried %s", strings.Join(names, ", "))
}
