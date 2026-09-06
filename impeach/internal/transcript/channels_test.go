package transcript

import (
	"testing"
)

// The asymmetry is the whole point of the change, so it is asserted directly
// rather than only through the verifiers.
func TestOnlyPresentChannelsMayCorroborate(t *testing.T) {
	t.Parallel()
	cases := map[ChannelState]bool{
		ChannelPresent:  true,
		ChannelPartial:  false,
		ChannelRedacted: false,
		ChannelAbsent:   false,
	}
	for state, want := range cases {
		if got := state.Corroborating(); got != want {
			t.Errorf("%s.Corroborating() = %v, want %v", state, got, want)
		}
	}
}

// An absent channel is complete: it is honestly empty. A partial or redacted
// one is not, because something is missing that a reader cannot see.
func TestCompleteDistinguishesAbsentFromRedacted(t *testing.T) {
	t.Parallel()
	cases := map[ChannelState]bool{
		ChannelPresent:  true,
		ChannelAbsent:   true,
		ChannelPartial:  false,
		ChannelRedacted: false,
	}
	for state, want := range cases {
		if got := state.Complete(); got != want {
			t.Errorf("%s.Complete() = %v, want %v", state, got, want)
		}
	}
}

func TestIsRedacted(t *testing.T) {
	t.Parallel()
	yes := []string{
		"[redacted]",
		"[REDACTED: secret detected]",
		"value is <redacted>",
		"{{redacted}}",
		"REDACTED_TOKEN",
		"ENTIRE_REDACTED",
		"secret redacted before write",
	}
	no := []string{
		"18 passed in 0.02s",
		"tests/test_api.py::test_quote_empty PASSED",
		"the redaction policy is documented",
		"",
	}
	for _, s := range yes {
		if !IsRedacted(s) {
			t.Errorf("IsRedacted(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if IsRedacted(s) {
			t.Errorf("IsRedacted(%q) = true, want false", s)
		}
	}
}

func TestIsTruncated(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"[truncated]", "[output truncated]",
		"... [4096 bytes truncated]",
		"<<< truncated >>>",
		"output was clipped",
	} {
		if !IsTruncated(s) {
			t.Errorf("IsTruncated(%q) = false, want true", s)
		}
	}
	if IsTruncated("18 passed") {
		t.Error("IsTruncated matched ordinary output")
	}
}

// A channel with no events is absent, not present-and-empty, so a verifier
// can tell "nothing happened" from "nothing survived".
func TestBuildLedgerAbsentChannels(t *testing.T) {
	t.Parallel()
	s := &Stream{Events: []Event{{Seq: 1, Kind: AssistantText, Text: "All tests pass."}}}
	l := s.BuildLedger()
	for _, c := range []Channel{ChannelCommands, ChannelReads, ChannelEdits, ChannelPrompts} {
		if got := l.State(c); got != ChannelAbsent {
			t.Errorf("%s = %s, want absent", c, got)
		}
		if l.Reason(c) == "" {
			t.Errorf("%s carries no reason", c)
		}
	}
}

func TestBuildLedgerPresentChannels(t *testing.T) {
	t.Parallel()
	s := &Stream{Events: []Event{
		{Seq: 1, Kind: Prompt, Text: "fix the rounding"},
		{Seq: 2, Kind: FileRead, Path: "app/service.py"},
		{Seq: 3, Kind: FileEdit, Path: "app/service.py"},
		{Seq: 4, Kind: Command, Cmd: &CommandInfo{Cmd: "pytest -q", Output: "18 passed"}},
	}}
	l := s.BuildLedger()
	for _, c := range []Channel{ChannelCommands, ChannelReads, ChannelEdits, ChannelPrompts} {
		if got := l.State(c); got != ChannelPresent {
			t.Errorf("%s = %s, want present", c, got)
		}
	}
	if !l.Complete() {
		t.Error("Complete() = false for an intact transcript")
	}
	if n := len(l.Incomplete()); n != 0 {
		t.Errorf("Incomplete() = %v, want none", l.Incomplete())
	}
}

// A redacted command output makes the command channel redacted, and only
// that channel.
func TestBuildLedgerRedactedCommandOutput(t *testing.T) {
	t.Parallel()
	s := &Stream{Events: []Event{
		{Seq: 1, Kind: Prompt, Text: "fix it"},
		{Seq: 2, Kind: FileRead, Path: "app/service.py"},
		{Seq: 3, Kind: Command, Cmd: &CommandInfo{Cmd: "pytest -q", Output: "[redacted: secret detected]"}},
	}}
	l := s.BuildLedger()
	if got := l.State(ChannelCommands); got != ChannelRedacted {
		t.Errorf("commands = %s, want redacted", got)
	}
	if got := l.State(ChannelReads); got != ChannelPresent {
		t.Errorf("reads = %s, want present; redaction of one channel must not smear onto another", got)
	}
	if l.Complete() {
		t.Error("Complete() = true with a redacted channel")
	}
	inc := l.Incomplete()
	if len(inc) != 1 || inc[0] != ChannelCommands {
		t.Errorf("Incomplete() = %v, want just commands", inc)
	}
	if l.Reason(ChannelCommands) == "" {
		t.Error("a redacted channel must carry its reason")
	}
}

// Truncated output is partial, which still cannot corroborate but is a weaker
// statement than redacted.
func TestBuildLedgerTruncatedOutputIsPartial(t *testing.T) {
	t.Parallel()
	s := &Stream{Events: []Event{
		{Seq: 1, Kind: Command, Cmd: &CommandInfo{Cmd: "pytest -q", Output: "collected 400 items\n... [8192 bytes truncated]"}},
	}}
	if got := s.BuildLedger().State(ChannelCommands); got != ChannelPartial {
		t.Errorf("commands = %s, want partial", got)
	}
}

// Redacted beats partial, because a removed secret is a stronger statement
// about what a reader cannot see than a shortened output is.
func TestBuildLedgerRedactedBeatsPartial(t *testing.T) {
	t.Parallel()
	s := &Stream{Events: []Event{
		{Seq: 1, Kind: Command, Cmd: &CommandInfo{Cmd: "pytest", Output: "[redacted] ... [10 bytes truncated]"}},
	}}
	if got := s.BuildLedger().State(ChannelCommands); got != ChannelRedacted {
		t.Errorf("commands = %s, want redacted", got)
	}
}

// Records the adapter could not read, or subagent activity it did not
// examine, make every otherwise-present channel partial: the dropped record
// could have been any kind of event.
func TestBuildLedgerSkippedRecordsMakeChannelsPartial(t *testing.T) {
	t.Parallel()
	for name, s := range map[string]*Stream{
		"skipped": {
			Events:         []Event{{Seq: 1, Kind: Command, Cmd: &CommandInfo{Cmd: "pytest", Output: "18 passed"}}},
			SkippedRecords: 3,
		},
		"subagent": {
			Events:          []Event{{Seq: 1, Kind: Command, Cmd: &CommandInfo{Cmd: "pytest", Output: "18 passed"}}},
			SubagentRecords: 2,
		},
	} {
		l := s.BuildLedger()
		if got := l.State(ChannelCommands); got != ChannelPartial {
			t.Errorf("%s: commands = %s, want partial", name, got)
		}
		// An absent channel stays absent; it was not made partial by
		// something that was dropped elsewhere.
		if got := l.State(ChannelReads); got != ChannelAbsent {
			t.Errorf("%s: reads = %s, want absent", name, got)
		}
	}
}

// The Graph channel is not a transcript channel, so BuildLedger leaves it for
// the record layer and it defaults to absent.
func TestBuildLedgerLeavesGraphToTheRecordLayer(t *testing.T) {
	t.Parallel()
	s := &Stream{Events: []Event{{Seq: 1, Kind: Command, Cmd: &CommandInfo{Cmd: "pytest", Output: "ok"}}}}
	l := s.BuildLedger()
	if got := l.State(ChannelGraph); got != ChannelAbsent {
		t.Errorf("graph = %s, want absent until the record layer sets it", got)
	}
	l.Set(ChannelGraph, ChannelPresent, "")
	if got := l.State(ChannelGraph); got != ChannelPresent {
		t.Errorf("graph = %s after Set, want present", got)
	}
}

func TestLedgerNilIsSafe(t *testing.T) {
	t.Parallel()
	var l *Ledger
	if got := l.State(ChannelCommands); got != ChannelAbsent {
		t.Errorf("nil ledger State = %s, want absent", got)
	}
	if l.Reason(ChannelCommands) != "" {
		t.Error("nil ledger should have no reason")
	}
	if l.Complete() {
		t.Error("a nil ledger is not complete; nothing is known")
	}
	l.Set(ChannelGraph, ChannelPresent, "")
}
