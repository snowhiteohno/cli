// Command gen-site builds the landing page from a real report.
//
// The page is not hand-authored. It takes the output directory of an actual
// `entire impeach --out` run, scrubs machine-specific paths out of it,
// commits that as the sample report, copies the report's own stylesheet, and
// lifts the lead-impeachment block out of the sample verbatim.
//
// Lifting rather than rewriting is the point. The landing page's hero and the
// report's hero are the same bytes, so the two surfaces cannot drift into
// disagreeing about what the tool found. A test asserts that.
//
// Usage, from the impeach module root:
//
//	go run ./internal/report/gen-site --from <dir> --repo <repo-root>
package main

import (
	"flag"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	from := flag.String("from", "", "directory holding impeach.json and impeach.html from a real run")
	repo := flag.String("repo", "", "repository root, replaced with <repo> in the committed sample")
	out := flag.String("out", "site", "directory to write the site into")
	flag.Parse()

	if *from == "" {
		fail("--from is required: point it at the --out directory of a real impeach run")
	}
	if err := run(*from, *repo, *out); err != nil {
		fail("%v", err)
	}
}

func run(from, repo, out string) error {
	rawJSON, err := os.ReadFile(filepath.Join(from, "impeach.json"))
	if err != nil {
		return fmt.Errorf("read sample json: %w", err)
	}
	rawHTML, err := os.ReadFile(filepath.Join(from, "impeach.html"))
	if err != nil {
		return fmt.Errorf("read sample html: %w", err)
	}

	scrub := pathScrubber(repo)
	sampleJSON := scrub(string(rawJSON))
	sampleHTML := scrub(string(rawHTML))

	// The report's own stylesheet, copied rather than reimplemented, so the
	// two surfaces share one set of tokens by construction.
	css, err := os.ReadFile(filepath.Join("internal", "report", "report.css"))
	if err != nil {
		return fmt.Errorf("read report css: %w", err)
	}

	hero, err := extractSection(sampleHTML, "lead")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Join(out, "sample"), 0o755); err != nil {
		return err
	}
	writes := map[string]string{
		filepath.Join(out, "sample", "impeach.json"): sampleJSON,
		filepath.Join(out, "sample", "impeach.html"): sampleHTML,
		filepath.Join(out, "style.css"):              string(css),
	}
	for path, body := range writes {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}

	page, err := renderIndex(hero)
	if err != nil {
		return err
	}
	indexPath := filepath.Join(out, "index.html")
	if err := os.WriteFile(indexPath, page, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", indexPath, err)
	}

	fmt.Printf("wrote %s, %s/style.css and %s/sample/{impeach.json,impeach.html}\n", indexPath, out, out)
	return nil
}

// extractSection lifts one <section id="..."> ... </section> block verbatim.
//
// Verbatim matters: the hero on the landing page has to be the same bytes as
// the hero in the report, or the page becomes a second, drifting account of
// the same audit.
func extractSection(page, id string) (string, error) {
	open := fmt.Sprintf(`<section id=%q>`, id)
	start := strings.Index(page, open)
	if start < 0 {
		return "", fmt.Errorf("no <section id=%q> in the sample report; the template changed and gen-site needs updating", id)
	}
	rest := page[start:]
	end := strings.Index(rest, "</section>")
	if end < 0 {
		return "", fmt.Errorf("section %q is not terminated in the sample report", id)
	}
	return rest[:end+len("</section>")], nil
}

// pathScrubber rewrites machine-specific paths out of the committed sample.
//
// A locally generated report shows real paths on purpose, so a reader can
// copy a reproduce command and run it. A committed one must not: it would
// publish the author's home directory to anyone who opens the page.
func pathScrubber(repo string) func(string) string {
	var pairs [][2]string
	if repo != "" {
		if abs, err := filepath.Abs(repo); err == nil {
			pairs = append(pairs, [2]string{abs, "<repo>"})
		}
		pairs = append(pairs, [2]string{repo, "<repo>"})
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		pairs = append(pairs, [2]string{home, "<home>"})
	}
	if u := os.Getenv("USER"); len(u) > 2 {
		pairs = append(pairs, [2]string{u, "<user>"})
	}
	return func(s string) string {
		for _, p := range pairs {
			s = strings.ReplaceAll(s, p[0], p[1])
		}
		return s
	}
}

func renderIndex(hero string) ([]byte, error) {
	tmpl, err := template.New("index").Parse(indexTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse index template: %w", err)
	}
	var b strings.Builder
	// The hero is trusted because it came from our own renderer, which
	// already escaped every value it placed. Re-escaping would render the
	// markup as text.
	if err := tmpl.Execute(&b, map[string]any{"Hero": template.HTML(hero)}); err != nil {
		return nil, fmt.Errorf("render index: %w", err)
	}
	return []byte(b.String()), nil
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "gen-site: "+format+"\n", args...)
	os.Exit(1)
}
