package transcript

import (
	"reflect"
	"testing"
)

func TestSegments(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cmd  string
		want []string
	}{
		{"single", "pytest -q", []string{"pytest -q"}},
		{"and", "cd app && pytest -q", []string{"cd app", "pytest -q"}},
		{"or", "pytest || echo failed", []string{"pytest", "echo failed"}},
		{"semicolon", "pytest; echo done", []string{"pytest", "echo done"}},
		{"pipe", "pytest | tail -5", []string{"pytest", "tail -5"}},
		{"newline", "pytest\necho done", []string{"pytest", "echo done"}},
		{"operators do not split inside quotes", `pytest -k "a and b"`, []string{`pytest -k "a and b"`}},
		{"empty", "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := Segments(c.cmd); !reflect.DeepEqual(got, c.want) {
				t.Errorf("Segments(%q) = %q, want %q", c.cmd, got, c.want)
			}
		})
	}
}

func TestCommandTargets(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cmd  string
		want []string
	}{
		// A full run names nothing, which is what lets a claim about all
		// tests rest on it.
		{"bare pytest", "pytest", nil},
		{"pytest quiet", "pytest -q", nil},
		{"go test all", "go test ./...", nil},
		{"npm test", "npm test", nil},
		{"cargo test", "cargo test", nil},
		{"make test", "make test", nil},

		// A narrowed run names its scope.
		{"one test file", "pytest tests/test_api.py", []string{"tests/test_api.py"}},
		{"node id", "pytest tests/test_api.py::test_quote_empty", []string{"tests/test_api.py::test_quote_empty"}},
		{"two files", "pytest tests/test_api.py tests/test_refunds.py", []string{"tests/test_api.py", "tests/test_refunds.py"}},
		{"keyword selector", `pytest -k "refund"`, []string{"refund"}},
		{"marker selector", "pytest -m slow", []string{"slow"}},
		{"go package", "go test ./internal/record", []string{"./internal/record"}},
		{"go run filter", "go test -run TestResolve ./...", nil},

		// The probe's real command: a cd, an interpreter by path, the module
		// flag, and one test file. Only the test file is a target.
		{
			"probe command",
			"cd /Users/x/app && ./.venv/bin/python -m pytest tests/test_api.py",
			[]string{"tests/test_api.py"},
		},
		{
			"interpreter with no narrowing",
			"cd /Users/x/app && ./.venv/bin/python -m pytest -q",
			nil,
		},

		// A segment that is not a runner contributes nothing, so a cd path is
		// never mistaken for a test target.
		{"cd only", "cd /Users/x/app", nil},
		{"git is not a runner", "git status --porcelain", nil},
		{"echo is not a runner", "echo tests/test_api.py", nil},

		// Flags whose values are not selectors must not become targets.
		{"rootdir value ignored", "pytest --rootdir /Users/x/app -q", nil},
		{"maxfail value ignored", "pytest --maxfail 1", nil},
		{"parallel count ignored", "pytest -n 4", nil},

		{"equals form selector", `pytest --filter=refund`, []string{"refund"}},
		{"empty command", "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := CommandTargets(c.cmd)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("CommandTargets(%q) = %q, want %q", c.cmd, got, c.want)
			}
		})
	}
}

func TestCommandTargetsDeduplicates(t *testing.T) {
	t.Parallel()
	got := CommandTargets("pytest tests/test_api.py && pytest tests/test_api.py")
	want := []string{"tests/test_api.py"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CommandTargets() = %q, want %q", got, want)
	}
}

func TestTokenize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want []string
	}{
		{"pytest -q", []string{"pytest", "-q"}},
		{`pytest -k "a and b"`, []string{"pytest", "-k", "a and b"}},
		{`pytest -k 'single quoted'`, []string{"pytest", "-k", "single quoted"}},
		{"  spaced   out  ", []string{"spaced", "out"}},
		{"", nil},
	}
	for _, c := range cases {
		if got := tokenize(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("tokenize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLooksLikeTarget(t *testing.T) {
	t.Parallel()
	yes := []string{"tests/test_api.py", "a::b", "./internal/record", "test_thing", "thing_test", "x.go", "y.ts"}
	no := []string{"", ".", "./...", "-q", "quiet"}
	for _, s := range yes {
		if !looksLikeTarget(s) {
			t.Errorf("looksLikeTarget(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if looksLikeTarget(s) {
			t.Errorf("looksLikeTarget(%q) = true, want false", s)
		}
	}
}
