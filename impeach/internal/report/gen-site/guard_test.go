package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The sample report has to carry a lead section, because that block is what
// gen-site lifts. Everything else about it is irrelevant to these tests.
const sampleReport = `<!doctype html>
<html><body>
<section id="lead">
  <h2>Lead impeachment</h2>
  <p>The tests passed.</p>
</section>
</body></html>
`

// setup puts the process in the module root, which is where gen-site expects
// to find internal/report/report.css, and returns a --from directory holding
// one run's output.
//
// No t.Parallel in this file: t.Chdir is process-global and Go's test
// framework refuses to combine the two.
func setup(t *testing.T) string {
	t.Helper()
	t.Chdir(filepath.Join("..", "..", ".."))

	from := t.TempDir()
	if err := os.WriteFile(filepath.Join(from, "impeach.json"), []byte(`{"verdicts":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(from, "impeach.html"), []byte(sampleReport), 0o644); err != nil {
		t.Fatal(err)
	}
	return from
}

// Running gen-site against the shipped site used to replace the hand-written
// page with the older generated one, and every test stayed green while it
// happened. This is the check that stops it.
func TestGenSiteRefusesToOverwriteAHandWrittenPage(t *testing.T) {
	from := setup(t)
	out := t.TempDir()

	const handWritten = "<!doctype html>\n<p>written by hand</p>\n"
	index := filepath.Join(out, "index.html")
	if err := os.WriteFile(index, []byte(handWritten), 0o644); err != nil {
		t.Fatal(err)
	}

	err := run(from, "", out, false)
	if err == nil {
		t.Fatal("run() overwrote a hand-written page instead of refusing")
	}
	if !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Errorf("run() error = %v, want a refusal naming the file", err)
	}

	// A refusal has to leave the directory alone, not half rewrite it.
	after, readErr := os.ReadFile(index)
	if readErr != nil {
		t.Fatalf("read index after refusal: %v", readErr)
	}
	if string(after) != handWritten {
		t.Errorf("index.html was modified by a run that refused:\n%s", after)
	}
	if _, statErr := os.Stat(filepath.Join(out, "sample")); statErr == nil {
		t.Error("sample/ was written by a run that refused; the refusal must cover the whole run")
	}
	if _, statErr := os.Stat(filepath.Join(out, "style.css")); statErr == nil {
		t.Error("style.css was written by a run that refused")
	}
}

// The stylesheet is protected for the same reason as the page. It is hand
// written too, and overwriting it with the report's copy strips the site's own
// type and layout while leaving the tokens agreeing, so no test would fail.
func TestGenSiteRefusesToOverwriteAHandWrittenStylesheet(t *testing.T) {
	from := setup(t)
	out := t.TempDir()

	css := filepath.Join(out, "style.css")
	if err := os.WriteFile(css, []byte(":root { --paper: #fff; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := run(from, "", out, false)
	if err == nil {
		t.Fatal("run() overwrote a hand-written stylesheet instead of refusing")
	}
	if !strings.Contains(err.Error(), "style.css") {
		t.Errorf("run() error = %v, want it to name style.css", err)
	}
}

// Generated output carries the marker, so gen-site can recognise its own work
// and regenerating into its own output directory keeps working. Without this
// the refusal above would make the command single-use.
func TestGenSiteRecognisesItsOwnOutputAndRegenerates(t *testing.T) {
	from := setup(t)
	out := t.TempDir()

	if err := run(from, "", out, false); err != nil {
		t.Fatalf("first run() error = %v", err)
	}
	for _, name := range []string{"index.html", "style.css"} {
		body, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !strings.Contains(string(body), genMarker) {
			t.Errorf("generated %s carries no marker, so gen-site cannot tell it from a hand-written file", name)
		}
	}
	if err := run(from, "", out, false); err != nil {
		t.Errorf("second run() into its own output refused: %v", err)
	}
}

// The escape hatch exists because the alternative is someone deleting the page
// by hand to get past the refusal, which loses the reason it was refused.
func TestGenSiteOverwritesHandWrittenOutputWhenTold(t *testing.T) {
	from := setup(t)
	out := t.TempDir()

	index := filepath.Join(out, "index.html")
	if err := os.WriteFile(index, []byte("<p>written by hand</p>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(from, "", out, true); err != nil {
		t.Fatalf("run() with force error = %v", err)
	}
	body, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), genMarker) {
		t.Error("forced run did not replace the hand-written page")
	}
}

// An unreadable output file is not evidence that overwriting is safe. The
// check reports the error rather than treating it as absence.
func TestGenSiteReportsAnUnreadableOutputRatherThanAssumingAbsence(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root, which can read a file with no permission bits")
	}
	from := setup(t)
	out := t.TempDir()

	index := filepath.Join(out, "index.html")
	if err := os.WriteFile(index, []byte("<p>hand written</p>\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	err := run(from, "", out, false)
	if err == nil {
		t.Fatal("run() proceeded past an output file it could not read")
	}
	if strings.Contains(err.Error(), "refusing to overwrite") {
		t.Errorf("run() error = %v, want the read failure rather than the refusal", err)
	}
}
