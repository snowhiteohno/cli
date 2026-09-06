package record

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/entireio/cli/impeach/internal/runner"
)

// ChangeKind is how an entity changed between the parent and the commit.
type ChangeKind string

// The kinds `entire graph commit --json` emits, as observed by the Step 0
// probe. Body-only changes are never candidates for the unrequested detector,
// which is why the distinction matters.
const (
	Added            ChangeKind = "added"
	Removed          ChangeKind = "removed"
	Renamed          ChangeKind = "renamed"
	SignatureChanged ChangeKind = "signature_changed"
	BodyChanged      ChangeKind = "body_changed"
)

// EntityChange is one entity-level change.
type EntityChange struct {
	Path       string     `json:"path"`
	Language   string     `json:"language"`
	Kind       ChangeKind `json:"type"`
	SymbolKind string     `json:"kind"`
	Name       string     `json:"name"`
	OldSig     string     `json:"old_signature,omitempty"`
	NewSig     string     `json:"new_signature,omitempty"`
	Line       int        `json:"after_start_line,omitempty"`
	Dependents int        `json:"dependents_count"`
}

// IsTestFile reports whether the change lives in a test file. The unrequested
// detector tags these rather than dropping them.
func (c EntityChange) IsTestFile() bool {
	p := strings.ToLower(c.Path)
	base := p
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	return strings.HasPrefix(base, "test_") ||
		strings.HasSuffix(base, "_test.py") ||
		strings.HasSuffix(base, "_test.go") ||
		strings.Contains(p, "/tests/") ||
		strings.HasPrefix(p, "tests/") ||
		strings.Contains(p, "/test/") ||
		strings.HasPrefix(p, "test/") ||
		strings.HasSuffix(base, ".test.ts") ||
		strings.HasSuffix(base, ".test.js") ||
		strings.HasSuffix(base, "_spec.rb")
}

// CommitChanges is the parsed result of `entire graph commit --json`.
type CommitChanges struct {
	Base    string
	Head    string
	Changes []EntityChange
	// Files is every path Graph reported on, whether or not any entity in it
	// changed. A file Graph did not parse cannot support a structural verdict,
	// so the set of parsed files is itself evidence.
	Files     []string
	Languages map[string]bool
	// Warnings carries Graph's own warnings, such as an exceeded analysis
	// budget, so a partial answer is never presented as a complete one.
	Warnings []string
}

// ChangedPaths returns the distinct paths that carried a change.
func (c *CommitChanges) ChangedPaths() []string {
	seen := map[string]bool{}
	var out []string
	for _, ch := range c.Changes {
		if !seen[ch.Path] {
			seen[ch.Path] = true
			out = append(out, ch.Path)
		}
	}
	return out
}

// Parsed reports whether Graph parsed the given path.
func (c *CommitChanges) Parsed(path string) bool {
	for _, f := range c.Files {
		if f == path {
			return true
		}
	}
	return false
}

// FindSymbol returns the changes matching a symbol name.
func (c *CommitChanges) FindSymbol(name string) []EntityChange {
	var out []EntityChange
	for _, ch := range c.Changes {
		if strings.EqualFold(ch.Name, name) {
			out = append(out, ch)
		}
	}
	return out
}

// graphCommitJSON mirrors the documented JSON shape.
type graphCommitJSON struct {
	Base  string `json:"base"`
	Head  string `json:"head"`
	Files []struct {
		Path     string `json:"path"`
		Status   string `json:"status"`
		Language string `json:"language"`
		Changes  []struct {
			Type       string `json:"type"`
			Kind       string `json:"kind"`
			Name       string `json:"name"`
			OldSig     string `json:"old_signature"`
			NewSig     string `json:"new_signature"`
			AfterLine  int    `json:"after_start_line"`
			BeforeLine int    `json:"before_start_line"`
			Dependents int    `json:"dependents_count"`
		} `json:"changes"`
	} `json:"files"`
	Warnings []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Effect  string `json:"effect_on_semantic_completeness"`
	} `json:"warnings"`
}

// ParseCommitChanges parses `entire graph commit --json` output.
func ParseCommitChanges(blob []byte) (*CommitChanges, error) {
	var raw graphCommitJSON
	if err := json.Unmarshal(blob, &raw); err != nil {
		return nil, fmt.Errorf("parse graph commit json: %w", err)
	}

	out := &CommitChanges{
		Base:      raw.Base,
		Head:      raw.Head,
		Languages: map[string]bool{},
	}
	for _, f := range raw.Files {
		out.Files = append(out.Files, f.Path)
		if f.Language != "" {
			out.Languages[f.Language] = true
		}
		for _, c := range f.Changes {
			line := c.AfterLine
			if line == 0 {
				line = c.BeforeLine
			}
			out.Changes = append(out.Changes, EntityChange{
				Path:       f.Path,
				Language:   f.Language,
				Kind:       ChangeKind(c.Type),
				SymbolKind: c.Kind,
				Name:       c.Name,
				OldSig:     c.OldSig,
				NewSig:     c.NewSig,
				Line:       line,
				Dependents: c.Dependents,
			})
		}
	}
	for _, w := range raw.Warnings {
		msg := w.Message
		if msg == "" {
			msg = w.Effect
		}
		if w.Code != "" {
			msg = w.Code + ": " + msg
		}
		if msg != "" {
			out.Warnings = append(out.Warnings, msg)
		}
	}
	return out, nil
}

// Caller is one place that calls a symbol.
type Caller struct {
	Name  string
	Path  string
	Line  int
	Depth int
}

// Impact is the parsed result of `entire graph impact --format json`.
type Impact struct {
	Symbol string
	Path   string
	Line   int
	// Resolved is false when Graph could not identify the symbol at all, which
	// makes a safety claim about it unverifiable rather than false.
	Resolved bool
	// Ambiguous is true when the name matched several definitions and Graph
	// returned the definition list instead of an answer.
	Ambiguous bool
	Callers   []Caller
	// DirectCallers and TransitiveCallers are Graph's own counts.
	DirectCallers     int
	TransitiveCallers int
	TypeConsumers     int
	Warnings          []string
}

// CallerFiles returns the distinct files that contain callers. The reading
// verifier checks these against the transcript's read events.
func (i *Impact) CallerFiles() []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range i.Callers {
		if c.Path != "" && !seen[c.Path] {
			seen[c.Path] = true
			out = append(out, c.Path)
		}
	}
	return out
}

type graphImpactJSON struct {
	Query                string `json:"query"`
	FocusMatchesTotal    int    `json:"focus_matches_total"`
	DisambiguationNeeded bool   `json:"disambiguation_required"`
	Focus                *struct {
		Name      string `json:"name"`
		FilePath  string `json:"file_path"`
		StartLine int    `json:"start_line"`
	} `json:"focus"`
	Callers struct {
		Total      int `json:"total"`
		Direct     int `json:"direct"`
		Transitive int `json:"transitive"`
		Entries    []struct {
			Endpoint struct {
				Name     string `json:"name"`
				FilePath string `json:"file_path"`
			} `json:"endpoint"`
			Depth    int `json:"depth"`
			CallSite *struct {
				FilePath string `json:"file_path"`
				Line     int    `json:"line"`
			} `json:"call_site"`
		} `json:"entries"`
	} `json:"callers"`
	TypeConsumers struct {
		Total int `json:"total"`
	} `json:"type_consumers"`
	Warnings []struct {
		Code   string `json:"code"`
		Effect string `json:"effect_on_semantic_completeness"`
	} `json:"warnings"`
}

// ParseImpact parses `entire graph impact --format json` output.
func ParseImpact(blob []byte) (*Impact, error) {
	var raw graphImpactJSON
	if err := json.Unmarshal(blob, &raw); err != nil {
		return nil, fmt.Errorf("parse graph impact json: %w", err)
	}

	out := &Impact{
		Symbol:            raw.Query,
		Ambiguous:         raw.DisambiguationNeeded,
		DirectCallers:     raw.Callers.Direct,
		TransitiveCallers: raw.Callers.Transitive,
		TypeConsumers:     raw.TypeConsumers.Total,
	}
	if raw.Focus != nil && raw.Focus.Name != "" {
		out.Resolved = true
		out.Symbol = raw.Focus.Name
		out.Path = raw.Focus.FilePath
		out.Line = raw.Focus.StartLine
	}
	for _, e := range raw.Callers.Entries {
		c := Caller{
			Name:  e.Endpoint.Name,
			Path:  e.Endpoint.FilePath,
			Depth: e.Depth,
		}
		if e.CallSite != nil {
			c.Line = e.CallSite.Line
			if c.Path == "" {
				c.Path = e.CallSite.FilePath
			}
		}
		out.Callers = append(out.Callers, c)
	}
	for _, w := range raw.Warnings {
		if w.Code != "" {
			out.Warnings = append(out.Warnings, w.Code+": "+w.Effect)
		}
	}
	return out, nil
}

// Graph runs the Graph commands through the Runner.
type Graph struct {
	Runner runner.Runner
}

// Commit reads the entity-level change list for a commit.
func (g *Graph) Commit(ctx context.Context, repo, rev string) (*CommitChanges, error) {
	ctx, cancel := context.WithTimeout(ctx, runner.TimeoutGraph)
	defer cancel()

	args := []string{"graph", "commit", rev, "--repo", repo, "--json"}
	stdout, stderr, exit, err := g.Runner.Run(ctx, "entire", args, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", runner.Format("entire", args), err)
	}
	if exit != 0 {
		return nil, fmt.Errorf("%s exited %d: %s", runner.Format("entire", args), exit, firstLine(stderr))
	}
	return ParseCommitChanges(stdout)
}

// Impact reads the blast radius for one symbol.
//
// excludeTests drops test-only entries, which is what a "no other callers"
// claim usually means: a caller that is a test is not a caller that breaks.
// Both readings are available so the verifier can report each.
func (g *Graph) Impact(ctx context.Context, repo, symbol string, excludeTests bool) (*Impact, error) {
	ctx, cancel := context.WithTimeout(ctx, runner.TimeoutGraph)
	defer cancel()

	// The symbol goes through as a single argument. It never reaches a shell.
	args := []string{"graph", "impact", "--repo", repo, "--symbol", symbol, "--format", "json"}
	if excludeTests {
		args = append(args, "--exclude-tests")
	}
	stdout, stderr, exit, err := g.Runner.Run(ctx, "entire", args, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", runner.Format("entire", args), err)
	}
	if exit != 0 {
		return nil, fmt.Errorf("%s exited %d: %s", runner.Format("entire", args), exit, firstLine(stderr))
	}
	return ParseImpact(stdout)
}
