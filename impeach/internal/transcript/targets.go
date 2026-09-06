package transcript

import (
	"strings"
)

// runnerTokens are the program or module names that mean "this segment ran
// tests, a build, or a linter". A segment with none of them contributes no
// targets, which is what keeps `cd /some/path && pytest tests/x.py` from
// reporting /some/path as a test target.
var runnerTokens = map[string]bool{
	"pytest": true, "py.test": true, "tox": true, "nox": true, "unittest": true,
	"go": true, "gotestsum": true,
	"npm": true, "yarn": true, "pnpm": true, "jest": true, "vitest": true, "mocha": true,
	"cargo": true, "make": true, "just": true,
	"rspec": true, "minitest": true, "rake": true,
	"phpunit": true, "mvn": true, "gradle": true, "ctest": true,
	"golangci-lint": true, "ruff": true, "flake8": true, "eslint": true, "mypy": true, "pylint": true,
}

// flagsTakingValue are flags whose following argument is a value, not a target
// path. Selector flags are handled separately because their value IS the scope.
var flagsTakingValue = map[string]bool{
	"-c": true, "-C": true, "-p": true, "-n": true, "-j": true,
	"--rootdir": true, "--config": true, "--maxfail": true, "--tb": true,
	"-o": true, "--out": true, "--output": true, "-run": true, "--run": true,
}

// selectorFlags carry a test selector as their value. Their value narrows
// scope just as a path does, so it counts as a target.
var selectorFlags = map[string]bool{
	"-k": true, "-m": true, "--deselect": true, "--filter": true,
	"--testNamePattern": true, "-t": true,
}

// CommandTargets parses the paths and selectors a command was pointed at.
//
// An empty result means the command named nothing, which the scope classifier
// reads as a full run. A non-empty result means the command was narrowed, and
// a claim about all tests cannot rest on it.
//
// This function only ever reads a command line. Impeach never executes
// anything found in a transcript, so nothing here is quoted for a shell or
// passed onward as a command.
func CommandTargets(cmd string) []string {
	var out []string
	seen := map[string]bool{}
	for _, seg := range Segments(cmd) {
		for _, t := range segmentTargets(seg) {
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}

// Segments splits a command line on the shell operators that separate one
// command from the next. It is a reader, not a shell: the pieces are only ever
// inspected.
func Segments(cmd string) []string {
	var segs []string
	var cur strings.Builder
	var quote rune

	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			segs = append(segs, s)
		}
		cur.Reset()
	}

	runes := []rune(cmd)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			cur.WriteRune(c)
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
			cur.WriteRune(c)
		case '\\':
			// A line continuation joins, anything else is literal.
			if i+1 < len(runes) && runes[i+1] == '\n' {
				i++
				cur.WriteRune(' ')
				continue
			}
			cur.WriteRune(c)
		case ';', '|', '&', '\n':
			// Collapse a run of operators, so && and || split once.
			flush()
			for i+1 < len(runes) && isOperator(runes[i+1]) {
				i++
			}
		default:
			cur.WriteRune(c)
		}
	}
	flush()
	return segs
}

func isOperator(c rune) bool {
	return c == ';' || c == '|' || c == '&' || c == '\n'
}

// segmentTargets extracts targets from one command segment, but only when the
// segment actually invoked a runner.
func segmentTargets(seg string) []string {
	tokens := tokenize(seg)
	if len(tokens) == 0 {
		return nil
	}
	if !mentionsRunner(tokens) {
		return nil
	}

	var out []string
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]

		if selectorFlags[tok] {
			if i+1 < len(tokens) {
				next := strings.Trim(tokens[i+1], `"'`)
				// -m is pytest's marker expression but python's module flag,
				// so `python -m pytest` must not read pytest as a selector.
				// Deciding by whether the value names a runner keeps both
				// readings correct without tracking the program.
				if isRunnerToken(next) {
					i++
					continue
				}
				if next != "" {
					out = append(out, next)
				}
				i++
			}
			continue
		}
		// --k=value form.
		if strings.HasPrefix(tok, "--") && strings.Contains(tok, "=") {
			name, val, _ := strings.Cut(tok, "=")
			if selectorFlags[name] && val != "" {
				out = append(out, strings.Trim(val, `"'`))
			}
			continue
		}
		if flagsTakingValue[tok] {
			i++ // skip its value
			continue
		}
		if strings.HasPrefix(tok, "-") {
			continue
		}
		// The runner itself, its module name and its subcommands are not
		// targets.
		if isRunnerToken(tok) || isSubcommand(tok) {
			continue
		}
		if looksLikeTarget(tok) {
			out = append(out, strings.Trim(tok, `"'`))
		}
	}
	return out
}

func mentionsRunner(tokens []string) bool {
	for _, t := range tokens {
		if isRunnerToken(t) {
			return true
		}
	}
	return false
}

// isRunnerToken matches a runner by its base name, so /path/to/.venv/bin/pytest
// and python -m pytest both count.
func isRunnerToken(tok string) bool {
	if tok == "" {
		return false
	}
	// python -m pytest: the interpreter alone is not a runner, the module is,
	// and the module appears as its own token.
	return runnerTokens[basename(tok)]
}

// isSubcommand skips words that select a runner's mode rather than a target,
// and interpreters that merely carry one.
//
// The comparison is on the base name so an interpreter invoked by path, such
// as ./.venv/bin/python, is recognised as the interpreter it is rather than
// read as a narrowing path.
func isSubcommand(tok string) bool {
	switch basename(tok) {
	case "test", "run", "check", "build", "lint", "vet", "ci", "install", "exec",
		"python", "python3", "node", "npx", "sh", "bash", "zsh", "env", "time", "cd":
		return true
	}
	return false
}

// basename strips any directory and a Windows executable suffix.
func basename(tok string) string {
	tok = strings.Trim(tok, `"'`)
	if i := strings.LastIndexByte(tok, '/'); i >= 0 {
		tok = tok[i+1:]
	}
	return strings.TrimSuffix(tok, ".exe")
}

// looksLikeTarget reports whether a bare word narrows a run.
func looksLikeTarget(tok string) bool {
	tok = strings.Trim(tok, `"'`)
	if tok == "" || tok == "." {
		return false
	}
	// A pytest node id.
	if strings.Contains(tok, "::") {
		return true
	}
	// A Go package pattern that is not the whole module.
	if strings.HasPrefix(tok, "./") {
		return tok != "./..."
	}
	if strings.Contains(tok, "/") {
		return true
	}
	for _, ext := range []string{".py", ".go", ".js", ".jsx", ".ts", ".tsx", ".rb", ".rs", ".java", ".php"} {
		if strings.HasSuffix(tok, ext) {
			return true
		}
	}
	// A bare test name, as jest and rspec accept.
	if strings.HasPrefix(tok, "test_") || strings.HasSuffix(tok, "_test") {
		return true
	}
	return false
}

// tokenize splits a segment on whitespace, keeping quoted runs together.
func tokenize(seg string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, c := range seg {
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				cur.WriteRune(c)
			}
		case c == '\'' || c == '"':
			quote = c
		case c == ' ' || c == '\t':
			flush()
		default:
			cur.WriteRune(c)
		}
	}
	flush()
	return out
}
