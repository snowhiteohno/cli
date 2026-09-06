package record

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entireio/cli/impeach/internal/runner"
)

// readTestdata loads a real Graph output captured by the Step 0 probe. The
// parsers are tested against what the installed plugin actually emitted, so a
// format change shows up as a test failure in one file.
func readTestdata(t *testing.T, parts ...string) []byte {
	t.Helper()
	p := filepath.Join(append([]string{"..", "..", "testdata"}, parts...)...)
	blob, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return blob
}

func TestParseCommitChangesBodyChanged(t *testing.T) {
	t.Parallel()
	got, err := ParseCommitChanges(readTestdata(t, "graph", "commit_body_changed.json"))
	if err != nil {
		t.Fatalf("ParseCommitChanges() error = %v", err)
	}
	if len(got.Changes) != 1 {
		t.Fatalf("Changes = %d, want 1", len(got.Changes))
	}
	c := got.Changes[0]
	if c.Kind != BodyChanged {
		t.Errorf("Kind = %q, want %q", c.Kind, BodyChanged)
	}
	if c.Name != "round_money" {
		t.Errorf("Name = %q", c.Name)
	}
	if c.SymbolKind != "function" {
		t.Errorf("SymbolKind = %q, want function", c.SymbolKind)
	}
	if c.Path != "impeach/fixtures/app/app/service.py" {
		t.Errorf("Path = %q", c.Path)
	}
	if c.Language != "Python" {
		t.Errorf("Language = %q", c.Language)
	}
	// The probe recorded eight dependents for round_money.
	if c.Dependents != 8 {
		t.Errorf("Dependents = %d, want 8", c.Dependents)
	}
	if !got.Parsed("impeach/fixtures/app/app/service.py") {
		t.Error("Parsed() = false for a file Graph reported on")
	}
	if got.Parsed("some/other/file.py") {
		t.Error("Parsed() = true for a file Graph never mentioned")
	}
}

// Distinguishing a signature change from a body change is what the safety
// verifier rests on, so it is asserted against the real output.
func TestParseCommitChangesSignatureChangedAndAdded(t *testing.T) {
	t.Parallel()
	got, err := ParseCommitChanges(readTestdata(t, "graph", "commit_signature_changed_and_added.json"))
	if err != nil {
		t.Fatalf("ParseCommitChanges() error = %v", err)
	}
	if len(got.Changes) != 2 {
		t.Fatalf("Changes = %d, want 2", len(got.Changes))
	}

	sig := got.FindSymbol("compute_total")
	if len(sig) != 1 {
		t.Fatalf("FindSymbol(compute_total) = %d changes, want 1", len(sig))
	}
	if sig[0].Kind != SignatureChanged {
		t.Errorf("Kind = %q, want %q", sig[0].Kind, SignatureChanged)
	}
	// Graph carries both signatures, so the report can quote the delta rather
	// than infer it.
	if !strings.Contains(sig[0].OldSig, "tax_rate=TAX_RATE)") {
		t.Errorf("OldSig = %q", sig[0].OldSig)
	}
	if !strings.Contains(sig[0].NewSig, "discount=0.0") {
		t.Errorf("NewSig = %q", sig[0].NewSig)
	}

	added := got.FindSymbol("_legacy_shim")
	if len(added) != 1 {
		t.Fatalf("FindSymbol(_legacy_shim) = %d changes, want 1", len(added))
	}
	if added[0].Kind != Added {
		t.Errorf("Kind = %q, want %q", added[0].Kind, Added)
	}
	// Zero dependents is what makes the unrequested severity low rather than
	// high.
	if added[0].Dependents != 0 {
		t.Errorf("Dependents = %d, want 0", added[0].Dependents)
	}
}

func TestFindSymbolIsCaseInsensitiveAndMissesCleanly(t *testing.T) {
	t.Parallel()
	got, err := ParseCommitChanges(readTestdata(t, "graph", "commit_body_changed.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.FindSymbol("ROUND_MONEY")) != 1 {
		t.Error("FindSymbol should match regardless of case")
	}
	if len(got.FindSymbol("no_such_symbol")) != 0 {
		t.Error("FindSymbol invented a change")
	}
}

func TestParseCommitChangesRejectsGarbage(t *testing.T) {
	t.Parallel()
	if _, err := ParseCommitChanges([]byte("not json")); err == nil {
		t.Error("ParseCommitChanges(garbage) error = nil, want an error")
	}
}

func TestIsTestFile(t *testing.T) {
	t.Parallel()
	yes := []string{
		"tests/test_api.py",
		"impeach/fixtures/app/tests/test_service.py",
		"internal/record/graph_test.go",
		"src/thing.test.ts",
		"spec/models/user_spec.rb",
		"test/helper.py",
	}
	no := []string{
		"app/service.py",
		"impeach/fixtures/app/app/api.py",
		"internal/record/graph.go",
		"src/latest.ts",
	}
	for _, p := range yes {
		if !(EntityChange{Path: p}).IsTestFile() {
			t.Errorf("IsTestFile(%q) = false, want true", p)
		}
	}
	for _, p := range no {
		if (EntityChange{Path: p}).IsTestFile() {
			t.Errorf("IsTestFile(%q) = true, want false", p)
		}
	}
}

// The three real callers of compute_total are the safety scenario. If this
// parser drops one, a "no other callers" claim would be corroborated when it
// should be impeached.
func TestParseImpactRealCallers(t *testing.T) {
	t.Parallel()
	got, err := ParseImpact(readTestdata(t, "graph", "impact_compute_total_excludetests.json"))
	if err != nil {
		t.Fatalf("ParseImpact() error = %v", err)
	}
	if !got.Resolved {
		t.Fatal("Resolved = false, want true")
	}
	if got.Ambiguous {
		t.Error("Ambiguous = true, want false")
	}
	if got.Symbol != "compute_total" {
		t.Errorf("Symbol = %q", got.Symbol)
	}
	if got.Path != "impeach/fixtures/app/app/service.py" {
		t.Errorf("Path = %q", got.Path)
	}
	if len(got.Callers) != 3 {
		t.Fatalf("Callers = %d, want 3", len(got.Callers))
	}
	if got.DirectCallers != 3 {
		t.Errorf("DirectCallers = %d, want 3", got.DirectCallers)
	}

	names := map[string]bool{}
	for _, c := range got.Callers {
		names[c.Name] = true
		if c.Path == "" {
			t.Errorf("caller %q has no file path", c.Name)
		}
		if c.Line == 0 {
			t.Errorf("caller %q has no call-site line", c.Name)
		}
	}
	for _, want := range []string{"checkout", "quote", "refund_amount"} {
		if !names[want] {
			t.Errorf("caller %q missing; got %v", want, names)
		}
	}

	files := got.CallerFiles()
	if len(files) != 2 {
		t.Errorf("CallerFiles() = %v, want the two files api.py and refunds.py", files)
	}
}

func TestParseImpactUnresolvedSymbol(t *testing.T) {
	t.Parallel()
	// Graph returns an envelope with no focus when it cannot identify the
	// symbol. That must read as unresolved, which the verifier turns into
	// unverifiable rather than into a false verdict.
	got, err := ParseImpact([]byte(`{"query":"nope","focus_matches_total":0,"callers":{"total":0}}`))
	if err != nil {
		t.Fatalf("ParseImpact() error = %v", err)
	}
	if got.Resolved {
		t.Error("Resolved = true, want false")
	}
	if len(got.Callers) != 0 {
		t.Errorf("Callers = %v, want none", got.Callers)
	}
}

func TestParseImpactAmbiguousSymbol(t *testing.T) {
	t.Parallel()
	got, err := ParseImpact([]byte(`{"query":"handle","focus_matches_total":3,"disambiguation_required":true,"callers":{"total":0}}`))
	if err != nil {
		t.Fatalf("ParseImpact() error = %v", err)
	}
	if !got.Ambiguous {
		t.Error("Ambiguous = false, want true")
	}
}

func TestGraphCommitThroughRunner(t *testing.T) {
	t.Parallel()
	blob := readTestdata(t, "graph", "commit_body_changed.json")
	f := runner.NewFake(runner.Call{
		Name:   "entire",
		Args:   []string{"graph", "commit", "458bb14", "--repo", "/wt/head", "--json"},
		Stdout: string(blob),
	})
	g := &Graph{Runner: f}

	got, err := g.Commit(context.Background(), "/wt/head", "458bb14")
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if len(got.Changes) != 1 {
		t.Errorf("Changes = %d, want 1", len(got.Changes))
	}
}

func TestGraphCommitSurfacesFailure(t *testing.T) {
	t.Parallel()
	f := runner.NewFake(runner.Call{
		Name:   "entire",
		Args:   []string{"graph", "commit", "bad", "--repo", "/wt/head", "--json"},
		Stderr: "fatal: unknown revision\n", Exit: 1,
	})
	g := &Graph{Runner: f}
	if _, err := g.Commit(context.Background(), "/wt/head", "bad"); err == nil {
		t.Fatal("Commit() error = nil, want the failure surfaced")
	}
}

// The symbol must reach Graph as a single argument and never touch a shell.
func TestGraphImpactPassesSymbolAsOneArgument(t *testing.T) {
	t.Parallel()
	hostile := `compute_total; rm -rf /`
	f := runner.NewFake(runner.Call{
		Name:   "entire",
		Args:   []string{"graph", "impact", "--repo", "/wt/head", "--symbol", hostile, "--format", "json", "--exclude-tests"},
		Stdout: `{"query":"x","callers":{"total":0}}`,
	})
	g := &Graph{Runner: f}
	if _, err := g.Impact(context.Background(), "/wt/head", hostile, true); err != nil {
		t.Fatalf("Impact() error = %v", err)
	}
	// The Fake matches on exact argv, so a match proves the symbol was passed
	// whole rather than split or interpolated.
	reqs := f.Requests()
	if len(reqs) != 1 || !strings.Contains(reqs[0], hostile) {
		t.Errorf("Requests() = %v, want the symbol passed as one argument", reqs)
	}
}
