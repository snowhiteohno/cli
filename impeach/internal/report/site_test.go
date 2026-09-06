package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// These check the committed landing page, which is generated rather than
// hand-authored. They fail if someone edits site/ by hand and lets it drift
// from the report, which is exactly the failure gen-site exists to prevent.

func siteFile(t *testing.T, parts ...string) string {
	t.Helper()
	p := filepath.Join(append([]string{"..", "..", "site"}, parts...)...)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Skipf("site not generated: %v", err)
	}
	return string(b)
}

func leadSection(t *testing.T, page, where string) string {
	t.Helper()
	const open = `<section id="lead">`
	i := strings.Index(page, open)
	if i < 0 {
		t.Fatalf("%s has no lead section", where)
	}
	rest := page[i:]
	j := strings.Index(rest, "</section>")
	if j < 0 {
		t.Fatalf("%s lead section is not terminated", where)
	}
	return rest[:j+len("</section>")]
}

// The QA item from the frontend spec: the landing page's hero and the sample
// report's hero are the same bytes. If they can differ, the page becomes a
// second, drifting account of the same audit.
func TestLandingHeroMatchesTheSampleReportByteForByte(t *testing.T) {
	t.Parallel()
	index := siteFile(t, "index.html")
	sample := siteFile(t, "sample", "impeach.html")

	a := leadSection(t, index, "index.html")
	b := leadSection(t, sample, "sample/impeach.html")
	if a != b {
		t.Errorf("the landing hero has drifted from the report hero\nindex:\n%s\n\nreport:\n%s", a, b)
	}
	if len(a) < 100 {
		t.Errorf("the hero is suspiciously short (%d bytes); gen-site may have lifted nothing", len(a))
	}
}

// The page must carry no absolute path and no user name. A committed sample
// report would otherwise publish the author's home directory to anyone who
// opens it.
func TestSiteCarriesNoMachinePaths(t *testing.T) {
	t.Parallel()
	for _, name := range [][]string{{"index.html"}, {"style.css"}, {"sample", "impeach.html"}, {"sample", "impeach.json"}} {
		body := siteFile(t, name...)
		for _, bad := range []string{"/Users/", "/home/", "/root/"} {
			if strings.Contains(body, bad) {
				t.Errorf("%v contains an absolute path %q", name, bad)
			}
		}
	}
}

// No external requests: the page must be readable offline and must not report
// who read it.
func TestSiteMakesNoExternalRequests(t *testing.T) {
	t.Parallel()
	index := siteFile(t, "index.html")
	refs := regexp.MustCompile(`(?:src|href)="(https?://[^"]+)"`).FindAllStringSubmatch(index, -1)
	for _, m := range refs {
		// The repository link is the one permitted outbound reference, and it
		// is a link to follow rather than a resource the page loads.
		if !strings.HasPrefix(m[1], "https://github.com/") {
			t.Errorf("the page references an external resource: %s", m[1])
		}
	}
	// Checking for the word "analytics" would trip on the footer saying the
	// page carries none, so these look for actual inclusion instead.
	for _, bad := range []string{
		"fonts.googleapis", "cdn.jsdelivr", "unpkg.com",
		"gtag(", "googletagmanager", "plausible.io", "@import url(http",
	} {
		if strings.Contains(index, bad) {
			t.Errorf("the page pulls in %q", bad)
		}
	}
	// And no script element at all: the page needs none.
	if strings.Contains(index, "<script") {
		t.Error("the landing page has a script element; it should need none")
	}
}

// The stylesheet is the report's own, copied at build time, so the two
// surfaces share one set of tokens by construction rather than by discipline.
func TestSiteStylesheetIsTheReportStylesheet(t *testing.T) {
	t.Parallel()
	site := siteFile(t, "style.css")
	own, err := os.ReadFile(filepath.Join("report.css"))
	if err != nil {
		t.Fatalf("read report.css: %v", err)
	}
	if site != string(own) {
		t.Error("site/style.css has drifted from internal/report/report.css; re-run gen-site")
	}
}

func TestSiteSampleJSONIsValidAndIsAReport(t *testing.T) {
	t.Parallel()
	var doc struct {
		Version string `json:"impeach_version"`
		Counts  struct {
			Impeached int `json:"impeached"`
		} `json:"counts"`
		Channels map[string]any `json:"channels"`
	}
	if err := json.Unmarshal([]byte(siteFile(t, "sample", "impeach.json")), &doc); err != nil {
		t.Fatalf("sample json does not parse: %v", err)
	}
	if doc.Version != Version {
		t.Errorf("sample json version = %q, want %q; re-run gen-site", doc.Version, Version)
	}
	// The sample is chosen for the impeached row. A sample with nothing
	// impeached would make the hero read "No claim was impeached."
	if doc.Counts.Impeached < 1 {
		t.Errorf("the sample report has no impeachment, so the hero has nothing to show")
	}
	if len(doc.Channels) == 0 {
		t.Error("the sample predates the context ledger; re-run gen-site")
	}
}

// The reproduce instructions on the page have to include the one step that is
// easy to miss, or a reviewer follows them and gets an accurate but baffling
// failure.
func TestSiteReproduceMentionsTheCheckpointRefFetch(t *testing.T) {
	t.Parallel()
	index := siteFile(t, "index.html")
	if !strings.Contains(index, "refs/entire/*") {
		t.Error("the reproduce section does not tell the reader to fetch the checkpoint refs")
	}
	if !strings.Contains(index, "not optional") {
		t.Error("the fetch step should say plainly that it is required")
	}
}

// The four verdicts and the fifth row type all have to be explained, since
// the page is for someone who will not read the docs.
func TestSiteExplainsEveryVerdict(t *testing.T) {
	t.Parallel()
	index := siteFile(t, "index.html")
	for _, want := range []string{
		"Corroborated", "Impeached", "Uncorroborated", "Unverifiable", "unrequested",
		// The completeness rule is the whole basis of the unverifiable state.
		"can never corroborate",
	} {
		if !strings.Contains(index, want) {
			t.Errorf("the page does not explain %q", want)
		}
	}
}
