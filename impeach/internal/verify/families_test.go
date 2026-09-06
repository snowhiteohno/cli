package verify

import (
	"strings"
	"testing"

	"github.com/entireio/cli/impeach/internal/claims"
	"github.com/entireio/cli/impeach/internal/record"
	"github.com/entireio/cli/impeach/internal/transcript"
)

func changes(list ...record.EntityChange) *record.CommitChanges {
	c := &record.CommitChanges{Languages: map[string]bool{"Python": true}}
	seen := map[string]bool{}
	for _, ch := range list {
		c.Changes = append(c.Changes, ch)
		if !seen[ch.Path] {
			seen[ch.Path] = true
			c.Files = append(c.Files, ch.Path)
		}
	}
	return c
}

func claimOf(t *testing.T, sentence string, want claims.Family) claims.Claim {
	t.Helper()
	got, err := claims.Pattern{}.Extract([]transcript.Event{
		{Seq: 1, Kind: transcript.AssistantText, Turn: 1, Text: sentence},
	})
	if err != nil {
		t.Fatalf("Extract(%q) error = %v", sentence, err)
	}
	if len(got) != 1 {
		t.Fatalf("Extract(%q) gave %d claims, want 1: %+v", sentence, len(got), got)
	}
	if got[0].Family != want {
		t.Fatalf("Extract(%q) family = %v, want %v", sentence, got[0].Family, want)
	}
	return got[0]
}

// ---------- structural ----------

func TestStructuralCorroborated(t *testing.T) {
	t.Parallel()
	r := newBuilder().record()
	r.Changes = changes(record.EntityChange{
		Path: "tests/test_refunds.py", Kind: record.Added, SymbolKind: "function",
		Name: "test_refund_rounding", Line: 12,
	})
	c := claimOf(t, "Added `test_refund_rounding`.", claims.Structural)
	v := Structural{}.Verify(c, r)
	if v.Status != Corroborated {
		t.Fatalf("Status = %q, want %q (summary %q)", v.Status, Corroborated, v.Summary)
	}
	if !hasEvidence(v, EvidenceEntity, "test_refund_rounding") {
		t.Errorf("evidence missing the entity: %+v", v.Evidence)
	}
}

func TestStructuralImpeachedNotInDiff(t *testing.T) {
	t.Parallel()
	r := newBuilder().record()
	r.Changes = changes(record.EntityChange{
		Path: "app/service.py", Kind: record.BodyChanged, SymbolKind: "function", Name: "round_money",
	})
	c := claimOf(t, "Added `parse_refund`.", claims.Structural)
	v := Structural{}.Verify(c, r)
	if v.Status != Impeached {
		t.Fatalf("Status = %q, want %q", v.Status, Impeached)
	}
	if !hasReason(v, ReasonNotInDiff) {
		t.Errorf("Reasons = %v, want %q", v.Reasons, ReasonNotInDiff)
	}
}

// A name present in the diff but with a different change kind is a
// contradiction, not an absence.
func TestStructuralImpeachedWrongKind(t *testing.T) {
	t.Parallel()
	r := newBuilder().record()
	r.Changes = changes(record.EntityChange{
		Path: "app/service.py", Kind: record.BodyChanged, SymbolKind: "function", Name: "compute_total",
	})
	c := claimOf(t, "Added `compute_total`.", claims.Structural)
	v := Structural{}.Verify(c, r)
	if v.Status != Impeached {
		t.Fatalf("Status = %q, want %q", v.Status, Impeached)
	}
	if !strings.Contains(v.Summary, "body_changed") {
		t.Errorf("Summary = %q, want it to name what Graph actually saw", v.Summary)
	}
}

// Missing data is a state. No entity diff means unverifiable, not wrong.
func TestStructuralUnverifiableWithoutEntityDiff(t *testing.T) {
	t.Parallel()
	r := newBuilder().record()
	r.Changes = nil
	c := claimOf(t, "Added `parse_refund`.", claims.Structural)
	v := Structural{}.Verify(c, r)
	if v.Status != Unverifiable {
		t.Errorf("Status = %q, want %q", v.Status, Unverifiable)
	}
}

// A structural sentence naming no identifier is not extracted at all, so it
// can never be reported as unresolvable.
func TestStructuralSentenceWithoutIdentifierIsNotAClaim(t *testing.T) {
	t.Parallel()
	got, err := claims.Pattern{}.Extract([]transcript.Event{
		{Seq: 1, Kind: transcript.AssistantText, Turn: 1, Text: "I added the legacy shim."},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range got {
		if c.Family == claims.Structural {
			t.Errorf("extracted a structural claim with no identifier: %q", c.Text)
		}
	}
}

// ---------- safety ----------

func safetyRecord(t *testing.T, sig bool, callers int) *Record {
	t.Helper()
	r := newBuilder().record()
	kind := record.BodyChanged
	ch := record.EntityChange{
		Path: "app/service.py", SymbolKind: "function", Name: "compute_total", Line: 30,
	}
	if sig {
		kind = record.SignatureChanged
		ch.OldSig = "def compute_total(items, tax_rate=TAX_RATE)"
		ch.NewSig = "def compute_total(items, tax_rate=TAX_RATE, discount=0.0)"
	}
	ch.Kind = kind
	r.Changes = changes(ch)

	imp := &record.Impact{
		Symbol: "compute_total", Path: "app/service.py", Line: 30,
		Resolved: true, DirectCallers: callers,
	}
	for i := 0; i < callers; i++ {
		imp.Callers = append(imp.Callers, record.Caller{
			Name: []string{"checkout", "quote", "refund_amount"}[i%3],
			Path: "app/api.py", Line: 10 + i, Depth: 1,
		})
	}
	r.Impacts["compute_total"] = imp
	return r
}

// The design's safety scenario: "no other callers" with three real callers.
func TestSafetyContainmentImpeachedByCallers(t *testing.T) {
	t.Parallel()
	r := safetyRecord(t, true, 3)
	c := claimOf(t, "There are no other callers of `compute_total`.", claims.Safety)
	if c.SafetyKind != claims.SafetyContainment {
		t.Fatalf("SafetyKind = %v, want containment", c.SafetyKind)
	}
	v := Safety{}.Verify(c, r)
	if v.Status != Impeached {
		t.Fatalf("Status = %q, want %q (summary %q)", v.Status, Impeached, v.Summary)
	}
	if !hasReason(v, ReasonCallersExist) {
		t.Errorf("Reasons = %v, want %q", v.Reasons, ReasonCallersExist)
	}
	if !hasReason(v, ReasonSignatureChanged) {
		t.Errorf("Reasons = %v, want %q as well", v.Reasons, ReasonSignatureChanged)
	}
	if !strings.Contains(v.Summary, "3 callers") {
		t.Errorf("Summary = %q, want the caller count", v.Summary)
	}
}

func TestSafetyContainmentCorroboratedWithNoCallers(t *testing.T) {
	t.Parallel()
	r := safetyRecord(t, false, 0)
	c := claimOf(t, "Nothing else calls `compute_total`.", claims.Safety)
	v := Safety{}.Verify(c, r)
	if v.Status != Corroborated {
		t.Errorf("Status = %q, want %q (summary %q)", v.Status, Corroborated, v.Summary)
	}
}

// The false positive this build shipped and then fixed. A compatibility claim
// is about whether dependents still work. Callers alone do not contradict it:
// a symbol being called is the normal case, and impeaching "no behaviour
// change" for having callers is a false accusation.
func TestSafetyCompatibilityIsNotImpeachedByCallersAlone(t *testing.T) {
	t.Parallel()
	r := safetyRecord(t, false, 3)
	c := claimOf(t, "A plain rename of `compute_total` with no behaviour change.", claims.Safety)
	if c.SafetyKind != claims.SafetyCompatibility {
		t.Fatalf("SafetyKind = %v, want compatibility", c.SafetyKind)
	}
	v := Safety{}.Verify(c, r)
	if v.Status == Impeached {
		t.Fatalf("Status = impeached for a compatibility claim with callers but no signature change; that is a false accusation (reasons %v)", v.Reasons)
	}
	if v.Status != Corroborated {
		t.Errorf("Status = %q, want %q", v.Status, Corroborated)
	}
}

// A compatibility claim IS impeached when the signature changed under callers.
func TestSafetyCompatibilityImpeachedBySignatureChange(t *testing.T) {
	t.Parallel()
	r := safetyRecord(t, true, 3)
	c := claimOf(t, "`compute_total` stays backward compatible.", claims.Safety)
	v := Safety{}.Verify(c, r)
	if v.Status != Impeached {
		t.Fatalf("Status = %q, want %q (summary %q)", v.Status, Impeached, v.Summary)
	}
	if !hasReason(v, ReasonSignatureChanged) {
		t.Errorf("Reasons = %v, want %q", v.Reasons, ReasonSignatureChanged)
	}
	if !strings.Contains(v.Summary, "discount=0.0") {
		t.Errorf("Summary = %q, want the new signature quoted from Graph", v.Summary)
	}
}

// An ambiguous or unresolved symbol is unverifiable, never a verdict. Graph
// returns a definition list rather than an answer for an ambiguous name.
func TestSafetyUnverifiableWhenGraphCannotResolve(t *testing.T) {
	t.Parallel()
	for name, imp := range map[string]*record.Impact{
		"unresolved": {Symbol: "handle", Resolved: false},
		"ambiguous":  {Symbol: "handle", Resolved: true, Ambiguous: true},
	} {
		r := newBuilder().record()
		r.Changes = changes(record.EntityChange{Path: "app/service.py", Kind: record.SignatureChanged, Name: "handle"})
		r.Impacts["handle"] = imp
		c := claimOf(t, "Nothing else calls `handle`.", claims.Safety)
		v := Safety{}.Verify(c, r)
		if v.Status != Unverifiable {
			t.Errorf("%s: Status = %q, want %q", name, v.Status, Unverifiable)
		}
	}
}

// A symbol that was never looked up is unverifiable too, and the summary says
// so rather than implying the symbol is clean.
func TestSafetyUnverifiableWhenNeverLookedUp(t *testing.T) {
	t.Parallel()
	r := newBuilder().record()
	r.Changes = changes(record.EntityChange{Path: "app/service.py", Kind: record.Added, Name: "other"})
	c := claimOf(t, "Nothing else calls `never_queried`.", claims.Safety)
	v := Safety{}.Verify(c, r)
	if v.Status != Unverifiable {
		t.Errorf("Status = %q, want %q", v.Status, Unverifiable)
	}
}

// ---------- reading ----------

func TestReadingCorroboratedByNamedFile(t *testing.T) {
	t.Parallel()
	b := newBuilder()
	b.seq++
	b.events = append(b.events, transcript.Event{Seq: b.seq, Kind: transcript.FileRead, Path: "app/service.py"})
	r := b.record()
	c := claimOf(t, "I checked `app/service.py`.", claims.Reading)
	v := Reading{}.Verify(c, r)
	if v.Status != Corroborated {
		t.Fatalf("Status = %q, want %q (summary %q)", v.Status, Corroborated, v.Summary)
	}
	if !hasEvidence(v, EvidenceRead, "app/service.py") {
		t.Errorf("evidence missing the read: %+v", v.Evidence)
	}
}

func TestReadingImpeachedNeverRead(t *testing.T) {
	t.Parallel()
	b := newBuilder()
	b.seq++
	b.events = append(b.events, transcript.Event{Seq: b.seq, Kind: transcript.FileRead, Path: "README.md"})
	r := b.record()
	c := claimOf(t, "I reviewed `app/refunds.py`.", claims.Reading)
	v := Reading{}.Verify(c, r)
	if v.Status != Impeached {
		t.Fatalf("Status = %q, want %q", v.Status, Impeached)
	}
	if !hasReason(v, ReasonNeverRead) {
		t.Errorf("Reasons = %v, want %q", v.Reasons, ReasonNeverRead)
	}
}

// "I reviewed the callers" is checked against the caller files Graph found,
// which is why the record caches impact results.
func TestReadingCallersCorroboratedByCallerFile(t *testing.T) {
	t.Parallel()
	b := newBuilder()
	b.seq++
	b.events = append(b.events, transcript.Event{Seq: b.seq, Kind: transcript.FileRead, Path: "app/api.py"})
	r := b.record()
	r.Impacts["compute_total"] = &record.Impact{
		Symbol: "compute_total", Resolved: true,
		Callers: []record.Caller{{Name: "checkout", Path: "app/api.py", Line: 10}},
	}
	c := claimOf(t, "I reviewed the callers.", claims.Reading)
	v := Reading{}.Verify(c, r)
	if v.Status != Corroborated {
		t.Fatalf("Status = %q, want %q (summary %q)", v.Status, Corroborated, v.Summary)
	}
}

func TestReadingCallersImpeachedWhenNoneRead(t *testing.T) {
	t.Parallel()
	b := newBuilder()
	b.seq++
	b.events = append(b.events, transcript.Event{Seq: b.seq, Kind: transcript.FileRead, Path: "README.md"})
	r := b.record()
	r.Impacts["compute_total"] = &record.Impact{
		Symbol: "compute_total", Resolved: true,
		Callers: []record.Caller{{Name: "checkout", Path: "app/api.py", Line: 10}},
	}
	c := claimOf(t, "I reviewed the call sites.", claims.Reading)
	v := Reading{}.Verify(c, r)
	if v.Status != Impeached {
		t.Fatalf("Status = %q, want %q", v.Status, Impeached)
	}
	if !hasReason(v, ReasonNeverRead) {
		t.Errorf("Reasons = %v, want %q", v.Reasons, ReasonNeverRead)
	}
}

func TestReadingUnverifiableWithoutReadChannel(t *testing.T) {
	t.Parallel()
	r := newBuilder().record()
	c := claimOf(t, "I checked `app/service.py`.", claims.Reading)
	v := Reading{}.Verify(c, r)
	if v.Status != Unverifiable {
		t.Fatalf("Status = %q, want %q", v.Status, Unverifiable)
	}
	if !strings.Contains(v.Summary, "no read records") {
		t.Errorf("Summary = %q, want it to name the missing channel", v.Summary)
	}
}

// A reading claim naming nothing resolvable is uncorroborated, not impeached.
func TestReadingUncorroboratedWhenNothingNamed(t *testing.T) {
	t.Parallel()
	b := newBuilder()
	b.seq++
	b.events = append(b.events, transcript.Event{Seq: b.seq, Kind: transcript.FileRead, Path: "app/service.py"})
	r := b.record()
	c := claimOf(t, "I read all three.", claims.Reading)
	v := Reading{}.Verify(c, r)
	if v.Status != Uncorroborated {
		t.Errorf("Status = %q, want %q (summary %q)", v.Status, Uncorroborated, v.Summary)
	}
}

// ---------- headings are not testimony ----------

// The false positives this build shipped and then fixed. A wholly emphasized
// line is a section heading, and a clause ending in a colon introduces what
// follows. Neither asserts anything, and reading them as claims produced
// verdicts against text that made no claim.
func TestHeadingsAndLeadInsAreNotClaims(t *testing.T) {
	t.Parallel()
	for _, text := range []string{
		"**Other callers, and whether I reviewed them**",
		"**Tests, read this part**",
		"__What changed__",
		"## Tests",
		"Now the remaining call site of the renamed function, in `tests/test_service.py`:",
		"Two things I found and did not change:",
	} {
		got, err := claims.Pattern{}.Extract([]transcript.Event{
			{Seq: 1, Kind: transcript.AssistantText, Turn: 1, Text: text},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Errorf("%q was read as a claim: %+v", text, got)
		}
	}
}

// Prose that happens to be bold-emphasized inside a sentence is still a claim.
func TestPartialEmphasisIsStillAClaim(t *testing.T) {
	t.Parallel()
	got, err := claims.Pattern{}.Extract([]transcript.Event{
		{Seq: 1, Kind: transcript.AssistantText, Turn: 1,
			Text: "The **whole** suite passes."},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d claims, want 1: %+v", len(got), got)
	}
}

// ---------- unrequested ----------

func TestDetectUnrequestedFlagsUnmentionedSymbol(t *testing.T) {
	t.Parallel()
	ch := changes(
		record.EntityChange{Path: "app/service.py", Kind: record.Added, SymbolKind: "function",
			Name: "_legacy_shim", Dependents: 0},
		record.EntityChange{Path: "app/service.py", Kind: record.SignatureChanged, SymbolKind: "function",
			Name: "compute_total", Dependents: 8},
	)
	got := DetectUnrequested(ch, []string{"Please add a discount to compute_total."})
	if got.Skipped {
		t.Fatalf("Skipped = true, reason %q", got.Reason)
	}
	if len(got.Items) != 1 {
		t.Fatalf("Items = %+v, want only _legacy_shim", got.Items)
	}
	it := got.Items[0]
	if it.Symbol != "_legacy_shim" {
		t.Errorf("Symbol = %q", it.Symbol)
	}
	if it.Severity != SeverityNone {
		t.Errorf("Severity = %q, want %q for zero dependents", it.Severity, SeverityNone)
	}
	// The tokens searched are reported so a reader can see why it did not
	// match, rather than taking the flag on trust.
	if len(it.Tokens) == 0 {
		t.Error("Tokens is empty, want the identifier tokens that were searched")
	}
	if len(got.PromptTokens) == 0 {
		t.Error("PromptTokens is empty, want the mention corpus for display")
	}
}

// Body-only changes are never candidates. That is deliberate conservatism,
// disclosed as a limitation.
func TestDetectUnrequestedIgnoresBodyOnlyChanges(t *testing.T) {
	t.Parallel()
	ch := changes(record.EntityChange{
		Path: "app/service.py", Kind: record.BodyChanged, Name: "secretly_rewritten",
	})
	got := DetectUnrequested(ch, []string{"tidy up the service"})
	if len(got.Items) != 0 {
		t.Errorf("Items = %+v, want none for a body-only change", got.Items)
	}
}

func TestDetectUnrequestedMentionForms(t *testing.T) {
	t.Parallel()
	ch := changes(record.EntityChange{
		Path: "app/refunds.py", Kind: record.Added, Name: "parse_refund",
	})
	cases := map[string]bool{
		"add parse_refund please":        false, // exact name
		"add a refund parser":            true,  // neither name nor tokens
		"please handle parse and refund": false, // all tokens present
		"update refunds.py":              false, // file base name
		"update app/refunds.py":          false, // file base name in a path
		"make the totals work":           true,  // unrelated
	}
	for prompt, wantFlagged := range cases {
		got := DetectUnrequested(ch, []string{prompt})
		flagged := len(got.Items) > 0
		if flagged != wantFlagged {
			t.Errorf("prompt %q: flagged = %v, want %v", prompt, flagged, wantFlagged)
		}
	}
}

func TestDetectUnrequestedSeverityFromDependents(t *testing.T) {
	t.Parallel()
	for dependents, want := range map[int]Severity{0: SeverityNone, 3: SeverityLow, 12: SeverityHigh} {
		ch := changes(record.EntityChange{
			Path: "app/service.py", Kind: record.Added, Name: "zzz_unmentioned", Dependents: dependents,
		})
		got := DetectUnrequested(ch, []string{"unrelated prompt"})
		if len(got.Items) != 1 {
			t.Fatalf("dependents %d: Items = %+v", dependents, got.Items)
		}
		if got.Items[0].Severity != want {
			t.Errorf("dependents %d: Severity = %q, want %q", dependents, got.Items[0].Severity, want)
		}
	}
}

func TestDetectUnrequestedTagsTestSymbolsAndRanksThemLast(t *testing.T) {
	t.Parallel()
	ch := changes(
		record.EntityChange{Path: "tests/test_x.py", Kind: record.Added, Name: "test_zzz_unmentioned"},
		record.EntityChange{Path: "app/service.py", Kind: record.Added, Name: "yyy_unmentioned"},
	)
	got := DetectUnrequested(ch, []string{"unrelated"})
	if len(got.Items) != 2 {
		t.Fatalf("Items = %+v, want 2", got.Items)
	}
	// A production symbol nobody asked for matters more than a test the agent
	// added, so it is listed first.
	if got.Items[0].IsTest || !got.Items[1].IsTest {
		t.Errorf("ordering = %+v, want the non-test symbol first", got.Items)
	}
}

// Missing data is a state. No prompts means the detector cannot run, and it
// says so rather than flagging everything.
func TestDetectUnrequestedSkippedWithoutPrompts(t *testing.T) {
	t.Parallel()
	ch := changes(record.EntityChange{Path: "app/service.py", Kind: record.Added, Name: "whatever"})
	got := DetectUnrequested(ch, nil)
	if !got.Skipped {
		t.Fatal("Skipped = false, want true with no prompts")
	}
	if len(got.Items) != 0 {
		t.Errorf("Items = %+v, want none", got.Items)
	}
	if got.Reason == "" {
		t.Error("Reason is empty, want an explanation")
	}
}

func TestDetectUnrequestedSkippedWithoutEntityDiff(t *testing.T) {
	t.Parallel()
	got := DetectUnrequested(nil, []string{"a prompt"})
	if !got.Skipped {
		t.Fatal("Skipped = false, want true with no entity diff")
	}
}

func TestIdentifierTokens(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"_legacy_shim": {"legacy", "shim"},
		"computeTotal": {"compute", "total"},
		"parse_refund": {"parse", "refund"},
		"x":            nil,
		"ComputeTotal": {"compute", "total"},
	}
	for in, want := range cases {
		got := identifierTokens(in)
		if len(got) != len(want) {
			t.Errorf("identifierTokens(%q) = %v, want %v", in, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("identifierTokens(%q) = %v, want %v", in, got, want)
				break
			}
		}
	}
}
