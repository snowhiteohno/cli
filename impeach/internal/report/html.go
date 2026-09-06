package report

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io"
	"strings"

	"github.com/entireio/cli/impeach/internal/verify"
)

// The report is one self-contained file: no build step, no external requests,
// readable offline and in print. The CSS and the script are embedded rather
// than linked, which is also what lets the Content-Security-Policy forbid
// every external source.
//
//go:embed report.html.tmpl report.css report.js
var assets embed.FS

// htmlView is the template's model. It is deliberately flat and
// pre-rendered: anything needing a decision is decided in Go, so the template
// only places values and cannot quietly change a verdict.
type htmlView struct {
	Version     string
	Checkpoint  Checkpoint
	Inputs      Inputs
	Counts      Counts
	Notes       []string
	Limitations []string
	CommandsRun []string

	Rows []htmlRow
	Lead *htmlRow

	Unrequested        []htmlUnrequested
	UnrequestedSkipped string
	PromptTokens       string

	ShortCommit   string
	ShortParent   string
	Agent         string
	SessionLine   string
	ExtractorList string
	TestCommand   string
	RerunWord     string
	ModelCommand  string

	CSS          template.CSS
	JS           template.JS
	EmbeddedJSON template.JS

	// ChannelLedger and ContextSentence are the context ledger, rendered
	// above the summary strip because they qualify every count in it.
	ChannelLedger   string
	ContextSentence string
	Sensitive       bool
}

type htmlRow struct {
	Text           string
	Family         string
	Status         string
	Summary        string
	ReasonPhrases  string
	RerunLabel     string
	Turn           int
	IsUnverifiable bool
	Evidence       []htmlEvidence
}

type htmlEvidence struct {
	Label   string
	Text    string
	Detail  string
	Excerpt string
	Command string
}

type htmlUnrequested struct {
	Symbol     string
	File       string
	Kind       string
	Dependents int
	Severity   string
	IsTest     bool
}

// evidenceLabels give each evidence type a readable name.
var evidenceLabels = map[verify.EvidenceType]string{
	verify.EvidenceCommand: "command",
	verify.EvidenceEdit:    "edit",
	verify.EvidenceRead:    "read",
	verify.EvidenceEntity:  "entity",
	verify.EvidenceImpact:  "impact",
	verify.EvidenceRerun:   "rerun",
}

// HTML renders the self-contained report.
func HTML(r *Report) ([]byte, error) {
	css, err := assets.ReadFile("report.css")
	if err != nil {
		return nil, fmt.Errorf("read embedded css: %w", err)
	}
	js, err := assets.ReadFile("report.js")
	if err != nil {
		return nil, fmt.Errorf("read embedded script: %w", err)
	}
	blob, err := ToJSON(r)
	if err != nil {
		return nil, err
	}

	tmpl, err := template.New("report.html.tmpl").ParseFS(assets, "report.html.tmpl")
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	v := newHTMLView(r, css, js, blob)
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, v); err != nil {
		return nil, fmt.Errorf("render html report: %w", err)
	}
	return buf.Bytes(), nil
}

// WriteHTML renders and writes the report.
func WriteHTML(w io.Writer, r *Report) error {
	blob, err := HTML(r)
	if err != nil {
		return err
	}
	_, err = w.Write(blob)
	return err
}

func newHTMLView(r *Report, css, js, jsonBlob []byte) *htmlView {
	v := &htmlView{
		Version:     r.Version,
		Checkpoint:  r.Checkpoint,
		Inputs:      r.Inputs,
		Counts:      r.Counts,
		Notes:       ScrubAll(r.Notes),
		Limitations: r.Limitations,
		CommandsRun: ScrubAll(r.CommandsRun),
		ShortCommit: short(r.Checkpoint.Commit),
		ShortParent: short(r.Checkpoint.Parent),
		Agent:       orUnknown(r.Checkpoint.Agent),
		// A test command can be long enough to bury the rest of the header.
		// The full value stays in the commands-run list and the JSON.
		TestCommand:  orNone(truncate(Scrub(r.Inputs.TestCommand), 90)),
		ModelCommand: orNone(r.Inputs.ModelCommand),
		RerunWord:    yesNo(r.Inputs.Rerun),
		// The CSS and the script are ours, embedded at build time, and are
		// marked safe deliberately. Everything that came from a transcript
		// goes through the template's contextual escaping instead.
		CSS:          template.CSS(css),
		JS:           template.JS(js),
		EmbeddedJSON: template.JS(escapeJSONForScript(jsonBlob)),

		ChannelLedger:   r.ChannelLedger(),
		ContextSentence: r.ContextSentence(),
		Sensitive:       r.Sensitive,
	}
	if len(r.Inputs.Extractors) > 0 {
		v.ExtractorList = strings.Join(r.Inputs.Extractors, ", ")
	} else {
		v.ExtractorList = "none"
	}
	if n := len(r.Checkpoint.SessionIDs); n == 1 {
		v.SessionLine = "1 session"
	} else if n > 1 {
		v.SessionLine = fmt.Sprintf("%d sessions", n)
	}

	for _, row := range r.Rows {
		hr := newHTMLRow(row)
		v.Rows = append(v.Rows, hr)
	}
	if lead, ok := r.Lead(); ok {
		hr := newHTMLRow(lead)
		v.Lead = &hr
	}

	if r.Unrequested != nil {
		if r.Unrequested.Skipped {
			v.UnrequestedSkipped = r.Unrequested.Reason
		}
		for _, it := range r.Unrequested.Items {
			v.Unrequested = append(v.Unrequested, htmlUnrequested{
				Symbol: it.Symbol, File: it.File, Kind: string(it.Kind),
				Dependents: it.Dependents, Severity: string(it.Severity), IsTest: it.IsTest,
			})
		}
		// A reader needs to see why a match failed, but the security policy
		// is explicit that prompts never appear as full text. Listing the
		// whole prompt corpus effectively reproduced the prompt, so what is
		// published is the tokens actually searched for, plus the size of the
		// corpus they were searched in.
		v.PromptTokens = searchExplanation(r.Unrequested)
	}
	return v
}

func newHTMLRow(row Row) htmlRow {
	hr := htmlRow{
		// The claim is never truncated in HTML. Truncation is for the
		// terminal only.
		Text:           Scrub(row.Claim.Text),
		Family:         FamilyLabel(row.Claim),
		Status:         string(row.Verdict.Status),
		Summary:        Scrub(row.Verdict.Summary),
		ReasonPhrases:  phrases(row.Verdict.Reasons),
		RerunLabel:     rerunLabel(row.Verdict.Rerun),
		Turn:           row.Claim.Turn,
		IsUnverifiable: row.Verdict.Status == verify.Unverifiable,
	}
	for _, e := range row.Verdict.Evidence {
		label := evidenceLabels[e.Type]
		if label == "" {
			label = string(e.Type)
		}
		hr.Evidence = append(hr.Evidence, htmlEvidence{
			Label:   label,
			Text:    Scrub(e.Text),
			Detail:  e.Detail,
			Excerpt: Scrub(e.Excerpt),
			Command: Scrub(e.Command),
		})
	}
	return hr
}

// escapeJSONForScript makes a JSON blob safe to sit inside a script element.
//
// A reader should be able to copy the data straight out of the page, so the
// JSON is embedded rather than re-encoded. The two sequences that could end
// the element early, or open a comment that swallows the rest of the
// document, are escaped. Both remain valid JSON string escapes, so the
// embedded document still parses.
func escapeJSONForScript(blob []byte) string {
	s := string(blob)
	s = strings.ReplaceAll(s, "</", `<\/`)
	s = strings.ReplaceAll(s, "<!--", `<\!--`)
	return s
}

// searchExplanation says what was searched for and how big the corpus was,
// without reproducing the prompt.
func searchExplanation(un *verify.UnrequestedResult) string {
	seen := map[string]bool{}
	var tokens []string
	for _, it := range un.Items {
		for _, tok := range it.Tokens {
			if !seen[tok] {
				seen[tok] = true
				tokens = append(tokens, tok)
			}
		}
	}
	if len(tokens) == 0 {
		return ""
	}
	return fmt.Sprintf("Searched %d prompt tokens for: %s.",
		len(un.PromptTokens), strings.Join(tokens, ", "))
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "none"
	}
	return s
}

func orUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "unknown"
	}
	return s
}
