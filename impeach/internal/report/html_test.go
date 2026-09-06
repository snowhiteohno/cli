package report

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/entireio/cli/impeach/internal/claims"
	"github.com/entireio/cli/impeach/internal/record"
	"github.com/entireio/cli/impeach/internal/transcript"
	"github.com/entireio/cli/impeach/internal/verify"
)

func renderHTML(t *testing.T, r *Report) string {
	t.Helper()
	blob, err := HTML(r)
	if err != nil {
		t.Fatalf("HTML() error = %v", err)
	}
	return string(blob)
}

func fullReport() *Report {
	impeached := row("c1", verify.Impeached, 7,
		[]string{verify.ReasonScopeMismatch, verify.ReasonStale, verify.ReasonContradictedRerun},
		record.RerunNewFailures, "All tests pass.")
	impeached.Verdict.Summary = "One of four test files ran, and service.py was edited afterwards."
	impeached.Verdict.Evidence = []verify.Evidence{
		{Type: verify.EvidenceCommand, Seq: 41, Text: "pytest tests/test_api.py",
			Excerpt: "4 passed in 0.00s", Detail: "pass"},
		{Type: verify.EvidenceEdit, Seq: 58, Text: "app/service.py", Detail: "edited after the last run"},
		{Type: verify.EvidenceRerun, Detail: "new_failures", Text: "3 new failures",
			Command: "entire graph verify --repo /wt/head"},
	}

	corr := row("c2", verify.Corroborated, 2, nil, record.RerunPass, "Added `test_refund_rounding`.")
	corr.Claim.Family = claims.Structural
	unc := row("c3", verify.Uncorroborated, 3, nil, record.RerunNotRun, "Verified the migration path.")
	unc.Claim.Family = claims.Reading
	unv := row("c4", verify.Unverifiable, 4, nil, record.RerunNotRun, "`handle` is isolated.")
	unv.Claim.Family = claims.Safety

	un := &verify.UnrequestedResult{
		Items: []verify.Unrequested{{
			Symbol: "_legacy_shim", File: "app/service.py", Kind: record.Added,
			Dependents: 0, Severity: verify.SeverityNone,
			Tokens: []string{"legacy", "shim"},
		}},
		PromptTokens: []string{"add", "discount", "rounding"},
	}
	return New(testCheckpoint(), testInputs(), []Row{impeached, corr, unc, unv}, un,
		[]string{"This transcript carries no read records."},
		[]string{"Claim detection is pattern based."},
		[]string{"entire graph commit HEAD --json"})
}

func TestHTMLStructure(t *testing.T) {
	t.Parallel()
	out := renderHTML(t, fullReport())
	for _, want := range []string{
		"<!doctype html>",
		`<html lang="en">`,
		"Impeach 0.1.0 report",
		"01M1TET4N33VMY0DTHNZKV5HT9",
		"Lead impeachment",
		"Summary",
		"Claims",
		"Unrequested changes",
		"_legacy_shim",
		"Searched 3 prompt tokens for:",
		"Commands run",
		"nothing in this file was produced by a model",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("html missing %q", want)
		}
	}
}

// A strict policy is the point of embedding everything: no external script,
// style, font, image or connection is permitted.
func TestHTMLContentSecurityPolicy(t *testing.T) {
	t.Parallel()
	out := renderHTML(t, fullReport())
	want := `content="default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; img-src data:"`
	if !strings.Contains(out, want) {
		t.Errorf("missing or altered CSP meta tag, want %s", want)
	}
}

// No external URL may appear at all. A report that phones home is not
// readable offline and leaks who read it.
func TestHTMLMakesNoExternalRequests(t *testing.T) {
	t.Parallel()
	out := renderHTML(t, fullReport())
	for _, forbidden := range []string{
		`src="http`, `href="http`, `@import`, `url(http`, "fonts.googleapis", "cdn.",
	} {
		if strings.Contains(out, forbidden) {
			t.Errorf("html references something external: %q", forbidden)
		}
	}
	// Nor a stylesheet link, since the CSS is inlined.
	if regexp.MustCompile(`(?i)<link[^>]+stylesheet`).MatchString(out) {
		t.Error("html links an external stylesheet")
	}
}

// The page must be complete without JavaScript, so every panel is present in
// the markup and open by default. The script only collapses them.
func TestHTMLWorksWithScriptsDisabled(t *testing.T) {
	t.Parallel()
	out := renderHTML(t, fullReport())
	opens := strings.Count(out, "<details class=\"row")
	if opens != 4 {
		t.Fatalf("found %d rows, want 4", opens)
	}
	// Every details element carries the open attribute in the markup.
	if n := strings.Count(out, " open>"); n != 4 {
		t.Errorf("%d rows are open by default, want all 4; a reader without JS must see the evidence", n)
	}
	// And the evidence itself is in the document, not fetched.
	for _, want := range []string{"pytest tests/test_api.py", "4 passed in 0.00s", "app/service.py"} {
		if !strings.Contains(out, want) {
			t.Errorf("evidence %q is not in the markup", want)
		}
	}
}

// From the spec's QA checklist. A claim containing markup and a symbol named
// like a closing tag must render as text.
func TestHTMLEscapesHostileClaimText(t *testing.T) {
	t.Parallel()
	rw := row("c1", verify.Impeached, 1, []string{verify.ReasonStale}, record.RerunPass,
		`<script>alert('xss')</script> and <img src=x onerror=alert(1)>`)
	rw.Claim.Subjects = []string{"</script>"}
	rw.Verdict.Summary = `</details><script>alert(2)</script>`
	rw.Verdict.Evidence = []verify.Evidence{{
		Type: verify.EvidenceCommand, Seq: 1,
		Text:    `pytest "</script><script>alert(3)</script>"`,
		Excerpt: `</pre><script>alert(4)</script>`,
	}}
	out := renderHTML(t, New(testCheckpoint(), testInputs(), []Row{rw}, nil, nil, nil, nil))

	// Assert on the document body, meaning everything before the embedded
	// data block. Content inside a script element is raw text that only
	// </script can end, and that sequence is escaped, so hostile markup
	// appearing there is inert. Counting script tags across the whole file
	// would just be counting that inert text.
	cut := strings.Index(out, `<script type="application/json"`)
	if cut < 0 {
		t.Fatal("embedded data block not found")
	}
	body := out[:cut]

	if n := strings.Count(body, "<script"); n != 0 {
		t.Errorf("found %d script elements in the document body, want none", n)
	}
	for _, injected := range []string{
		"<script>alert('xss')</script>",
		"<img src=x onerror=alert(1)>",
		"</details><script>alert(2)</script>",
		"</pre><script>alert(4)</script>",
	} {
		if strings.Contains(body, injected) {
			t.Errorf("hostile markup reached the document body unescaped: %q", injected)
		}
	}
	// It must still be readable as text.
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Error("claim text should be escaped and still visible")
	}
}

// The embedded JSON must be copyable and must not be able to close its own
// element or open a comment that swallows the document.
func TestHTMLEmbeddedJSONIsSafeAndParseable(t *testing.T) {
	t.Parallel()
	rw := row("c1", verify.Impeached, 1, []string{verify.ReasonStale}, record.RerunPass,
		`a claim with </script> and <!-- a comment --> in it`)
	out := renderHTML(t, New(testCheckpoint(), testInputs(), []Row{rw}, nil, nil, nil, nil))

	start := strings.Index(out, `<script type="application/json" id="impeach-data">`)
	if start < 0 {
		t.Fatal("embedded data block not found")
	}
	start += len(`<script type="application/json" id="impeach-data">`)
	end := strings.Index(out[start:], "</script>")
	if end < 0 {
		t.Fatal("embedded data block is not terminated")
	}
	body := out[start : start+end]

	if strings.Contains(body, "</") {
		t.Errorf("embedded JSON contains an unescaped </, which could end the element early")
	}
	if strings.Contains(body, "<!--") {
		t.Errorf("embedded JSON contains an unescaped <!--")
	}
	// Reverse the two escapes and confirm it is still valid JSON, so a reader
	// really can copy the data out.
	restored := strings.ReplaceAll(body, `<\/`, "</")
	restored = strings.ReplaceAll(restored, `<\!--`, "<!--")
	var doc map[string]any
	if err := json.Unmarshal([]byte(restored), &doc); err != nil {
		t.Fatalf("embedded JSON does not parse: %v\n%s", err, restored)
	}
	if doc["impeach_version"] != Version {
		t.Errorf("embedded JSON is not the report: %v", doc["impeach_version"])
	}
}

// Colour alone must not carry meaning, so each verdict also appears as its
// word, and unverifiable additionally carries a hatch for greyscale and print.
func TestHTMLVerdictsAreLegibleWithoutColour(t *testing.T) {
	t.Parallel()
	out := renderHTML(t, fullReport())
	for _, word := range []string{"corroborated", "impeached", "uncorroborated", "unverifiable"} {
		if !strings.Contains(out, ">"+word+"<") {
			t.Errorf("verdict %q does not appear as a word", word)
		}
	}
	if !strings.Contains(out, "hatched") {
		t.Error("the unverifiable row carries no hatch; colour would be the only signal")
	}
	if !strings.Contains(out, "prefers-color-scheme: dark") {
		t.Error("no dark scheme; the spec requires both")
	}
	if !strings.Contains(out, "@media print") {
		t.Error("no print styles")
	}
}

// Filters and the disclosure rows have to be reachable by keyboard and
// announce their state.
func TestHTMLAccessibility(t *testing.T) {
	t.Parallel()
	out := renderHTML(t, fullReport())
	if n := strings.Count(out, `aria-pressed="false"`); n != 4 {
		t.Errorf("%d filter toggles carry aria-pressed, want 4", n)
	}
	if !strings.Contains(out, "<caption>") {
		t.Error("the unrequested table has no caption")
	}
	if !strings.Contains(out, `scope="col"`) {
		t.Error("table headers are missing scope")
	}
	if !strings.Contains(out, "outline: 2px solid var(--focus)") {
		t.Error("no visible focus ring")
	}
	if !strings.Contains(out, `type="button"`) {
		t.Error("filters should be real buttons")
	}
}

// The lead is the impeached claim with the most reasons, and it is typeset as
// testimony followed by the record.
func TestHTMLLeadImpeachment(t *testing.T) {
	t.Parallel()
	out := renderHTML(t, fullReport())
	if !strings.Contains(out, "Testimony") || !strings.Contains(out, "Record") {
		t.Error("lead block is missing its testimony or record label")
	}
	if !strings.Contains(out, `class="testimony"`) {
		t.Error("the lead claim is not typeset as testimony")
	}
	if !strings.Contains(out, "turn 7") {
		t.Error("the lead should carry its turn number")
	}
	if !strings.Contains(out, "scope mismatch") {
		t.Error("the lead should list its reasons as readable phrases")
	}
}

func TestHTMLNoImpeachmentSaysSo(t *testing.T) {
	t.Parallel()
	r := New(testCheckpoint(), testInputs(),
		[]Row{row("c1", verify.Corroborated, 1, nil, record.RerunPass, "All tests pass.")},
		nil, nil, nil, nil)
	out := renderHTML(t, r)
	if !strings.Contains(out, "No claim was impeached.") {
		t.Error("expected the no-impeachment message")
	}
}

func TestHTMLNoClaimsSaysWhatToDoNext(t *testing.T) {
	t.Parallel()
	out := renderHTML(t, New(testCheckpoint(), testInputs(), nil, nil, nil, nil, nil))
	if !strings.Contains(out, "No claims matched the pattern library") {
		t.Error("expected the no-claims message")
	}
	if !strings.Contains(out, "--model") || !strings.Contains(out, "--full") {
		t.Error("the no-claims message should point at the next step")
	}
}

// Row type five works from the entity diff and the prompts, so it renders
// even when no claim was extracted.
func TestHTMLUnrequestedRendersWithoutClaims(t *testing.T) {
	t.Parallel()
	un := &verify.UnrequestedResult{
		Items:        []verify.Unrequested{{Symbol: "_legacy_shim", File: "app/service.py", Kind: record.Added}},
		PromptTokens: []string{"discount"},
	}
	out := renderHTML(t, New(testCheckpoint(), testInputs(), nil, un, nil, nil, nil))
	if !strings.Contains(out, "_legacy_shim") {
		t.Error("unrequested block missing with no claims present")
	}
}

func TestHTMLUnrequestedSkippedIsExplicit(t *testing.T) {
	t.Parallel()
	un := &verify.UnrequestedResult{Skipped: true, Reason: "the transcript carries no prompts"}
	out := renderHTML(t, New(testCheckpoint(), testInputs(), nil, un, nil, nil, nil))
	if !strings.Contains(out, "Not checked:") {
		t.Error("a skipped detector must say so rather than look empty")
	}
}

// The security policy keeps prompts out of reports as full text. Publishing
// the whole token corpus reconstructs the prompt almost verbatim, so what the
// report shows is the tokens searched for and the size of the corpus.
func TestHTMLDoesNotReproduceThePromptCorpus(t *testing.T) {
	t.Parallel()
	un := &verify.UnrequestedResult{
		Items: []verify.Unrequested{{
			Symbol: "_legacy_shim", File: "app/service.py", Kind: record.Added,
			Tokens: []string{"legacy", "shim"},
		}},
		PromptTokens: []string{"refactor", "the", "pricing", "service", "urgently", "before", "friday"},
	}
	out := renderHTML(t, New(testCheckpoint(), testInputs(), nil, un, nil, nil, nil))

	for _, word := range []string{"urgently", "friday", "refactor"} {
		if strings.Contains(out, word) {
			t.Errorf("the report reproduced prompt text: %q", word)
		}
	}
	if !strings.Contains(out, "Searched 7 prompt tokens for: legacy, shim.") {
		t.Error("expected the count and the tokens actually searched for")
	}
}

// Credentials must not reach the HTML any more than the JSON.
func TestHTMLScrubsCredentials(t *testing.T) {
	t.Parallel()
	secret := "gh" + "p_" + strings.Repeat("A", 36)
	rw := row("c1", verify.Impeached, 1, []string{verify.ReasonStale}, record.RerunPass, "exported "+secret)
	rw.Verdict.Evidence = []verify.Evidence{{
		Type: verify.EvidenceCommand, Seq: 1, Text: "curl -H 'Bearer " + secret + "'", Excerpt: secret,
	}}
	out := renderHTML(t, New(testCheckpoint(), testInputs(), []Row{rw}, nil, nil, nil, []string{"echo " + secret}))
	if strings.Contains(out, secret) {
		t.Error("the html report leaked a credential")
	}
	if !strings.Contains(out, "[redacted]") {
		t.Error("expected a redaction marker")
	}
}

// Wide content scrolls inside its own container, so the page body never
// scrolls sideways at the narrowest supported width.
func TestHTMLWideContentIsContained(t *testing.T) {
	t.Parallel()
	out := renderHTML(t, fullReport())
	if !strings.Contains(out, "overflow-x: auto") {
		t.Error("no horizontal scroll container for wide content")
	}
	if !strings.Contains(out, "max-width: 620px") {
		t.Error("no narrow-width handling")
	}
}

// The context ledger goes above the summary strip, because it qualifies every
// count in it. A reader who sees a corroborated count without knowing a
// channel was redacted has been told something misleading.
func TestHTMLRendersTheContextLedger(t *testing.T) {
	t.Parallel()
	l := &transcript.Ledger{}
	l.Set(transcript.ChannelCommands, transcript.ChannelRedacted,
		"content in the commands channel was redacted before the transcript was written")
	l.Set(transcript.ChannelReads, transcript.ChannelPresent, "")
	l.Set(transcript.ChannelEdits, transcript.ChannelPresent, "")
	l.Set(transcript.ChannelPrompts, transcript.ChannelPresent, "")
	l.Set(transcript.ChannelGraph, transcript.ChannelPresent, "")

	rw := row("c1", verify.Unverifiable, 3, []string{verify.ReasonChannelIncomplete},
		record.RerunNotRun, "All tests pass.")
	r := New(testCheckpoint(), testInputs(), []Row{rw}, nil, nil, nil, nil)
	r.Ledger = l

	out := renderHTML(t, r)
	if !strings.Contains(out, "commands redacted") {
		t.Errorf("the ledger does not name the redacted channel:\n%s", out)
	}
	if !strings.Contains(out, "reads present") {
		t.Error("intact channels must be reported too, or the ledger reads as a blanket warning")
	}
	if !strings.Contains(out, "Context is incomplete") {
		t.Error("expected the incomplete-context sentence")
	}
	// It has to sit above the summary strip, not below it.
	ctx := strings.Index(out, "Context is incomplete")
	strip := strings.Index(out, "corroborated</button>")
	if ctx < 0 || strip < 0 || ctx > strip {
		t.Errorf("the context sentence must appear above the summary strip (ctx %d, strip %d)", ctx, strip)
	}
}

// With no ledger the section is omitted entirely rather than rendering an
// empty line or a bare full stop.
func TestHTMLOmitsContextSectionWithoutALedger(t *testing.T) {
	t.Parallel()
	out := renderHTML(t, New(testCheckpoint(), testInputs(), nil, nil, nil, nil, nil))
	if strings.Contains(out, "<h2>Context</h2>") {
		t.Error("the context section should be omitted when there is no ledger")
	}
}

// An intact run still reports its ledger, so a reader can see the run had
// everything rather than having to infer it from the absence of a warning.
func TestHTMLReportsAnIntactLedgerToo(t *testing.T) {
	t.Parallel()
	l := &transcript.Ledger{}
	for _, c := range transcript.AllChannels {
		l.Set(c, transcript.ChannelPresent, "")
	}
	r := New(testCheckpoint(), testInputs(), nil, nil, nil, nil, nil)
	r.Ledger = l

	out := renderHTML(t, r)
	if !strings.Contains(out, "<h2>Context</h2>") {
		t.Error("an intact ledger should still be reported")
	}
	if strings.Contains(out, "Context is incomplete") {
		t.Error("an intact ledger must not claim incomplete context")
	}
}

// Sensitive mode is stated verbatim in the HTML header, as it is in the
// terminal.
func TestHTMLStatesSensitiveMode(t *testing.T) {
	t.Parallel()
	r := New(testCheckpoint(), testInputs(), nil, nil, nil, nil, nil)
	r.Sensitive = true
	out := renderHTML(t, r)
	if !strings.Contains(out, "Mode: sensitive") {
		t.Error("the HTML report does not state sensitive mode")
	}
	if !strings.Contains(out, "nothing leaves this machine") {
		t.Error("the mode line should say what it means")
	}
}
