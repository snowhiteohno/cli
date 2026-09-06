package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/entireio/cli/impeach/internal/transcript"
)

// The JSON schema is the machine-facing contract, and it exists from the
// first release so the CI continuation path is real rather than promised.
// Field names follow docs/ARCHITECTURE.md.

type jsonReport struct {
	ImpeachVersion string            `json:"impeach_version"`
	Checkpoint     jsonCheckpoint    `json:"checkpoint"`
	Inputs         jsonInputs        `json:"inputs"`
	Claims         []jsonClaim       `json:"claims"`
	Unrequested    []jsonUnrequested `json:"unrequested"`
	// UnrequestedSkipped explains why row type five did not run, when it did
	// not. Missing data is a state, and the JSON says so rather than showing
	// an empty list that reads as "nothing found".
	UnrequestedSkipped string `json:"unrequested_skipped,omitempty"`
	// PromptTokenCount is the size of the corpus the symbol tokens were
	// searched in. The corpus itself is not published: the security policy
	// keeps prompts out of reports as full text, and a full token list
	// reconstructs the prompt almost verbatim. Each item carries the tokens
	// that were actually searched for, which is what explains a failed match.
	PromptTokenCount int `json:"prompt_token_count,omitempty"`
	// Channels is the context ledger: each evidence channel and its state,
	// with the reason for anything short of present. A machine consumer needs
	// this to know whether a corroborated count means anything.
	Channels        map[string]jsonChannel `json:"channels"`
	ContextComplete bool                   `json:"context_complete"`
	ContextNote     string                 `json:"context_note,omitempty"`
	GatedClaims     int                    `json:"claims_gated_by_channel"`
	Sensitive       bool                   `json:"sensitive_mode"`
	Counts          jsonCounts             `json:"counts"`
	Notes           []string               `json:"notes,omitempty"`
	Limitations     []string               `json:"limitations"`
	CommandsRun     []string               `json:"commands_run"`
}

type jsonCheckpoint struct {
	ID         string   `json:"id"`
	Commit     string   `json:"commit"`
	Parent     string   `json:"parent"`
	SessionIDs []string `json:"session_ids"`
	Agent      string   `json:"agent"`
	IsMerge    bool     `json:"is_merge,omitempty"`
}

type jsonInputs struct {
	Adapter      string          `json:"adapter"`
	Extractors   []string        `json:"extractors"`
	TestCommand  string          `json:"test_command"`
	Rerun        bool            `json:"rerun"`
	ModelCommand string          `json:"model_command,omitempty"`
	Channels     map[string]bool `json:"channels"`
}

type jsonClaim struct {
	ID        string         `json:"id"`
	Text      string         `json:"text"`
	Family    string         `json:"family"`
	Scope     string         `json:"scope,omitempty"`
	Subjects  []string       `json:"subjects,omitempty"`
	Turn      int            `json:"turn"`
	Seq       int            `json:"seq"`
	Extractor string         `json:"extractor"`
	Verdict   string         `json:"verdict"`
	Reasons   []string       `json:"reasons,omitempty"`
	Summary   string         `json:"summary"`
	Evidence  []jsonEvidence `json:"evidence"`
	Rerun     string         `json:"rerun"`
}

type jsonEvidence struct {
	Type    string `json:"type"`
	Seq     int    `json:"seq,omitempty"`
	Text    string `json:"text,omitempty"`
	Excerpt string `json:"output_excerpt,omitempty"`
	Detail  string `json:"parsed,omitempty"`
	Command string `json:"reproduce,omitempty"`
}

type jsonUnrequested struct {
	Symbol     string   `json:"symbol"`
	File       string   `json:"file"`
	Kind       string   `json:"kind"`
	Dependents int      `json:"dependents"`
	Severity   string   `json:"severity"`
	IsTest     bool     `json:"is_test,omitempty"`
	Tokens     []string `json:"tokens_searched,omitempty"`
}

type jsonChannel struct {
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

type jsonCounts struct {
	Corroborated   int `json:"corroborated"`
	Impeached      int `json:"impeached"`
	Uncorroborated int `json:"uncorroborated"`
	Unverifiable   int `json:"unverifiable"`
	Unrequested    int `json:"unrequested"`
}

// ToJSON renders the report as the documented schema.
//
// Every excerpt goes through Scrub first. A transcript is never written to
// --out in whole or in part; what appears here is a bounded, scrubbed excerpt
// of a command's own output.
func ToJSON(r *Report) ([]byte, error) {
	out := jsonReport{
		ImpeachVersion: r.Version,
		Checkpoint: jsonCheckpoint{
			ID: r.Checkpoint.ID, Commit: r.Checkpoint.Commit, Parent: r.Checkpoint.Parent,
			SessionIDs: r.Checkpoint.SessionIDs, Agent: r.Checkpoint.Agent,
			IsMerge: r.Checkpoint.IsMerge,
		},
		Inputs: jsonInputs{
			Adapter: r.Inputs.Adapter, Extractors: r.Inputs.Extractors,
			TestCommand: r.Inputs.TestCommand, Rerun: r.Inputs.Rerun,
			ModelCommand: r.Inputs.ModelCommand, Channels: r.Inputs.Channels,
		},
		Counts: jsonCounts{
			Corroborated: r.Counts.Corroborated, Impeached: r.Counts.Impeached,
			Uncorroborated: r.Counts.Uncorroborated, Unverifiable: r.Counts.Unverifiable,
			Unrequested: r.Counts.Unrequested,
		},
		// Notes can carry a stderr excerpt and commands can carry a flag
		// value, so both go through the scrub. Leaving commands_run
		// unscrubbed leaked a credential into the JSON and into the copy the
		// HTML report embeds.
		Channels:        map[string]jsonChannel{},
		ContextComplete: r.Ledger == nil || r.Ledger.Complete(),
		ContextNote:     r.ContextSentence(),
		GatedClaims:     r.GatedByChannel(),
		Sensitive:       r.Sensitive,
		Notes:           ScrubAll(r.Notes),
		Limitations:     r.Limitations,
		CommandsRun:     ScrubAll(r.CommandsRun),
	}
	if out.Inputs.Channels == nil {
		out.Inputs.Channels = map[string]bool{}
	}
	if out.Inputs.Extractors == nil {
		out.Inputs.Extractors = []string{}
	}
	if out.Checkpoint.SessionIDs == nil {
		out.Checkpoint.SessionIDs = []string{}
	}
	if out.Limitations == nil {
		out.Limitations = []string{}
	}
	if out.CommandsRun == nil {
		out.CommandsRun = []string{}
	}

	if r.Ledger != nil {
		for _, c := range transcript.AllChannels {
			out.Channels[string(c)] = jsonChannel{
				State:  string(r.Ledger.State(c)),
				Reason: r.Ledger.Reason(c),
			}
		}
	}

	out.Claims = make([]jsonClaim, 0, len(r.Rows))
	for _, row := range r.Rows {
		c := jsonClaim{
			ID:        row.Claim.ID,
			Text:      Scrub(row.Claim.Text),
			Family:    row.Claim.Family.String(),
			Scope:     row.Claim.Scope.String(),
			Subjects:  row.Claim.Subjects,
			Turn:      row.Claim.Turn,
			Seq:       row.Claim.Seq,
			Extractor: row.Claim.Extractor,
			Verdict:   string(row.Verdict.Status),
			Reasons:   row.Verdict.Reasons,
			Summary:   Scrub(row.Verdict.Summary),
			Rerun:     string(row.Verdict.Rerun),
			Evidence:  make([]jsonEvidence, 0, len(row.Verdict.Evidence)),
		}
		for _, e := range row.Verdict.Evidence {
			c.Evidence = append(c.Evidence, jsonEvidence{
				Type:    string(e.Type),
				Seq:     e.Seq,
				Text:    Scrub(e.Text),
				Excerpt: Scrub(e.Excerpt),
				Detail:  e.Detail,
				Command: Scrub(e.Command),
			})
		}
		out.Claims = append(out.Claims, c)
	}

	out.Unrequested = make([]jsonUnrequested, 0)
	if r.Unrequested != nil {
		if r.Unrequested.Skipped {
			out.UnrequestedSkipped = r.Unrequested.Reason
		}
		out.PromptTokenCount = len(r.Unrequested.PromptTokens)
		for _, it := range r.Unrequested.Items {
			out.Unrequested = append(out.Unrequested, jsonUnrequested{
				Symbol: it.Symbol, File: it.File, Kind: string(it.Kind),
				Dependents: it.Dependents, Severity: string(it.Severity),
				IsTest: it.IsTest, Tokens: it.Tokens,
			})
		}
	}

	// HTML escaping is off because this file is read by people as well as
	// machines, and a path or a claim containing < should stay legible. The
	// HTML report escapes on its own terms when it embeds this.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return nil, fmt.Errorf("render json report: %w", err)
	}
	return buf.Bytes(), nil
}

// WriteJSON renders and writes the report.
func WriteJSON(w io.Writer, r *Report) error {
	blob, err := ToJSON(r)
	if err != nil {
		return err
	}
	_, err = w.Write(blob)
	return err
}
