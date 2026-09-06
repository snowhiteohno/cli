package report

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/entireio/cli/impeach/internal/claims"
	"github.com/entireio/cli/impeach/internal/record"
	"github.com/entireio/cli/impeach/internal/verify"
)

func decodeJSON(t *testing.T, r *Report) map[string]any {
	t.Helper()
	blob, err := ToJSON(r)
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(blob, &out); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, blob)
	}
	return out
}

// The JSON schema is the machine-facing contract, so its top-level shape is
// pinned rather than left to drift.
func TestJSONTopLevelSchema(t *testing.T) {
	t.Parallel()
	rw := row("c1", verify.Impeached, 7, []string{verify.ReasonStale, verify.ReasonScopeMismatch},
		record.RerunNewFailures, "All tests pass.")
	r := New(testCheckpoint(), testInputs(), []Row{rw}, nil,
		[]string{"a note"}, []string{"a limitation"}, []string{"entire graph commit HEAD --json"})

	got := decodeJSON(t, r)
	for _, key := range []string{
		"impeach_version", "checkpoint", "inputs", "claims",
		"unrequested", "counts", "limitations", "commands_run",
	} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing top-level key %q", key)
		}
	}
	if got["impeach_version"] != Version {
		t.Errorf("impeach_version = %v, want %q", got["impeach_version"], Version)
	}

	cp := got["checkpoint"].(map[string]any)
	for _, key := range []string{"id", "commit", "parent", "session_ids", "agent"} {
		if _, ok := cp[key]; !ok {
			t.Errorf("checkpoint missing %q", key)
		}
	}
	in := got["inputs"].(map[string]any)
	for _, key := range []string{"adapter", "extractors", "test_command", "rerun", "channels"} {
		if _, ok := in[key]; !ok {
			t.Errorf("inputs missing %q", key)
		}
	}
}

func TestJSONClaimFields(t *testing.T) {
	t.Parallel()
	rw := row("c1", verify.Impeached, 7, []string{verify.ReasonStale}, record.RerunNewFailures, "All tests pass.")
	rw.Claim.Family = claims.Execution
	rw.Claim.Scope = claims.ScopeAll
	rw.Claim.Subjects = []string{"tests/test_api.py"}
	rw.Claim.Extractor = "pattern"
	rw.Verdict.Evidence = []verify.Evidence{{
		Type: verify.EvidenceCommand, Seq: 41, Text: "pytest tests/test_api.py",
		Excerpt: "4 passed in 0.00s", Detail: "pass", Command: "entire graph verify --repo /wt/head",
	}}
	r := New(testCheckpoint(), testInputs(), []Row{rw}, nil, nil, nil, nil)

	got := decodeJSON(t, r)
	list := got["claims"].([]any)
	if len(list) != 1 {
		t.Fatalf("claims = %d, want 1", len(list))
	}
	c := list[0].(map[string]any)
	if c["id"] != "c1" || c["verdict"] != "impeached" || c["family"] != "execution" {
		t.Errorf("claim = %+v", c)
	}
	if c["rerun"] != "new_failures" {
		t.Errorf("rerun = %v, want new_failures", c["rerun"])
	}
	// Reason codes stay as codes in the JSON; the readable phrasing is for
	// the terminal and the HTML only.
	reasons := c["reasons"].([]any)
	if len(reasons) != 1 || reasons[0] != verify.ReasonStale {
		t.Errorf("reasons = %v, want the raw code", reasons)
	}
	ev := c["evidence"].([]any)
	if len(ev) != 1 {
		t.Fatalf("evidence = %d, want 1", len(ev))
	}
	e := ev[0].(map[string]any)
	for _, key := range []string{"type", "seq", "text", "output_excerpt", "parsed", "reproduce"} {
		if _, ok := e[key]; !ok {
			t.Errorf("evidence missing %q: %+v", key, e)
		}
	}
}

// Nothing that looks like a credential may reach --out. This is the security
// policy's second pass, and it applies to the JSON as much as the terminal.
func TestJSONScrubsCredentialsFromEveryField(t *testing.T) {
	t.Parallel()
	// Assembled from parts so this file carries no scannable token.
	secret := "gh" + "p_" + strings.Repeat("A", 36)
	rw := row("c1", verify.Impeached, 1, []string{verify.ReasonStale}, record.RerunPass,
		"I exported "+secret+" to run the tests.")
	rw.Verdict.Summary = "the log contained " + secret
	rw.Verdict.Evidence = []verify.Evidence{{
		Type: verify.EvidenceCommand, Seq: 1,
		Text:    "curl -H 'Authorization: Bearer " + secret + "'",
		Excerpt: "using " + secret,
		Command: "echo " + secret,
	}}
	r := New(testCheckpoint(), testInputs(), []Row{rw}, nil, nil, nil, nil)

	blob, err := ToJSON(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), secret) {
		t.Errorf("the JSON report leaked a credential:\n%s", blob)
	}
	if !strings.Contains(string(blob), "[redacted]") {
		t.Errorf("expected a redaction marker:\n%s", blob)
	}
}

func TestJSONUnrequestedBlock(t *testing.T) {
	t.Parallel()
	un := &verify.UnrequestedResult{
		Items: []verify.Unrequested{{
			Symbol: "_legacy_shim", File: "app/service.py", Kind: record.Added,
			Dependents: 0, Severity: verify.SeverityNone, Tokens: []string{"legacy", "shim"},
		}},
		PromptTokens: []string{"add", "discount"},
	}
	r := New(testCheckpoint(), testInputs(), nil, un, nil, nil, nil)

	got := decodeJSON(t, r)
	list := got["unrequested"].([]any)
	if len(list) != 1 {
		t.Fatalf("unrequested = %d, want 1", len(list))
	}
	it := list[0].(map[string]any)
	if it["symbol"] != "_legacy_shim" || it["severity"] != "none" || it["kind"] != "added" {
		t.Errorf("unrequested item = %+v", it)
	}
	// The tokens searched are published so a reader can see why the match
	// failed rather than taking the flag on trust.
	if _, ok := it["tokens_searched"]; !ok {
		t.Errorf("item missing tokens_searched: %+v", it)
	}
	if _, ok := got["prompt_tokens_searched"]; !ok {
		t.Error("missing prompt_tokens_searched")
	}
	if got["counts"].(map[string]any)["unrequested"].(float64) != 1 {
		t.Errorf("counts.unrequested = %v, want 1", got["counts"])
	}
}

// A skipped detector must say so, rather than emitting an empty list that
// reads as "nothing found". Missing data is a state.
func TestJSONUnrequestedSkippedIsExplicit(t *testing.T) {
	t.Parallel()
	un := &verify.UnrequestedResult{
		Skipped: true,
		Reason:  "the transcript carries no prompts, so there is no record of what was asked for",
	}
	r := New(testCheckpoint(), testInputs(), nil, un, nil, nil, nil)
	got := decodeJSON(t, r)
	reason, ok := got["unrequested_skipped"].(string)
	if !ok || reason == "" {
		t.Fatalf("unrequested_skipped = %v, want the reason", got["unrequested_skipped"])
	}
	if len(got["unrequested"].([]any)) != 0 {
		t.Error("skipped detector should emit an empty list alongside the reason")
	}
}

// Empty collections must serialise as [] rather than null, so a consumer can
// iterate without a nil check.
func TestJSONEmptyCollectionsAreArraysNotNull(t *testing.T) {
	t.Parallel()
	r := New(testCheckpoint(), Inputs{Adapter: "claude-code"}, nil, nil, nil, nil, nil)
	blob, err := ToJSON(r)
	if err != nil {
		t.Fatal(err)
	}
	s := string(blob)
	for _, key := range []string{`"claims": []`, `"unrequested": []`, `"limitations": []`, `"commands_run": []`} {
		if !strings.Contains(s, key) {
			t.Errorf("expected %s in:\n%s", key, s)
		}
	}
	if strings.Contains(s, "null") {
		t.Errorf("JSON contains null, want empty arrays and objects:\n%s", s)
	}
}

// A claim or path containing angle brackets must stay legible, because this
// file is read by people as well as machines.
func TestJSONDoesNotHTMLEscape(t *testing.T) {
	t.Parallel()
	rw := row("c1", verify.Corroborated, 1, nil, record.RerunPass, "Handled <script> and a > b.")
	r := New(testCheckpoint(), testInputs(), []Row{rw}, nil, nil, nil, nil)
	blob, err := ToJSON(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), `\u003c`) {
		t.Errorf("JSON escaped angle brackets as \\u003c:\n%s", blob)
	}
	if !strings.Contains(string(blob), "<script>") {
		t.Errorf("expected the literal text to survive unescaped:\n%s", blob)
	}
}

func TestJSONCountsMatchRows(t *testing.T) {
	t.Parallel()
	r := New(testCheckpoint(), testInputs(), []Row{
		row("c1", verify.Impeached, 1, []string{verify.ReasonStale}, record.RerunPass, "a"),
		row("c2", verify.Corroborated, 2, nil, record.RerunPass, "b"),
		row("c3", verify.Uncorroborated, 3, nil, record.RerunNotRun, "c"),
		row("c4", verify.Unverifiable, 4, nil, record.RerunNotRun, "d"),
	}, nil, nil, nil, nil)

	counts := decodeJSON(t, r)["counts"].(map[string]any)
	for key, want := range map[string]float64{
		"corroborated": 1, "impeached": 1, "uncorroborated": 1, "unverifiable": 1,
	} {
		if counts[key] != want {
			t.Errorf("counts.%s = %v, want %v", key, counts[key], want)
		}
	}
}
