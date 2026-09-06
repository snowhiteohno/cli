// Command entire-impeach cross-examines what an AI coding agent claimed it did
// against the record of what it actually did.
//
// It is dispatched by the Entire CLI as `entire impeach` because any
// executable named entire-<name> on $PATH runs kubectl-style with stdio and
// the exit code passed through.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/entireio/cli/impeach/internal/checkpoint"
	"github.com/entireio/cli/impeach/internal/record"
	"github.com/entireio/cli/impeach/internal/runner"
)

// Version is the Impeach version, printed on every run so a reader knows which
// binary produced a report.
const Version = "0.1.0"

// Exit codes are part of the contract from the first commit, so the CI
// continuation path is real rather than promised.
const (
	exitOK     = 0 // the audit completed
	exitError  = 1 // a runtime failure: no Entire CLI, unresolvable checkpoint, worktree failure
	exitFailOn = 2 // the --fail-on condition was met
)

type options struct {
	ref           string
	test          string
	session       bool
	format        string
	out           string
	failOn        string
	model         string
	adapter       string
	noRerun       bool
	keepWorktrees bool
	repo          string
	version       bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(argv []string, stdout, stderr *os.File) int {
	opts, err := parseFlags(argv, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		fmt.Fprintf(stderr, "entire-impeach: %v\n", err)
		return exitError
	}
	if opts.version {
		fmt.Fprintf(stdout, "entire-impeach %s\n", Version)
		return exitOK
	}
	if opts.ref == "" {
		fmt.Fprintf(stderr, "entire-impeach: no checkpoint or commit given\n\nUsage: entire impeach <checkpoint-id | commit-ish> [flags]\n")
		return exitError
	}

	// Ctrl-C must remove the worktrees rather than leave them behind, so the
	// signal cancels the context the whole audit runs under.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := audit(ctx, opts, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "entire-impeach: %v\n", err)
		return exitError
	}
	return exitOK
}

func parseFlags(argv []string, stderr *os.File) (*options, error) {
	opts := &options{}
	fs := flag.NewFlagSet("entire-impeach", flag.ContinueOnError)
	fs.SetOutput(stderr)

	fs.StringVar(&opts.test, "test", "", `test command to rerun in the checkpoint worktree`)
	fs.BoolVar(&opts.session, "session", false, "audit every checkpoint in the session")
	fs.StringVar(&opts.format, "format", "table", "output format: table, json or html")
	fs.StringVar(&opts.out, "out", "", "directory to write the json and html reports to")
	fs.StringVar(&opts.failOn, "fail-on", "", "exit 2 when any row is impeached or uncorroborated")
	fs.StringVar(&opts.model, "model", "", "opt-in claim extractor: a command taking a prompt on stdin")
	fs.StringVar(&opts.adapter, "adapter", "auto", "transcript adapter: auto or claude-code")
	fs.BoolVar(&opts.noRerun, "no-rerun", false, "skip the test rerun through entire graph verify")
	fs.BoolVar(&opts.keepWorktrees, "keep-worktrees", false, "leave the temporary worktrees in place")
	fs.StringVar(&opts.repo, "repo", "", "repository to audit (default: the current repository)")
	fs.BoolVar(&opts.version, "version", false, "print the version and exit")

	fs.Usage = func() {
		fmt.Fprintf(stderr, `entire impeach cross-examines an agent's claims against the record.

Usage:
  entire impeach <checkpoint-id | commit-ish> [flags]

Flags:
`)
		fs.PrintDefaults()
		fmt.Fprintf(stderr, `
Exit codes: 0 completed, 2 the --fail-on condition was met, 1 runtime error.

Impeach never executes a command found in a transcript. The only command it
runs on your behalf is the one you pass to --test.
`)
	}

	// The documented surface is `entire impeach <ref> [flags]`, but the flag
	// package stops parsing at the first non-flag argument. Parse repeatedly,
	// lifting one positional out each time, so flags may appear on either
	// side of the reference. Letting fs.Parse consume the flags is what keeps
	// a value like --test "pytest -q" from being mistaken for a positional.
	var positional []string
	rest := argv
	for {
		if err := fs.Parse(rest); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	if len(positional) > 0 {
		opts.ref = positional[0]
	}
	if len(positional) > 1 {
		return nil, fmt.Errorf("expected one checkpoint or commit, got %d: %s",
			len(positional), strings.Join(positional, " "))
	}
	if err := validate(opts); err != nil {
		return nil, err
	}
	return opts, nil
}

func validate(opts *options) error {
	switch opts.format {
	case "table", "json", "html":
	default:
		return fmt.Errorf("unknown --format %q: want table, json or html", opts.format)
	}
	switch opts.failOn {
	case "", "impeached", "uncorroborated":
	default:
		return fmt.Errorf("unknown --fail-on %q: want impeached or uncorroborated", opts.failOn)
	}
	switch opts.adapter {
	case "auto", "claude-code":
	default:
		return fmt.Errorf("unknown --adapter %q: want auto or claude-code", opts.adapter)
	}
	if (opts.format == "json" || opts.format == "html") && opts.out == "" {
		return fmt.Errorf("--format %s needs --out to say where to write the report", opts.format)
	}
	return nil
}

// audit is the pipeline. Phase 2 wires resolve and the worktrees; the
// remaining stages arrive in their own phases behind the same boundaries.
func audit(ctx context.Context, opts *options, stdout, stderr *os.File) error {
	repo := opts.repo
	if repo == "" {
		// The dispatcher passes ENTIRE_REPO_ROOT, which is cheaper and more
		// reliable than asking git, but Impeach must still work when run
		// directly rather than through `entire impeach`.
		repo = strings.TrimSpace(os.Getenv("ENTIRE_REPO_ROOT"))
	}
	if repo == "" {
		repo = "."
	}

	log := &runner.Logged{Inner: runner.Exec{}}

	resolver := &checkpoint.Resolver{Run: log, Repo: repo}
	res, err := resolver.Resolve(ctx, opts.ref)
	if err != nil {
		return err
	}

	dataDir, err := record.DataDir()
	if err != nil {
		return err
	}
	wt, err := record.AddWorktrees(ctx, log, repo, dataDir, res.Commit, res.Parent, opts.keepWorktrees)
	if err != nil {
		return err
	}
	defer func() {
		// Cleanup must not inherit a cancelled context, or an interrupted run
		// would leave both worktrees behind.
		if cerr := wt.Close(context.WithoutCancel(ctx)); cerr != nil {
			fmt.Fprintf(stderr, "entire-impeach: %v\n", cerr)
		}
	}()

	printHeader(stdout, res, wt, opts)
	return nil
}

func printHeader(stdout *os.File, res *checkpoint.Resolved, wt *record.Worktrees, opts *options) {
	agent := res.Agent
	if agent == "" {
		agent = "unknown"
	}
	fmt.Fprintf(stdout, "Impeach %s report for checkpoint %s (commit %s, parent %s), agent %s.\n",
		Version, res.ID, short(res.Commit), short(res.Parent), agent)

	rerun := "yes"
	if opts.noRerun || opts.test == "" {
		rerun = "no"
	}
	model := opts.model
	if model == "" {
		model = "none"
	}
	test := opts.test
	if test == "" {
		test = "none"
	}
	fmt.Fprintf(stdout, "Adapter %s. Extractors: pattern. Test command: %s. Rerun: %s. Model command: %s.\n",
		opts.adapter, test, rerun, model)

	if res.IsMerge {
		fmt.Fprintf(stdout, "This is a merge commit; the diff is against the first parent.\n")
	}
	if len(res.Ambiguous) > 0 {
		fmt.Fprintf(stdout, "Checkpoint %s is carried by %d commits; auditing the newest. Others: %s.\n",
			res.ID, len(res.Ambiguous)+1, strings.Join(shortAll(res.Ambiguous), ", "))
	}
	if res.MetadataErr != "" {
		fmt.Fprintf(stdout, "Checkpoint metadata unavailable (%s); resolved from the commit trailer alone.\n", res.MetadataErr)
	}
	if len(res.SessionIDs) > 0 {
		fmt.Fprintf(stdout, "Sessions: %s.\n", strings.Join(res.SessionIDs, ", "))
	}
	base := wt.Base
	if base == "" {
		base = "none (root commit)"
	}
	fmt.Fprintf(stdout, "Worktrees: head %s, base %s.\n", wt.Head, base)
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	if sha == "" {
		return "none"
	}
	return sha
}

func shortAll(shas []string) []string {
	out := make([]string, len(shas))
	for i, s := range shas {
		out[i] = short(s)
	}
	return out
}
