package claims

import (
	"reflect"
	"strings"
	"testing"

	"github.com/entireio/cli/impeach/internal/transcript"
)

// Every pattern gets a positive and a negative sentence, so the library is
// pinned in both directions and a widened pattern shows up as a failure.
func TestExecutionPatternsPositiveAndNegative(t *testing.T) {
	t.Parallel()
	cases := []struct {
		pattern string
		yes     []string
		no      []string
	}{
		{
			pattern: "all-tests-pass",
			yes: []string{
				"All tests pass.",
				"all the tests are passing",
				"Every test passes now.",
				"The tests now pass.",
				"All integration tests pass.",
			},
			no: []string{
				"All tests fail.",
				"Not all tests pass.",
				"Do all the tests pass?",
				"You should make sure all tests pass.",
			},
		},
		{
			pattern: "suite-green",
			yes:     []string{"The test suite passes.", "The suite is green."},
			no:      []string{"The suite is broken.", "The suite does not pass."},
		},
		{
			pattern: "ran-tests",
			yes:     []string{"I ran the tests.", "Ran the unit tests.", "I re-ran tests."},
			no:      []string{"I did not run the tests.", "Please run the tests."},
		},
		{
			pattern: "n-tests-passed",
			yes:     []string{"4 passed", "18 tests passed", "4 passed in 0.00s"},
			no:      []string{"2 failed, 16 passed", "0 tests failed but 3 broke"},
		},
		{
			pattern: "build-succeeds",
			yes:     []string{"The build succeeds.", "The build is clean."},
			no:      []string{"The build failed.", "The build does not succeed."},
		},
		{
			pattern: "lint-clean",
			yes:     []string{"The linter passes.", "Lint is clean."},
			no:      []string{"The linter reports errors.", "Lint failed."},
		},
		{
			pattern: "no-failures",
			yes:     []string{"No test failures.", "Zero failing tests."},
			no:      []string{"There are failures."},
		},
	}

	for _, c := range cases {
		t.Run(c.pattern, func(t *testing.T) {
			t.Parallel()
			for _, s := range c.yes {
				if !extractsOne(s) {
					t.Errorf("%q should be extracted as an execution claim", s)
				}
			}
			for _, s := range c.no {
				if extractsOne(s) {
					t.Errorf("%q should NOT be extracted as a claim", s)
				}
			}
		})
	}
}

// extractsOne reports whether a single sentence yields a claim.
func extractsOne(sentence string) bool {
	got, err := Pattern{}.Extract([]transcript.Event{
		{Seq: 1, Kind: transcript.AssistantText, Turn: 1, Text: sentence},
	})
	return err == nil && len(got) > 0
}

// A hedge is still a claim. Confidence is not what is being audited: "I think
// the tests pass" asserts the same checkable fact.
func TestHedgedClaimsAreStillClaims(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"I think all tests pass.",
		"Probably all the tests pass.",
		"All tests pass, as far as I can tell.",
	} {
		if !extractsOne(s) {
			t.Errorf("%q is a hedged claim but still a claim", s)
		}
	}
}

// A prediction is not a report. "The tests should pass" says nothing about
// whether anything ran, so there is nothing to cross-examine. Missing it is
// silent; reading it as testimony would be a false verdict.
func TestPredictionsAreNotClaims(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"All tests should pass now.",
		"The build should succeed.",
		"The tests will pass once this lands.",
	} {
		if extractsOne(s) {
			t.Errorf("%q is a prediction, not testimony", s)
		}
	}
}

// A denial of failure is an affirmative claim and must not be dropped just
// for containing the word failing.
func TestNoFailureClaimsSurviveTheFailureWordFilter(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"Zero failing tests.",
		"No test failures.",
		"No lint errors.",
	} {
		if !extractsOne(s) {
			t.Errorf("%q denies failure and is a claim", s)
		}
	}
	for _, s := range []string{
		"2 tests failed.",
		"The build failed.",
		"Three tests are failing.",
	} {
		if extractsOne(s) {
			t.Errorf("%q reports failure and is not a pass claim", s)
		}
	}
}

// Instructions, questions, conditionals and denials are not testimony.
func TestNonClaimsAreRejected(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"Do the tests pass?",
		"You should run the tests before merging.",
		"If all tests pass, we can merge.",
		"I did not run the other test files.",
		"Worth running them before this goes anywhere real.",
		"Make sure the build succeeds.",
		"The tests do not pass yet.",
		"Once the tests pass, ship it.",
	} {
		if extractsOne(s) {
			t.Errorf("%q should not be read as a claim", s)
		}
	}
}

// The real sentence the probe session wrote. It is a genuine claim, and its
// scope is the one test file it names.
func TestProbeSessionSentenceIsAClaim(t *testing.T) {
	t.Parallel()
	// Close to what the probe session actually wrote. The verbatim sentence
	// lives in testdata; the dash is a hyphen here to keep this file plain.
	text := "**Tests** - `./.venv/bin/python -m pytest tests/test_api.py` from `impeach/fixtures/app`: 4 passed."
	got, err := Pattern{}.Extract([]transcript.Event{
		{Seq: 12, Kind: transcript.AssistantText, Turn: 3, Text: text},
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d claims, want 1: %+v", len(got), got)
	}
	c := got[0]
	// Markdown emphasis must not survive into the quote.
	if strings.Contains(c.Text, "**") {
		t.Errorf("claim text kept markdown emphasis: %q", c.Text)
	}
	if c.Family != Execution || c.Kind != KindTest {
		t.Errorf("Family/Kind = %v/%v", c.Family, c.Kind)
	}
	if c.Scope != ScopeSubset {
		t.Errorf("Scope = %v, want subset; the sentence names one test file", c.Scope)
	}
	// The directory named as context must not become a scope subject, or the
	// claim would be impeached for a scope it never asserted.
	scoped := ScopeSubjects(c.Subjects)
	if !reflect.DeepEqual(scoped, []string{"tests/test_api.py"}) {
		t.Errorf("ScopeSubjects = %v, want just the test file", scoped)
	}
	if c.Seq != 12 || c.Turn != 3 {
		t.Errorf("Seq/Turn = %d/%d, want 12/3", c.Seq, c.Turn)
	}
	if c.Extractor != "pattern" {
		t.Errorf("Extractor = %q", c.Extractor)
	}
}

func TestClassifyScope(t *testing.T) {
	t.Parallel()
	cases := []struct {
		sentence string
		want     Scope
	}{
		{"All tests pass.", ScopeAll},
		{"The entire suite passes.", ScopeAll},
		{"The whole test suite is green.", ScopeAll},
		{"Tests pass.", ScopeUnspecified},
		{"The tests now pass.", ScopeUnspecified},
		{"`tests/test_api.py` passes.", ScopeSubset},
		{"4 passed in tests/test_refunds.py", ScopeSubset},
		{"tests/test_api.py::test_quote_empty passes", ScopeSubset},
		// A directory is context, not a runner target, so it does not narrow
		// the claim.
		{"All tests pass in `app/fixtures`.", ScopeAll},
	}
	for _, c := range cases {
		if got := ClassifyScope(c.sentence); got != c.want {
			t.Errorf("ClassifyScope(%q) = %v, want %v", c.sentence, got, c.want)
		}
	}
}

func TestScopeSubjectsKeepsOnlyRunnerTargets(t *testing.T) {
	t.Parallel()
	in := []string{"tests/test_api.py", "app/fixtures", "compute_total", "tests/test_x.py::test_y", "service.go"}
	want := []string{"tests/test_api.py", "tests/test_x.py::test_y", "service.go"}
	if got := ScopeSubjects(in); !reflect.DeepEqual(got, want) {
		t.Errorf("ScopeSubjects(%v) = %v, want %v", in, got, want)
	}
}

func TestSentences(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"two sentences", "All tests pass. I also fixed the docs.", []string{"All tests pass.", "I also fixed the docs."}},
		{"newline splits", "All tests pass\nThe build is clean", []string{"All tests pass", "The build is clean"}},
		{"bullets are stripped", "- All tests pass\n- The build is clean",
			[]string{"All tests pass", "The build is clean"}},
		{"decimal is not a boundary", "It finished in 0.00s and passed.", []string{"It finished in 0.00s and passed."}},
		{"bold is stripped", "**Tests** pass.", []string{"Tests pass."}},
		{"empty", "", nil},
		{"whitespace only", "   \n  \n", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := Sentences(c.in); !reflect.DeepEqual(got, c.want) {
				t.Errorf("Sentences(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// One sentence yields at most one claim, even when several patterns agree.
// Several patterns matching is the library agreeing with itself, not the agent
// making several claims.
func TestOneClaimPerSentence(t *testing.T) {
	t.Parallel()
	got, err := Pattern{}.Extract([]transcript.Event{{
		Seq: 1, Kind: transcript.AssistantText, Turn: 1,
		Text: "All tests pass and the test suite is green.",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("got %d claims from one sentence, want 1: %+v", len(got), got)
	}
}

// Only assistant text is testimony. A prompt is what the user asked for.
func TestExtractIgnoresNonAssistantEvents(t *testing.T) {
	t.Parallel()
	got, err := Pattern{}.Extract([]transcript.Event{
		{Seq: 1, Kind: transcript.Prompt, Text: "Make sure all tests pass."},
		{Seq: 2, Kind: transcript.Command, Cmd: &transcript.CommandInfo{Cmd: "pytest", Output: "4 passed"}},
		{Seq: 3, Kind: transcript.FileEdit, Path: "a.py"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got %d claims, want 0: %+v", len(got), got)
	}
}

func TestClaimIDsAreStableAndUnique(t *testing.T) {
	t.Parallel()
	got, err := Pattern{}.Extract([]transcript.Event{
		{Seq: 1, Kind: transcript.AssistantText, Turn: 1, Text: "All tests pass."},
		{Seq: 2, Kind: transcript.AssistantText, Turn: 2, Text: "The build is clean."},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d claims, want 2", len(got))
	}
	if got[0].ID == got[1].ID {
		t.Errorf("claim ids collide: %q", got[0].ID)
	}
}

func TestKindSeparatesTestBuildAndLint(t *testing.T) {
	t.Parallel()
	cases := map[string]Kind{
		"All tests pass.":      KindTest,
		"The build succeeds.":  KindBuild,
		"The linter is clean.": KindLint,
	}
	for sentence, want := range cases {
		got, err := Pattern{}.Extract([]transcript.Event{
			{Seq: 1, Kind: transcript.AssistantText, Turn: 1, Text: sentence},
		})
		if err != nil || len(got) != 1 {
			t.Fatalf("%q: got %d claims, err %v", sentence, len(got), err)
		}
		if got[0].Kind != want {
			t.Errorf("%q Kind = %q, want %q", sentence, got[0].Kind, want)
		}
	}
}

func TestFamilyString(t *testing.T) {
	t.Parallel()
	names := map[Family]string{Execution: "execution", Structural: "structural", Safety: "safety", Reading: "reading"}
	for f, want := range names {
		if got := f.String(); got != want {
			t.Errorf("Family(%d).String() = %q, want %q", int(f), got, want)
		}
	}
}
