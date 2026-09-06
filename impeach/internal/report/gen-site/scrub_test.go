package main

import (
	"strings"
	"testing"
)

// A scrubber that damages the output it is protecting is worse than no
// scrubber. The first version also replaced the raw --repo argument as given,
// so `--repo ..` rewrote every literal ".." in the report and turned pytest's
// "...." progress line into "<repo><repo>". That is real evidence, corrupted
// silently, in a file about to be committed and published.
func TestScrubberRefusesShortAndRelativeReplacements(t *testing.T) {
	t.Parallel()
	// The pytest progress line is the exact string that was destroyed.
	const evidence = "tests/test_api.py ....                    [100%]"

	for _, repo := range []string{"..", ".", "impeach", "../..", ""} {
		got := pathScrubber(repo)(evidence)
		if got != evidence {
			t.Errorf("--repo %q corrupted evidence:\n  in:  %q\n  out: %q", repo, evidence, got)
		}
	}
}

// An absolute path is still replaced, which is the whole point: a committed
// sample must not publish a home directory.
func TestScrubberReplacesAbsolutePaths(t *testing.T) {
	t.Parallel()
	in := "rootdir: /Users/someone/Desktop/Impeach/impeach/fixtures/app"
	got := pathScrubber("/Users/someone/Desktop/Impeach")(in)
	if strings.Contains(got, "/Users/someone/Desktop/Impeach") {
		t.Errorf("the absolute repository path survived: %q", got)
	}
	if !strings.Contains(got, "<repo>") {
		t.Errorf("expected the placeholder: %q", got)
	}
	// The part below the repository root has to survive, or the evidence
	// stops being readable.
	if !strings.Contains(got, "/impeach/fixtures/app") {
		t.Errorf("the path below the root was lost: %q", got)
	}
}

// Longest first, so a home directory nested inside a repository path does not
// leave a half-rewritten string behind.
func TestScrubberAppliesLongestPathFirst(t *testing.T) {
	t.Parallel()
	// A repo path under the home directory, which is the normal case.
	in := "/Users/someone/Desktop/Impeach/impeach/site"
	got := pathScrubber("/Users/someone/Desktop/Impeach")(in)
	if strings.Contains(got, "<home>") {
		t.Errorf("the shorter home path won and produced a mangled result: %q", got)
	}
	if !strings.HasPrefix(got, "<repo>") {
		t.Errorf("expected the repository placeholder to win: %q", got)
	}
}

// Ordinary report content must pass through untouched.
func TestScrubberLeavesReportContentAlone(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"18 passed in 0.02s",
		"NEWLY FAILING (3): tests/test_rounding.py::test_round_money_negative",
		"def compute_total(items, tax_rate=TAX_RATE, discount=0.0)",
		"cd impeach/fixtures/app && ./.venv/bin/python -m pytest -q",
		"commands present, reads present, edits present, prompts present, graph present",
	} {
		if got := pathScrubber("/Users/someone/Desktop/Impeach")(s); got != s {
			t.Errorf("content was altered:\n  in:  %q\n  out: %q", s, got)
		}
	}
}

// extractSection has to fail loudly if the report template changes, rather
// than silently producing a page with no hero.
func TestExtractSectionFailsWhenTheSectionIsGone(t *testing.T) {
	t.Parallel()
	if _, err := extractSection("<html><body>no sections here</body></html>", "lead"); err == nil {
		t.Fatal("error = nil, want a failure naming the missing section")
	}
	if _, err := extractSection(`<section id="lead">unterminated`, "lead"); err == nil {
		t.Fatal("error = nil, want a failure for an unterminated section")
	}
	got, err := extractSection(`before<section id="lead">hero</section>after`, "lead")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if got != `<section id="lead">hero</section>` {
		t.Errorf("extractSection = %q", got)
	}
}
