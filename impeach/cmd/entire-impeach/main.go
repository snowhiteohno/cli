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
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/entireio/cli/impeach/internal/checkpoint"
	"github.com/entireio/cli/impeach/internal/claims"
	"github.com/entireio/cli/impeach/internal/record"
	"github.com/entireio/cli/impeach/internal/report"
	"github.com/entireio/cli/impeach/internal/runner"
	"github.com/entireio/cli/impeach/internal/transcript"
	"github.com/entireio/cli/impeach/internal/verify"
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
	// record writes every Runner call to a directory, turning a real audit
	// into a replayable scenario.
	record string
	// replay serves a previously recorded scenario instead of running
	// anything. This is what makes the end-to-end tests offline.
	replay string
	// scrub rewrites absolute paths and identities out of a recording before
	// it is written, because scenarios are committed.
	scrub bool
	// modelTurns caps how many assistant turns reach the model command, so
	// enabling --model on a long session cannot quietly send a large amount
	// of text to a third party.
	modelTurns int
	// sensitive forbids anything leaving the machine. It is a refusal, not a
	// preference: with it set, --model is rejected rather than ignored.
	sensitive bool
	// setup is the command run before the tests in each worktree. It can come
	// from .impeach.json, so setupSet records whether the flag was given
	// explicitly: passing --setup "" has to mean "no setup", which is
	// different from not passing it at all and falling back to the file.
	setup    string
	setupSet bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(argv []string, stdout, stderr io.Writer) int {
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
		if errors.Is(err, errFailOn) {
			return exitFailOn
		}
		fmt.Fprintf(stderr, "entire-impeach: %v\n", err)
		return exitError
	}
	return exitOK
}

func parseFlags(argv []string, stderr io.Writer) (*options, error) {
	opts := &options{}
	fs := flag.NewFlagSet("entire-impeach", flag.ContinueOnError)
	fs.SetOutput(stderr)

	fs.StringVar(&opts.test, "test", "", `test command to rerun in the checkpoint worktree`)
	fs.StringVar(&opts.setup, "setup", "", "command run before the tests in each worktree; its output never contributes test ids")
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
	fs.StringVar(&opts.record, "record", "", "record every external call to this directory, for replay")
	fs.StringVar(&opts.replay, "replay", "", "replay a recorded directory instead of running anything")
	fs.BoolVar(&opts.scrub, "scrub", true, "scrub absolute paths and identities out of a recording")
	fs.IntVar(&opts.modelTurns, "model-turns", 20, "cap on assistant turns sent to the --model command")
	fs.BoolVar(&opts.sensitive, "sensitive", false, "refuse anything that would leave the machine; --model becomes an error")

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
	// Whether --setup was given explicitly, as opposed to left to
	// .impeach.json. An explicit empty value means no setup at all.
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "setup" {
			opts.setupSet = true
		}
	})

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
	case "", "impeached", "uncorroborated", "incomplete":
	default:
		return fmt.Errorf("unknown --fail-on %q: want impeached, uncorroborated or incomplete", opts.failOn)
	}
	switch opts.adapter {
	case "auto", "claude-code":
	default:
		return fmt.Errorf("unknown --adapter %q: want auto or claude-code", opts.adapter)
	}
	// --format json prints to stdout when no --out is given, so the command
	// composes with a pipe. HTML is a file by nature and needs a directory.
	if opts.format == "html" && opts.out == "" {
		return fmt.Errorf("--format html needs --out to say where to write the report")
	}
	if opts.record != "" && opts.replay != "" {
		return fmt.Errorf("--record and --replay are mutually exclusive: one writes a scenario, the other reads one")
	}
	return nil
}

// audit is the pipeline. Phase 2 wires resolve and the worktrees; the
// remaining stages arrive in their own phases behind the same boundaries.
func audit(ctx context.Context, opts *options, stdout, stderr io.Writer) error {
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
	// Absolute from here on. The adapter relativizes the absolute paths in
	// tool inputs against this root, and a relative root cannot do that job.
	// It also keeps the worktree key and Graph's --repo unambiguous.
	abs, err := filepath.Abs(repo)
	if err != nil {
		return fmt.Errorf("resolve repository path %q: %w", repo, err)
	}
	repo = abs

	// Sensitive mode comes from the flag or from a committed .impeach.json,
	// and is resolved before anything runs. The refusal has to happen here,
	// ahead of the pipeline, because refusing after the first turn has been
	// sent would be theatre.
	cfg, cfgErr := loadConfig(repo)
	if cfgErr != nil {
		fmt.Fprintf(stderr, "entire-impeach: %v\n", cfgErr)
		cfg = &config{}
	}
	if cfg.Sensitive {
		opts.sensitive = true
	}
	if opts.sensitive && opts.model != "" {
		return fmt.Errorf(
			"refusing to run: --model would send assistant text to %q, and sensitive mode forbids anything leaving this machine. "+
				"Drop --model, or drop --sensitive and remove \"sensitive\" from %s if that is really what you want",
			opts.model, configName)
	}

	base, err := buildRunner(opts, repo)
	if err != nil {
		return err
	}
	log := &runner.Logged{Inner: base}

	resolver := &checkpoint.Resolver{Run: log, Repo: repo}
	res, err := resolver.Resolve(ctx, opts.ref)
	if err != nil {
		return err
	}

	// Session mode loops the per-checkpoint pipeline and prints one section
	// each. Everything below is unchanged per checkpoint; only the reference
	// being audited moves.
	if opts.session {
		return auditSession(ctx, opts, repo, log, resolver, res, stdout, stderr)
	}
	return auditOne(ctx, opts, repo, log, resolver, res, stdout, stderr, nil)
}

// auditSession audits every checkpoint in the session the reference belongs
// to, oldest first.
//
// The prompt corpus for unrequested detection is the union across the whole
// session, because a symbol asked for in the first checkpoint is not
// unrequested when it appears in the third. Scoping the corpus per checkpoint
// would flag most of a multi-step session.
func auditSession(ctx context.Context, opts *options, repo string, log *runner.Logged,
	resolver *checkpoint.Resolver, res *checkpoint.Resolved, stdout, stderr io.Writer) error {

	if len(res.SessionIDs) == 0 {
		return fmt.Errorf("checkpoint %s reports no session, so --session has nothing to loop over", res.ID)
	}
	sessionID := res.SessionIDs[0]

	ids, err := resolver.SessionCheckpoints(ctx, sessionID)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Session %s: %d checkpoints, oldest first.\n", sessionID, len(ids))

	// First pass collects the prompts, so the union corpus is known before
	// any verdict is decided.
	corpus := sessionPrompts(ctx, resolver, repo, opts, ids)

	var failOn bool
	for i, id := range ids {
		fmt.Fprintf(stdout, "\n%s\nCheckpoint %d of %d: %s\n%s\n",
			strings.Repeat("=", 72), i+1, len(ids), id, strings.Repeat("=", 72))

		one, err := resolver.Resolve(ctx, id)
		if err != nil {
			fmt.Fprintf(stdout, "Could not resolve %s: %v\n", id, err)
			continue
		}
		err = auditOne(ctx, opts, repo, log, resolver, one, stdout, stderr, corpus)
		switch {
		case errors.Is(err, errFailOn):
			failOn = true
		case err != nil:
			// One checkpoint failing does not end the session audit; the
			// remaining sections are still worth having.
			fmt.Fprintf(stdout, "Checkpoint %s could not be audited: %v\n", id, err)
		}
	}
	if failOn {
		return errFailOn
	}
	return nil
}

// sessionPrompts gathers the prompts across a session for the union corpus.
// A checkpoint whose transcript cannot be read simply contributes nothing.
func sessionPrompts(ctx context.Context, resolver *checkpoint.Resolver, repo string,
	opts *options, ids []string) []string {

	var out []string
	for _, id := range ids {
		one, err := resolver.Resolve(ctx, id)
		if err != nil {
			continue
		}
		stream, err := readTestimony(ctx, resolver, one, repo, opts)
		if err != nil {
			continue
		}
		out = append(out, stream.Prompts()...)
	}
	return out
}

// auditOne runs the pipeline for a single checkpoint.
//
// promptCorpus overrides the prompts used for unrequested detection. It is nil
// for a single-checkpoint run, where the checkpoint's own prompts are the
// right corpus, and set in session mode to the union.
func auditOne(ctx context.Context, opts *options, repo string, log *runner.Logged,
	resolver *checkpoint.Resolver, res *checkpoint.Resolved, stdout, stderr io.Writer,
	promptCorpus []string) error {

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

	var notes []string

	stream, err := readTestimony(ctx, resolver, res, repo, opts)
	if err != nil {
		// A missing or unreadable transcript is a state, not a failure. With
		// no testimony there are no claims to check, so the run reports that
		// and stops rather than inventing rows.
		notes = append(notes, fmt.Sprintf("Testimony unavailable (%v); execution and reading claims are unverifiable.", err))
		stream = &transcript.Stream{Adapter: opts.adapter}
	}
	notes = append(notes, testimonyNotes(stream)...)

	rec := buildRecord(ctx, log, repo, res, wt, opts, stdout, stderr)
	if rec.ChangesErr != "" {
		notes = append(notes, fmt.Sprintf("Entity diff unavailable (%s); structural claims are unverifiable.", rec.ChangesErr))
	}
	if rec.Rerun != nil && rec.Rerun.ExitCodeOnly {
		notes = append(notes, "The test command emits no per-test ids, so the rerun was adjudicated on the exit code alone. "+
			"Add -v, or -q --tb=no -rA for pytest, to get per-test evidence.")
	}
	if res.MetadataErr != "" {
		notes = append(notes, fmt.Sprintf("Checkpoint metadata unavailable (%s); resolved from the commit trailer alone.", res.MetadataErr))
	}
	if len(res.Ambiguous) > 0 {
		notes = append(notes, fmt.Sprintf("Checkpoint %s is carried by %d commits; auditing the newest.",
			res.ID, len(res.Ambiguous)+1))
	}

	rep := assemble(res, stream, rec, opts, log, notes, promptCorpus)

	if err := emit(rep, opts, stdout); err != nil {
		return err
	}
	if failOnMet(rep, opts.failOn) {
		return errFailOn
	}
	return nil
}

// emit writes the report in the requested formats.
//
// The table always goes to stdout unless the format explicitly replaces it,
// because a run that prints nothing to the terminal looks like a run that did
// nothing. JSON and HTML are additionally written to --out whenever --out is
// given, which is what makes `--format table --out dir` useful.
func emit(rep *report.Report, opts *options, stdout io.Writer) error {
	if opts.format == "table" || opts.out != "" {
		if err := report.Table(stdout, rep); err != nil {
			return err
		}
	}
	if opts.out == "" {
		if opts.format == "json" {
			return report.WriteJSON(stdout, rep)
		}
		return nil
	}

	if err := os.MkdirAll(opts.out, 0o755); err != nil {
		return fmt.Errorf("create --out directory %s: %w", opts.out, err)
	}
	jsonPath := filepath.Join(opts.out, "impeach.json")
	blob, err := report.ToJSON(rep)
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, blob, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", jsonPath, err)
	}

	// The HTML report sits next to the JSON, and embeds it, so a reader can
	// copy the data straight out of the page.
	htmlPath := filepath.Join(opts.out, "impeach.html")
	page, err := report.HTML(rep)
	if err != nil {
		return err
	}
	if err := os.WriteFile(htmlPath, page, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", htmlPath, err)
	}

	fmt.Fprintf(stdout, "\nWrote %s\nWrote %s\n", jsonPath, htmlPath)
	return nil
}

// errFailOn signals that the --fail-on condition was met, which is exit 2 and
// not a runtime error.
var errFailOn = errors.New("fail-on condition met")

// buildRunner picks the Runner for this run.
//
// This is the whole point of having one Runner boundary. Recording, replaying
// and really executing are three implementations of one interface, so nothing
// downstream knows or cares which it got.
func buildRunner(opts *options, repo string) (runner.Runner, error) {
	switch {
	case opts.replay != "":
		f, err := runner.LoadFake(opts.replay)
		if err != nil {
			return nil, err
		}
		// A replayed scenario missing a call should fail loudly rather than
		// quietly return an empty success, which would look like a channel
		// that legitimately had nothing in it.
		f.Strict = true
		// The same transformation the recording applied, so a scrubbed
		// scenario still matches live calls.
		f.Normalize = scrubber(repo)
		return f, nil

	case opts.record != "":
		rec := &runner.Recording{Inner: runner.Exec{}, Dir: opts.record}
		if opts.scrub {
			rec.Scrub = scrubber(repo)
		}
		return rec, nil

	default:
		return runner.Exec{}, nil
	}
}

// scrubber rewrites machine-specific strings out of a recording.
//
// Recorded scenarios are committed and read by strangers, so absolute paths,
// the home directory and the user name all have to go. The replacements are
// stable placeholders rather than deletions, so a replayed path still
// relativizes the way a real one does.
func scrubber(repo string) func(string) string {
	home, _ := os.UserHomeDir()
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("LOGNAME")
	}
	var replacements [][2]string
	if repo != "" {
		replacements = append(replacements, [2]string{repo, "<repo>"})
	}
	if home != "" && home != repo {
		replacements = append(replacements, [2]string{home, "<home>"})
	}
	if len(user) > 2 {
		replacements = append(replacements, [2]string{user, "<user>"})
	}
	return func(s string) string {
		for _, r := range replacements {
			s = strings.ReplaceAll(s, r[0], r[1])
		}
		return report.Scrub(s)
	}
}

func testimonyNotes(s *transcript.Stream) []string {
	var notes []string
	if len(s.Events) == 0 {
		return notes
	}
	ch := s.Channels()
	if !ch.Commands {
		notes = append(notes, "This transcript carries no command records; execution claims are unverifiable.")
	}
	if !ch.Reads {
		notes = append(notes, "This transcript carries no read records; reading claims are unverifiable.")
	}
	if s.SubagentRecords > 0 {
		notes = append(notes, fmt.Sprintf("%d subagent records not examined.", s.SubagentRecords))
	}
	if !s.TimestampsPresent() {
		notes = append(notes, "This transcript carries no timestamps; ordering and the report use turn numbers.")
	}
	return notes
}

// assemble runs the extractors and the verifiers and builds the report.
func assemble(res *checkpoint.Resolved, stream *transcript.Stream, rec *auditRecord,
	opts *options, log *runner.Logged, notes []string, promptCorpus []string) *report.Report {

	pattern := claims.Pattern{}
	names := []string{pattern.Name()}
	found, err := pattern.Extract(stream.Events)
	if err != nil {
		notes = append(notes, fmt.Sprintf("Claim extraction failed (%v).", err))
	}

	// The model extractor is opt in and adds to the pattern library rather
	// than replacing it. Its claims carry the same type and go through the
	// same verifiers, so a model can suggest what to check but never what the
	// answer is.
	if opts.model != "" {
		m := &claims.Model{Command: opts.model, Runner: log, MaxTurns: opts.modelTurns}
		extra, mErr := m.Extract(stream.Events)
		if mErr != nil {
			notes = append(notes, fmt.Sprintf("Model extractor failed (%v); pattern claims are unaffected.", mErr))
		} else {
			found = append(found, extra...)
			names = append(names, m.Name())
		}
		for _, w := range m.Warnings {
			notes = append(notes, "Model extractor: "+w+".")
		}
	}

	// The ledger is the transcript's own account of which channels survived,
	// plus the record layer's answer for Graph, which is not a transcript
	// channel at all.
	ledger := stream.BuildLedger()
	switch {
	case rec.Changes == nil:
		ledger.Set(transcript.ChannelGraph, transcript.ChannelAbsent,
			"Entire Graph produced no entity diff for this commit")
	case len(rec.Changes.Warnings) > 0:
		ledger.Set(transcript.ChannelGraph, transcript.ChannelPartial,
			"Entire Graph reported "+strings.Join(rec.Changes.Warnings, "; "))
	default:
		ledger.Set(transcript.ChannelGraph, transcript.ChannelPresent, "")
	}

	vr := &verify.Record{
		Stream:       stream,
		Changes:      rec.Changes,
		Rerun:        rec.Rerun,
		FilesTouched: res.FilesTouched,
		Impacts:      rec.Impacts,
		RepoRoot:     opts.repo,
		Ledger:       ledger,
	}

	// One verifier per family, chosen by the claim's own family. A claim from
	// the model extractor goes through the same table, which is the point of
	// giving both extractors one Claim type.
	verifiers := map[claims.Family]verify.Verifier{
		claims.Execution:  verify.Execution{TestCommand: rec.TestCommand},
		claims.Structural: verify.Structural{},
		claims.Safety:     verify.Safety{},
		claims.Reading:    verify.Reading{},
	}

	rows := make([]report.Row, 0, len(found))
	for _, c := range found {
		v, ok := verifiers[c.Family]
		if !ok {
			continue
		}
		rows = append(rows, report.Row{Claim: c, Verdict: v.Verify(c, vr)})
	}

	// In session mode the corpus is the union of every checkpoint's prompts,
	// so a symbol asked for earlier is not flagged when it lands later.
	prompts := promptCorpus
	if prompts == nil {
		prompts = stream.Prompts()
	}
	unrequested := verify.DetectUnrequested(rec.Changes, prompts)

	ch := stream.Channels()
	in := report.Inputs{
		Adapter:      adapterName(stream, opts),
		Extractors:   names,
		TestCommand:  rec.TestCommand,
		Rerun:        rec.Rerun != nil && rec.Rerun.Status != record.RerunNotRun,
		ModelCommand: opts.model,
		Channels: map[string]bool{
			"commands": ch.Commands, "reads": ch.Reads,
			"edits": ch.Edits, "prompts": ch.Prompts,
			"graph": rec.Changes != nil,
		},
	}
	cp := report.Checkpoint{
		ID: res.ID, Commit: res.Commit, Parent: res.Parent,
		Agent: res.Agent, SessionIDs: res.SessionIDs, IsMerge: res.IsMerge,
	}
	rep := report.New(cp, in, rows, unrequested, notes, limitations(), log.Calls())
	rep.Ledger = ledger
	rep.Sensitive = opts.sensitive
	return rep
}

func adapterName(s *transcript.Stream, opts *options) string {
	if s.Adapter != "" {
		return s.Adapter
	}
	return opts.adapter
}

// limitations are the standing caveats, always printed, because a reader has
// to know what the tool cannot see.
func limitations() []string {
	return []string{
		"Claim detection is pattern based. Vaguely phrased claims are missed; a missed claim is silent, not a false one.",
		"Result parsing knows pytest, go test, jest, cargo, rspec, phpunit, maven and gradle summaries. Other runners fall to uncorroborated.",
		"The fixture app under impeach/fixtures/app is seeded to exercise each verdict.",
		"One transcript adapter, claude-code. Other agents degrade to unverifiable rows by design.",
	}
}

func failOnMet(r *report.Report, failOn string) bool {
	switch failOn {
	case "impeached":
		return r.Counts.Impeached > 0
	case "uncorroborated":
		return r.Counts.Impeached > 0 || r.Counts.Uncorroborated > 0
	case "incomplete":
		// Gating on partial context, so CI can refuse to accept a run whose
		// evidence was incomplete even when nothing was impeached. No new
		// exit code: this is the same exit 2 as any other --fail-on.
		return r.Ledger != nil && !r.Ledger.Complete()
	default:
		return false
	}
}

// auditRecord is the record side of the cross-examination.
type auditRecord struct {
	Changes *record.CommitChanges
	Rerun   *record.Rerun
	// Impacts caches graph impact results by symbol name.
	Impacts map[string]*record.Impact
	// TestCommand is the command actually used, from --test or .impeach.json.
	TestCommand string
	// ChangesErr records why the entity diff is unavailable, if it is. A
	// missing record channel is a state, so the structural rows become
	// unverifiable rather than the run failing.
	ChangesErr string
}

func buildRecord(ctx context.Context, run runner.Runner, repo string, res *checkpoint.Resolved,
	wt *record.Worktrees, opts *options, stdout, stderr io.Writer) *auditRecord {
	out := &auditRecord{
		Rerun:   &record.Rerun{Status: record.RerunNotRun},
		Impacts: map[string]*record.Impact{},
	}

	g := &record.Graph{Runner: run}
	changes, err := g.Commit(ctx, wt.Head, res.Commit)
	if err != nil {
		out.ChangesErr = err.Error()
	} else {
		out.Changes = changes
		// Impact is asked once per changed symbol that a safety claim could
		// be about, and once per added symbol so the unrequested severity has
		// a dependent count. Body-only changes are skipped: nothing rests on
		// their blast radius.
		for _, ch := range changes.Changes {
			if ch.Kind == record.BodyChanged || ch.Name == "" {
				continue
			}
			if _, done := out.Impacts[ch.Name]; done {
				continue
			}
			imp, ierr := g.Impact(ctx, wt.Head, ch.Name, true)
			if ierr != nil {
				fmt.Fprintf(stderr, "entire-impeach: %v\n", ierr)
				continue
			}
			out.Impacts[ch.Name] = imp
		}
	}

	test, setup := opts.test, opts.setup
	if cfg, cfgErr := loadConfig(repo); cfgErr != nil {
		fmt.Fprintf(stderr, "entire-impeach: %v\n", cfgErr)
	} else {
		if test == "" {
			test = cfg.Test
		}
		// The flag wins when given, including when given as empty, so a
		// committed setup command can always be turned off from the command
		// line. Without this a config could silently change what a run does
		// with no way to override it.
		if !opts.setupSet {
			setup = cfg.Setup
		}
	}

	out.TestCommand = test

	switch {
	case opts.noRerun:
		out.Rerun = &record.Rerun{Status: record.RerunNotRun, Reason: "--no-rerun"}
	case test == "":
		out.Rerun = &record.Rerun{Status: record.RerunNotRun,
			Reason: "no test command; pass --test or commit an " + configName}
	default:
		// The command is echoed before it runs. It executes as the user, so
		// the user gets to see exactly what was launched.
		fmt.Fprintf(stdout, "Rerunning: %s\n", test)
		v := &record.Verifier{Runner: run}
		out.Rerun = v.Run(ctx, record.RerunOptions{
			Test:         test,
			Setup:        setup,
			HeadRepo:     wt.Head,
			BaseRepo:     wt.Base,
			BaselinePath: filepath.Join(wt.Root(), "baseline.json"),
		})
	}
	return out
}

// readTestimony fetches the transcript and parses it with the chosen adapter.
func readTestimony(ctx context.Context, resolver *checkpoint.Resolver, res *checkpoint.Resolved,
	repo string, opts *options) (*transcript.Stream, error) {
	t, err := resolver.FetchTranscript(ctx, res.ID, -1)
	if err != nil {
		return nil, err
	}

	a, err := pickAdapter(opts.adapter, repo, t.Raw)
	if err != nil {
		return nil, err
	}
	return a.ParseStream(t.Raw)
}

// pickAdapter chooses the transcript adapter.
//
// The registry is the boundary that absorbs a change of agent: a new agent is
// a new entry here and nothing else. With --adapter auto the transcript
// decides; with an explicit name the user does, which matters when a
// transcript is recognised by more than one adapter or by none.
func pickAdapter(name, repo string, raw []byte) (*transcript.ClaudeCode, error) {
	cc := &transcript.ClaudeCode{RepoRoot: repo}
	registry := map[string]*transcript.ClaudeCode{"claude-code": cc}

	if name != "auto" {
		a, ok := registry[name]
		if !ok {
			return nil, fmt.Errorf("no adapter named %q", name)
		}
		return a, nil
	}
	adapters := make([]transcript.Adapter, 0, len(registry))
	for _, a := range registry {
		adapters = append(adapters, a)
	}
	if _, err := transcript.Detect(raw, adapters); err != nil {
		return nil, err
	}
	return cc, nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
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
