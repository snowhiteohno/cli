package report

import (
	"strings"
	"testing"
)

// Reports get pasted into pull requests and Entire's own transcript redaction
// is best effort, so anything that looks like a credential must not survive
// into an excerpt.
func TestScrubRedactsCredentialShapes(t *testing.T) {
	t.Parallel()
	// Every sample is assembled from parts at run time rather than written as
	// a literal.
	//
	// This is not fussiness. A complete, realistic token literal makes the
	// file itself look like a leak: GitHub push protection blocked this very
	// file over the Slack sample, which was a plausible-looking fake.
	// Splitting the prefix from the body keeps the pattern under test while
	// leaving no scannable token in the source.
	cases := map[string]string{
		"github classic":      "token is gh" + "p_" + strings.Repeat("A", 36) + " ok",
		"github fine grained": "github" + "_pat_" + strings.Repeat("B", 22) + "_x",
		"github oauth":        "gh" + "o_" + strings.Repeat("C", 36),
		"aws key id":          "AK" + "IA" + strings.Repeat("Q", 16),
		"aws secret":          "aws_secret_access_key=" + strings.Repeat("z", 20) + "/" + strings.Repeat("y", 19),
		"slack":               "xo" + "xb-" + strings.Repeat("1", 12) + "-" + strings.Repeat("a", 16),
		"openai":              "sk" + "-" + strings.Repeat("d", 32),
		"anthropic":           "sk" + "-ant-" + strings.Repeat("e", 32),
		"jwt":                 "ey" + "J" + strings.Repeat("h", 20) + "." + strings.Repeat("i", 20) + "." + strings.Repeat("j", 20),
		"google api key":      "AI" + "za" + strings.Repeat("f", 35),
		"generic api key":     "api_key = \"" + strings.Repeat("s", 16) + "\"",
		"generic password":    "password: " + strings.Repeat("p", 14),
		"bearer":              "Authorization: Bearer " + strings.Repeat("b", 16),
		"url credentials":     "https://user:" + strings.Repeat("w", 10) + "@example.com/repo.git",
		"private key":         "-----BEGIN RSA PRIVATE KEY-----\n" + strings.Repeat("M", 20) + "\n-----END RSA PRIVATE KEY-----",
	}
	for name, in := range cases {
		got := Scrub(in)
		if !looksRedacted(got) {
			t.Errorf("%s: Scrub(%q) = %q, nothing was redacted", name, in, got)
		}
	}
}

// The scrubber must not eat ordinary output. A test runner's summary, a path
// and a symbol name all have to survive, or the evidence becomes useless.
func TestScrubLeavesOrdinaryOutputAlone(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"18 passed in 0.02s",
		"tests/test_api.py::test_quote_empty PASSED",
		"impeach/fixtures/app/app/service.py",
		"def compute_total(items, tax_rate=TAX_RATE, discount=0.0)",
		"NEWLY FAILING (3): tests/test_rounding.py::test_round_money_negative",
		"entire graph impact --symbol compute_total --format json",
		"cd impeach/fixtures/app && ./.venv/bin/python -m pytest -q",
		"",
	} {
		if got := Scrub(in); got != in {
			t.Errorf("Scrub(%q) = %q, want it unchanged", in, got)
		}
	}
}

func TestScrubAll(t *testing.T) {
	t.Parallel()
	in := []string{"clean line", "ghp_" + strings.Repeat("A", 36)}
	got := ScrubAll(in)
	if got[0] != "clean line" {
		t.Errorf("clean line changed: %q", got[0])
	}
	if !looksRedacted(got[1]) {
		t.Errorf("secret survived ScrubAll: %q", got[1])
	}
	// The input must not be mutated, since evidence is shared across
	// renderers and one surface must not scrub another's data in place.
	if in[0] != "clean line" || looksRedacted(in[1]) {
		t.Error("ScrubAll mutated its input")
	}
	if ScrubAll(nil) != nil {
		t.Error("ScrubAll(nil) should stay nil")
	}
}
