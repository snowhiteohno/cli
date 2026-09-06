package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/entireio/cli/impeach/internal/record"
	"github.com/entireio/cli/impeach/internal/verify"
)

// claimWidth is where the Claim column truncates. Truncation is for the
// terminal only; the HTML report never truncates a claim.
const claimWidth = 60

// reasonPhrases turn reason codes into readable text. The code stays in the
// JSON; the phrase is for people.
var reasonPhrases = map[string]string{
	verify.ReasonStale:              "stale",
	verify.ReasonScopeMismatch:      "scope mismatch",
	verify.ReasonContradictedOutput: "contradicted by output",
	verify.ReasonContradictedRerun:  "contradicted by rerun",
	verify.ReasonCallersExist:       "callers exist",
	verify.ReasonSignatureChanged:   "signature changed",
	verify.ReasonNotInDiff:          "not in diff",
	verify.ReasonNeverRead:          "never read",
}

// Table writes the terminal report.
func Table(w io.Writer, r *Report) error {
	if err := writeHeader(w, r); err != nil {
		return err
	}

	if len(r.Rows) == 0 {
		fmt.Fprintf(w, "\nNo claims matched the pattern library. Run with --model CMD to add an extractor,\n"+
			"or read the transcript with entire checkpoint explain %s --full.\n", r.Checkpoint.ID)
		// Unrequested detection reads the entity diff and the prompts, so it
		// works even when no claim was extracted.
		writeUnrequested(w, r)
		return writeFooter(w, r)
	}

	rows := make([][5]string, 0, len(r.Rows))
	for _, row := range r.Rows {
		rows = append(rows, [5]string{
			string(row.Verdict.Status),
			row.Claim.Family.String(),
			truncate(row.Claim.Text, claimWidth),
			phrases(row.Verdict.Reasons),
			rerunLabel(row.Verdict.Rerun),
		})
	}
	head := [5]string{"VERDICT", "FAMILY", "CLAIM", "REASON", "RERUN"}

	widths := [5]int{}
	for i := range head {
		widths[i] = len(head[i])
	}
	for _, row := range rows {
		for i := range row {
			if l := len(row[i]); l > widths[i] {
				widths[i] = l
			}
		}
	}

	fmt.Fprintln(w)
	writeRow(w, head, widths)
	for _, row := range rows {
		writeRow(w, row, widths)
	}

	// The evidence behind every impeachment, because a verdict without its
	// evidence is just another claim.
	for _, row := range r.Rows {
		if row.Verdict.Status != verify.Impeached {
			continue
		}
		fmt.Fprintf(w, "\n%s [%s] %q\n", strings.ToUpper(string(row.Verdict.Status)), row.Claim.ID, row.Claim.Text)
		if row.Verdict.Summary != "" {
			fmt.Fprintf(w, "  %s\n", row.Verdict.Summary)
		}
		for _, e := range row.Verdict.Evidence {
			writeEvidence(w, e)
		}
	}

	writeUnrequested(w, r)

	fmt.Fprintf(w, "\n%d corroborated   %d impeached   %d uncorroborated   %d unverifiable   %d unrequested\n",
		r.Counts.Corroborated, r.Counts.Impeached, r.Counts.Uncorroborated,
		r.Counts.Unverifiable, r.Counts.Unrequested)

	return writeFooter(w, r)
}

// writeUnrequested prints row type five and, importantly, what was searched
// for, so a reader can see why a match failed rather than taking the flag on
// trust.
func writeUnrequested(w io.Writer, r *Report) {
	un := r.Unrequested
	if un == nil {
		return
	}
	fmt.Fprintln(w, "\nUnrequested changes")
	if un.Skipped {
		fmt.Fprintf(w, "  Not checked: %s.\n", un.Reason)
		return
	}
	if len(un.Items) == 0 {
		fmt.Fprintln(w, "  Every added or signature-changed symbol was named in a prompt.")
		return
	}
	head := [5]string{"SYMBOL", "FILE", "KIND", "DEPENDENTS", "SEVERITY"}
	rows := make([][5]string, 0, len(un.Items))
	for _, it := range un.Items {
		sev := string(it.Severity)
		if it.IsTest {
			sev += " (test)"
		}
		rows = append(rows, [5]string{
			it.Symbol, it.File, string(it.Kind), fmt.Sprintf("%d", it.Dependents), sev,
		})
	}
	widths := [5]int{}
	for i := range head {
		widths[i] = len(head[i])
	}
	for _, row := range rows {
		for i := range row {
			if l := len(row[i]); l > widths[i] {
				widths[i] = l
			}
		}
	}
	writeRow(w, head, widths)
	for _, row := range rows {
		writeRow(w, row, widths)
	}
	if len(un.PromptTokens) > 0 {
		fmt.Fprintf(w, "  Prompt tokens searched: %s\n", joinCapped(un.PromptTokens, 30))
	}
}

// joinCapped joins tokens, capping the list so the terminal stays readable.
func joinCapped(items []string, max int) string {
	if len(items) <= max {
		return strings.Join(items, " ")
	}
	return strings.Join(items[:max], " ") + fmt.Sprintf(" ... and %d more", len(items)-max)
}

func writeHeader(w io.Writer, r *Report) error {
	agent := r.Checkpoint.Agent
	if agent == "" {
		agent = "unknown"
	}
	if _, err := fmt.Fprintf(w, "Impeach %s report for checkpoint %s (commit %s, parent %s), agent %s.\n",
		r.Version, r.Checkpoint.ID, short(r.Checkpoint.Commit), short(r.Checkpoint.Parent), agent); err != nil {
		return err
	}

	test := r.Inputs.TestCommand
	if test == "" {
		test = "none"
	}
	model := r.Inputs.ModelCommand
	if model == "" {
		model = "none"
	}
	fmt.Fprintf(w, "Adapter %s. Extractors: %s. Test command: %s. Rerun: %s. Model command: %s.\n",
		r.Inputs.Adapter, strings.Join(r.Inputs.Extractors, ", "), test, yesNo(r.Inputs.Rerun), model)

	if r.Checkpoint.IsMerge {
		fmt.Fprintln(w, "This is a merge commit; the diff is against the first parent.")
	}
	for _, n := range r.Notes {
		fmt.Fprintf(w, "%s\n", n)
	}
	return nil
}

func writeFooter(w io.Writer, r *Report) error {
	if len(r.Limitations) > 0 {
		fmt.Fprintln(w, "\nLimitations:")
		for _, l := range r.Limitations {
			fmt.Fprintf(w, "  %s\n", l)
		}
	}
	if len(r.CommandsRun) > 0 {
		fmt.Fprintln(w, "\nCommands run:")
		for _, c := range r.CommandsRun {
			fmt.Fprintf(w, "  %s\n", c)
		}
	}
	_, err := fmt.Fprintln(w, "\nNothing in this report was produced by a model unless the header names a model command.")
	return err
}

func writeRow(w io.Writer, cells [5]string, widths [5]int) {
	var b strings.Builder
	for i, c := range cells {
		b.WriteString(pad(c, widths[i]))
		if i < len(cells)-1 {
			b.WriteString("  ")
		}
	}
	fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
}

func writeEvidence(w io.Writer, e verify.Evidence) {
	label := string(e.Type)
	switch e.Type {
	case verify.EvidenceCommand:
		fmt.Fprintf(w, "  %-8s seq %d  %s", label, e.Seq, e.Text)
		if e.Detail != "" {
			fmt.Fprintf(w, "  ->  %s", e.Detail)
		}
		fmt.Fprintln(w)
		if e.Excerpt != "" {
			for _, line := range lastLines(e.Excerpt, 3) {
				fmt.Fprintf(w, "           | %s\n", line)
			}
		}
	case verify.EvidenceRerun:
		fmt.Fprintf(w, "  %-8s %s", label, e.Detail)
		if e.Text != "" {
			fmt.Fprintf(w, "  %s", e.Text)
		}
		fmt.Fprintln(w)
	default:
		fmt.Fprintf(w, "  %-8s", label)
		if e.Seq > 0 {
			fmt.Fprintf(w, " seq %d ", e.Seq)
		}
		fmt.Fprintf(w, " %s", e.Text)
		if e.Detail != "" {
			fmt.Fprintf(w, "  (%s)", e.Detail)
		}
		fmt.Fprintln(w)
	}
	if e.Command != "" {
		fmt.Fprintf(w, "           reproduce: %s\n", e.Command)
	}
}

// lastLines returns the final n non-empty lines, which is where a test
// runner's summary lives.
func lastLines(s string, n int) []string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, strings.TrimRight(l, " \t"))
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func phrases(reasons []string) string {
	if len(reasons) == 0 {
		return ""
	}
	out := make([]string, 0, len(reasons))
	for _, r := range reasons {
		if p, ok := reasonPhrases[r]; ok {
			out = append(out, p)
		} else {
			out = append(out, r)
		}
	}
	return strings.Join(out, ", ")
}

func rerunLabel(s record.RerunStatus) string {
	switch s {
	case record.RerunPass:
		return "pass"
	case record.RerunNewFailures:
		return "new failures"
	case record.RerunNotRun:
		return "not run"
	case record.RerunSkipped:
		return "skipped"
	default:
		return string(s)
	}
}

func truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func short(sha string) string {
	if sha == "" {
		return "none"
	}
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
