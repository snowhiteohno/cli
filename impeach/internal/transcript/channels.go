package transcript

import (
	"regexp"
	"strings"
)

// ChannelState is how much of an evidence channel actually survived into the
// transcript.
//
// The distinction between absent and redacted matters for what a verdict may
// conclude. An absent channel says nothing at all. A redacted one says
// something happened and hid what it was, which is still enough to contradict
// a claim but never enough to confirm one.
type ChannelState string

const (
	// ChannelPresent means the channel is there and readable.
	ChannelPresent ChannelState = "present"
	// ChannelPartial means some of it is there. Records were dropped, output
	// was truncated, or a subagent's activity was not examined.
	ChannelPartial ChannelState = "partial"
	// ChannelRedacted means content was removed before the transcript was
	// written. Secret redaction is the usual cause.
	ChannelRedacted ChannelState = "redacted"
	// ChannelAbsent means the channel carries nothing.
	ChannelAbsent ChannelState = "absent"
)

// Corroborating reports whether a channel in this state may support a
// corroborated verdict.
//
// Only a present channel may. This is the asymmetry the whole change rests
// on: a partial or redacted channel can still contradict a claim, because a
// contradiction survives redaction, but it can never confirm one, because
// absence of evidence is not evidence of honesty.
func (s ChannelState) Corroborating() bool {
	return s == ChannelPresent
}

// Complete reports whether the channel is fully intact.
func (s ChannelState) Complete() bool {
	return s == ChannelPresent || s == ChannelAbsent
}

// Channel names an evidence channel.
type Channel string

const (
	ChannelCommands Channel = "commands"
	ChannelReads    Channel = "reads"
	ChannelEdits    Channel = "edits"
	ChannelPrompts  Channel = "prompts"
	// ChannelGraph is the entity diff and impact from Entire Graph. It is not
	// a transcript channel: Graph reads the code at the commit, so redaction
	// of the transcript leaves it untouched. Its state is set by the record
	// layer rather than by the adapter, because only the record layer knows
	// whether Graph answered.
	ChannelGraph Channel = "graph"
)

// Ledger is the state of every evidence channel for one transcript, with the
// reason recorded for anything short of present so a report can name it.
type Ledger struct {
	States  map[Channel]ChannelState
	Reasons map[Channel]string
}

// State returns a channel's state, defaulting to absent for one never set.
func (l *Ledger) State(c Channel) ChannelState {
	if l == nil || l.States == nil {
		return ChannelAbsent
	}
	if s, ok := l.States[c]; ok {
		return s
	}
	return ChannelAbsent
}

// Reason returns why a channel is not present, or the empty string.
func (l *Ledger) Reason(c Channel) string {
	if l == nil || l.Reasons == nil {
		return ""
	}
	return l.Reasons[c]
}

// Complete reports whether every channel is intact, meaning nothing is
// partial or redacted. An absent channel is complete: it is honestly empty.
func (l *Ledger) Complete() bool {
	if l == nil {
		return false
	}
	for _, s := range l.States {
		if !s.Complete() {
			return false
		}
	}
	return true
}

// Incomplete returns the channels that are partial or redacted, in a stable
// order so a report reads the same way twice.
func (l *Ledger) Incomplete() []Channel {
	var out []Channel
	for _, c := range AllChannels {
		if s := l.State(c); !s.Complete() {
			out = append(out, c)
		}
	}
	return out
}

// AllChannels is every evidence channel, in the order a report lists them.
var AllChannels = []Channel{
	ChannelCommands, ChannelReads, ChannelEdits, ChannelPrompts, ChannelGraph,
}

// Set records a channel's state and why.
func (l *Ledger) Set(c Channel, state ChannelState, reason string) {
	if l == nil {
		return
	}
	if l.States == nil {
		l.States = map[Channel]ChannelState{}
	}
	if l.Reasons == nil {
		l.Reasons = map[Channel]string{}
	}
	l.States[c] = state
	if reason != "" {
		l.Reasons[c] = reason
	} else {
		delete(l.Reasons, c)
	}
}

// redactionMarkers are the shapes a redacted span leaves behind.
//
// Entire replaces detected secrets before a checkpoint is written, and other
// tooling in the chain may do the same, so this is a set of markers rather
// than one. The adapter never tries to reconstruct what was removed; it only
// notices that something was.
var redactionMarkers = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\[redacted[^\]]*\]`),
	regexp.MustCompile(`(?i)<redacted[^>]*>`),
	regexp.MustCompile(`(?i)\{\{redacted[^}]*\}\}`),
	regexp.MustCompile(`(?i)\bREDACTED_(SECRET|TOKEN|KEY|CREDENTIAL)\b`),
	regexp.MustCompile(`(?i)\bENTIRE_REDACTED\b`),
	regexp.MustCompile(`‹redacted›`),
	regexp.MustCompile(`(?i)\bsecret\s+redacted\b`),
}

// truncationMarkers are the shapes a shortened output leaves behind. A
// truncated command output can still contradict a claim, but a success it does
// not show may simply have been cut off.
var truncationMarkers = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\[(output )?truncated[^\]]*\]`),
	regexp.MustCompile(`(?i)\.\.\.\s*\[\d+ (bytes|lines|characters) truncated\]`),
	regexp.MustCompile(`(?i)<<<\s*truncated\s*>>>`),
	regexp.MustCompile(`(?i)\boutput (was )?(clipped|truncated)\b`),
}

// IsRedacted reports whether a string carries a redaction marker.
func IsRedacted(s string) bool {
	for _, re := range redactionMarkers {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// IsTruncated reports whether a string carries a truncation marker.
func IsTruncated(s string) bool {
	for _, re := range truncationMarkers {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// BuildLedger works out the state of each channel from the parsed stream.
//
// The order of precedence is deliberate. Absent beats everything, because a
// channel with no events cannot be partially anything. Redacted beats partial,
// because a removed secret is a stronger statement about what a reader cannot
// see than a dropped record is.
func (s *Stream) BuildLedger() *Ledger {
	l := &Ledger{
		States:  map[Channel]ChannelState{},
		Reasons: map[Channel]string{},
	}

	counts := map[Channel]int{}
	redacted := map[Channel]bool{}
	truncated := map[Channel]bool{}

	for i := range s.Events {
		e := &s.Events[i]
		switch e.Kind {
		case Command:
			counts[ChannelCommands]++
			if e.Cmd != nil {
				if IsRedacted(e.Cmd.Output) || IsRedacted(e.Cmd.Cmd) {
					redacted[ChannelCommands] = true
				}
				if IsTruncated(e.Cmd.Output) {
					truncated[ChannelCommands] = true
				}
			}
		case FileRead:
			counts[ChannelReads]++
			if IsRedacted(e.Path) {
				redacted[ChannelReads] = true
			}
		case FileEdit:
			counts[ChannelEdits]++
			if IsRedacted(e.Path) {
				redacted[ChannelEdits] = true
			}
		case Prompt:
			counts[ChannelPrompts]++
			if IsRedacted(e.Text) {
				redacted[ChannelPrompts] = true
			}
		}
	}

	// Only the transcript channels. ChannelGraph is the record layer's to set.
	for _, c := range []Channel{ChannelCommands, ChannelReads, ChannelEdits, ChannelPrompts} {
		switch {
		case counts[c] == 0:
			l.States[c] = ChannelAbsent
			l.Reasons[c] = "the transcript carries no " + string(c) + " records"
		case redacted[c]:
			l.States[c] = ChannelRedacted
			l.Reasons[c] = "content in the " + string(c) + " channel was redacted before the transcript was written"
		case truncated[c]:
			l.States[c] = ChannelPartial
			l.Reasons[c] = "output in the " + string(c) + " channel was truncated"
		default:
			l.States[c] = ChannelPresent
		}
	}

	// Records the adapter could not read at all, and subagent activity that
	// v1 does not examine, make every channel partial rather than present:
	// the thing that was dropped could have been any kind of event.
	if s.SkippedRecords > 0 || s.SubagentRecords > 0 {
		why := []string{}
		if s.SkippedRecords > 0 {
			why = append(why, "records the adapter could not read")
		}
		if s.SubagentRecords > 0 {
			why = append(why, "subagent records not examined")
		}
		note := strings.Join(why, " and ")
		for c, st := range l.States {
			if st == ChannelPresent {
				l.States[c] = ChannelPartial
				l.Reasons[c] = "the transcript has " + note
			}
		}
	}
	return l
}
