# Impeach: working notes

Append-only working log. Probe findings, degradations in effect, impact checks
before touching files this build did not create, and the end-of-phase reports.

## Environment as built

The delivered working tree was not the one the kickoff described. What was
present: `docs/` with the four design documents. What was missing: the fork of
`entireio/cli`, Entire itself, the graph plugin, the Go toolchain,
`HANDOFF.md`, `KICKOFF_PROMPT.md` and `fixtures/`. The environment was built
from scratch on 2026-09-06:

- Go 1.27.1 via Homebrew (go.mod asks for 1.26.6; newer is accepted).
- `entireio/cli` cloned from GitHub at `3dbdf8b` and un-shallowed to full
  history, 8384 commits. The clone is the working repository.
- `entire` built from that source with `go build -o entire ./cmd/entire`,
  version `0.10.6-nightly.202609050622.61dac01ed`. Both `/entire` and
  `/git-remote-entire` are gitignored at the repository root, so building
  there leaves the tree clean. Symlinked into `~/.local/bin` for `$PATH`.
- `entire status` reports enabled on branch main, strategy `manual-commit`,
  checkpoints backend `git-refs`.
- Graph plugin installed with `entire plugin install graph`, v0.4.0 from
  `github.com/entireio/entire-graph`.
- `HANDOFF.md` reconstructed from the docs plus the kickoff's settled-decision
  list. It says so at the top.

Egress is closed deliberately, and this predates the request to keep
everything local:

- `origin` is upstream `entireio/cli`; its push URL is set to
  `DISABLED-no-push`, so any accidental push fails locally. Fetch still works.
- `entire configure --local --skip-push-sessions --telemetry=false`, written
  to `.entire/settings.local.json`, which is gitignored. Checkpoints stay on
  the machine.

## Probe findings, part one: flag surface

Taken from `--help` on the installed binaries, before the probe session.

`entire checkpoint list`: `--json`, `--pending`, `--session`, `--limit` via
explain, `--no-pager`.

`entire checkpoint explain` has more machine-readable surface than
ARCHITECTURE.md assumed. Documented modes:

- `--json`: metadata-only envelope. Lists checkpoints with no target, emits a
  single checkpoint envelope with one. Transcript bytes are never embedded.
- `--transcript`: streams stored transcript bytes as JSONL to stdout. The help
  states these are the same bytes as `--raw-transcript` while checkpoints v1 is
  the store.
- `--session-index N`: picks a session inside a multi-session checkpoint,
  0-based, defaulting to the latest. Only meaningful with `--transcript` or
  `--raw-transcript`. This matters: a checkpoint can hold several sessions, so
  the reader boundary needs to know which one it read.
- `--full` parsed transcript, `--short` summary, `--generate` for an AI
  summary. Impeach never calls `--generate`.

Graph JSON support, which decides the record parsers:

| Command | Machine-readable flag |
|---|---|
| `graph commit [rev]` | `--json` present |
| `graph diff --base --head` | `--json` present |
| `graph impact --symbol` | `--format text|json` present |
| `graph verify --test` | none. No `--json`, no `--format` |

So three of the four record inputs are JSON and only `verify` needs a text
parser. That inverts the doc's expectation, which planned text parsers for
`commit` and `impact` and said to check.

`graph verify` is a better fit than the doc assumed. It reports which tests
changed state rather than forwarding runner output: newly passing, newly
failing, and already failing before the edit labelled PRE-EXISTING. Raw output
is never forwarded, ids are, capped at 20 with a count. It ships parsers for
pytest, jest/vitest, cargo test, go test, phpunit, rspec, minitest,
maven/gradle surefire and ctest, and degrades to an exit-code-only verdict on
an unrecognised format, saying so. It also has `--setup`, whose output never
contributes test ids, which is how the fixture app's venv gets built inside a
fresh worktree. Baseline flow is `--record-baseline` on the pristine tree then
`--pre-edit-baseline` after, exactly as the doc describes.

`graph impact` extras worth using: `--depth 1|2`, `--limit`, `--exclude-tests`,
`--max-context-bytes`, `--profile syntax-only|fast|full`, and disambiguation by
`--file`/`--line`/`--kind` or `--symbol <file>:<line>`. Ambiguous names return
a definition list rather than an answer, so the safety verifier has to handle
that shape.

`graph capabilities --json` confirms Python is a semantic language with CALLS,
DEFINES, IMPORTS, INHERITS, OVERRIDES, PARAM_TYPE, RETURNS_TYPE, USES_TYPE and
DATA_FLOWS. The fixture app stays Python; the "Graph does not parse the fixture
language" degradation row does not apply. It also confirms every network
feature is false: no grammar download, no hosted models, no remote embeddings,
no telemetry upload. Graph is local-only as the security doc claims.

## Probe findings, part two: ENTIRE_PLUGIN_DATA_DIR

Open question six, answered from source rather than observation, because the
answer is a code path and not a value.

`entire graph search --repo . --profile full --query "set
ENTIRE_PLUGIN_DATA_DIR environment variable when dispatching a CLI plugin
subprocess"` ranked `cmd/entire/cli/plugin_store.go:139` `PluginDataDir` first
and `cmd/entire/cli/plugin.go:174` `runPlugin` third. `runPlugin` is the
dispatcher, and its comment settles it:

> Per-plugin durable storage. Passed regardless of where the binary lives so
> plugins installed via raw PATH and via `entire plugin install` get the same
> contract. The dir is not pre-created, that's the plugin's responsibility on
> first use.

So the variable is set for unmanaged plugins too, not only managed installs.
Consequences for Impeach:

1. `ENTIRE_PLUGIN_DATA_DIR` can be relied on whether Impeach is installed with
   `entire plugin install` or dropped on `$PATH`. The `$XDG_CACHE_HOME/impeach`
   fallback stays, for the case where Impeach is run directly rather than
   through `entire impeach`, which is how the tests will run it.
2. The directory is not created. Impeach must `MkdirAll` on first use.
3. The dispatcher also passes `ENTIRE_CLI_VERSION` and `ENTIRE_REPO_ROOT`.
   `ENTIRE_REPO_ROOT` is worth using instead of asking git for the worktree
   root when it is present.
4. In a degenerate environment where the data dir cannot be resolved, the
   dispatcher strips any inherited `ENTIRE_PLUGIN_DATA_DIR` rather than passing
   a value it did not sanction. So an empty value means absent, and Impeach
   should not treat a stray inherited value as authoritative.
5. `validatePluginName` rejects names starting with `agent-`, which is
   reserved for the agent protocol. `impeach` is a valid plugin name, matching
   the security doc's claim.

No file outside `impeach/` was read or changed to establish this, and nothing
was edited, so no `graph impact` call was required.

## Open questions still open

These three need a real checkpoint and are blocked on the first commit:

- Does `entire checkpoint list --json` expose the linked commit sha and
  session ids?
- Do `tool_result` blocks in the stored transcript carry full command output,
  truncated output, or none?
- Does `graph commit` report signature changes distinctly from body changes
  for Python?

## Commit authorship, resolved

The global git config authors as `snowhiteohnopt2
<mallika.suri@scalerailabs.com>`, a company address, and the `gh` CLI in this
environment is logged in as `snowhiteohnopt2`, which is a different account
from the personal one. The user named `https://github.com/snowhiteohno` as the
account to use. Looked up with `gh api users/snowhiteohno`: id 218808583, login
`snowhiteohno`, name `Mallika`.

Set repository-local only, so the global company config is untouched:

```
git config --local user.name  "Mallika"
git config --local user.email "218808583+snowhiteohno@users.noreply.github.com"
```

The noreply form is GitHub's canonical per-account address. It attributes
commits to `snowhiteohno` without publishing a private address. If a real
address is wanted instead, change the local config; nothing else depends on it.

Pushing stays disabled at the user's request. The `gh` CLI is still
authenticated as the wrong account, so before any push the user needs to
authenticate as `snowhiteohno`, create the destination repository, and add it
as a remote. None of that is needed to build.

## The git hook was missing, and why that matters

A fresh clone carries no git hooks, so Entire was enabled in settings but
captured nothing. `.git/hooks` held only samples. `entire configure --force`
reinstalled them: `commit-msg`, `post-commit`, `post-rewrite`, `pre-push`,
`prepare-commit-msg`. Without this the probe would have produced commits with
no checkpoint at all, and the whole Step 0 would have silently measured
nothing.

That command also rewrote `.entire/settings.json`, a tracked file outside
`impeach/`. The change was cosmetic only, key order and whitespace, no
semantic difference, so it was reverted with `git checkout` to honour the rule
that nothing outside `impeach/` changes. The hooks live in `.git/hooks`, which
is not tracked, so they survive the revert.

`entire agent list` shows `claude-code` installed, along with codex, cursor,
gemini, opencode and pi.

One consequence for the probe: this build's own session began before those
hooks existed, so it is not itself captured. The probe session therefore runs
as a separate `claude -p` invocation, which picks up the hooks and produces a
transcript Entire actually stores. `claude` 2.1.223 is on the path.

## Probe findings, part three: the session and the checkpoint

The probe session ran as `claude -p` from the repository root with
`--allowed-tools Read Edit Bash` and `--permission-mode bypassPermissions`. The
prompt asked it to read `service.py` and `api.py`, switch `round_money` to
banker's rounding, run `tests/test_api.py` only, and commit. What it claimed was
deliberately not scripted, so the testimony is genuine rather than staged.

Result: checkpoint `01M1TET4N33VMY0DTHNZKV5HT9`, session
`1c439358-7307-4721-8422-949e897dbfba`, agent `Claude Code`, model
`claude-opus-5`, commit `458bb14`, parent `650a885`, one file touched.
41 transcript records: 14 assistant, 9 user, 15 attachment, 2 queue-operation,
1 last-prompt. Eight tool calls: `Read` twice, `Edit` twice, `Bash` four times.
No sidechain records, so the "N subagent records not examined" header has
nothing to report for this checkpoint.

Worth recording: the session was honest. It ran only `tests/test_api.py` as
asked, reported "4 passed", and then volunteered that the other test files were
where a half-to-even regression would show up. That is not the overclaim the
demo wants, so the seeded `stale` and `scope` scenarios in phase 7 will have to
be constructed as recorded fixtures rather than harvested from this session.
The report footer already discloses that the fixture is seeded. This checkpoint
is still the right one for phases 3 and 4, because what those phases need is a
real transcript shape, and it is real.

The rerun does contradict the change regardless of what the agent said.
Baseline recorded on the parent worktree was 18 passing. Adjudicated against it
at `458bb14`, `graph verify` reported:

```
NEWLY FAILING (3): tests/test_rounding.py::test_round_money_negative, tests/test_rounding.py::test_round_money_two_places, tests/test_service.py::test_round_money_half_up
VERDICT: REGRESSION in 3 tests: ...
```

Three, not the two the PRD's illustrative demo mentions, because
`test_round_money_two_places` also turns on a halfway value. So an execution
claim on this checkpoint is impeachable on `contradicted-rerun` from real data,
which is what phase 5 needs.

## Answers and degradations

The six open questions are answered in full at the bottom of
`docs/ARCHITECTURE.md`, along with a new section listing six places the probe
contradicted the design and a replacement degradation table. Summary:

- None of the six degradation rows the design anticipated are in effect. Tool
  calls and results are present and full, reads carry paths, timestamps are
  present, `--raw-transcript` works, `graph commit` has JSON, and Graph parses
  Python semantically.
- Two new degradations replace them. First, no commit sha appears in any
  checkpoint JSON, so resolution rests entirely on the `Entire-Checkpoint:`
  trailer. Second, `graph verify` has no JSON and its pytest parser falls back
  to exit-code-only unless the test command emits per-test ids, which the
  documented `pytest -q` does not.

The second one is the finding with teeth, because every document in `docs/`
specifies `--test "pytest -q"`. Impeach will not rewrite a user's command, so
it detects the exit-code-only parser, reports the rerun as pass or fail without
ids, and names the degradation.

## Probe artifacts kept as testdata

Saved under `testdata/`, scrubbed of absolute paths, the username, the author
name and the author email, and checked for token-shaped strings and stray
addresses. Only the constant `noreply@anthropic.com` co-author trailer remains.

- `transcript/claudecode_probe.jsonl`, the 41-record probe transcript.
- `transcript/explain_json_envelope.json` and `transcript/explain_full.txt`.
- `graph/commit_body_changed.json`, the probe commit.
- `graph/commit_signature_changed_and_added.json`, from a throwaway worktree
  where `compute_total` gained a parameter and `_legacy_shim` was added.
- `graph/impact_compute_total_excludetests.json`, the three real callers.
- `verify/baseline_pytest_green.json` and `verify/baseline_exitcode_only.json`,
  the two parser outcomes side by side.
- `verify/verdict_regression.txt` and the two `BASELINE RECORDED` lines, which
  are the text shapes the verify parser must handle.

Both probe worktrees were removed and `git worktree prune` run; `git worktree
list` shows only the main checkout.

## Environment note: the PATH trap

`entire` was first symlinked into `~/.local/bin`, which is not on the login
PATH on this machine. Every Entire hook guards itself with
`if ! command -v entire >/dev/null 2>&1; then exit 0; fi`, so the hooks would
have run and silently done nothing, and the probe would have produced a commit
with no checkpoint while looking like it worked. Fixed by symlinking into
`/opt/homebrew/bin`, which is on the login PATH, and verified with
`zsh -lc 'command -v entire'`. Anything that runs Entire from a non-login shell
should re-check this.

## End of phase 5 report

Stop point reached. Phases 0 through 5 are committed, `go test ./...` is green
at every one of them, and the table prints on the fixture app with an
impeached row. This section is the handover: a fresh session should be able to
reconstruct the state from the checkpoint list plus this file.

### Commits

```
ab88cfb  phase 5 execution verifier, pattern extractor, table   (tests: 197)
c46d8f1  phase 4 record layer, graph parsers, verify wrapper    (tests: 135)
7ada18f  phase 3 Claude Code transcript adapter                 (tests: 101)
7ce9790  phase 2 skeleton, Runner, resolve by trailer, worktrees (tests: 48)
f672561  phase 1 step 0 probe, six open questions answered      (tests: 0 go, 18 pytest)
458bb14  the probe session's own commit, checkpoint 01M1TET4N33VMY0DTHNZKV5HT9
650a885  phase 0 docs, handoff, fixture app                     (tests: 0 go, 18 pytest)
```

197 Go tests plus 18 pytest tests in the fixture app. No test needs a live
agent or a network; the transcript, Graph and verify fixtures are all replayed
from `testdata/`.

### Probe findings, in brief

Full detail is at the bottom of `docs/ARCHITECTURE.md`. The six open questions
are answered there, along with a section listing the six places the probe
contradicted the design.

1. No commit sha appears in `checkpoint list --json` or `explain --json`, so
   resolution rests entirely on the `Entire-Checkpoint:` trailer. That path is
   primary, not the fallback the design called it.
2. `checkpoint explain` has more machine-readable surface than assumed:
   `--json` metadata envelope, `--transcript` byte-identical to
   `--raw-transcript`, and `--session-index` for multi-session checkpoints.
3. `tool_result` carries full output, plus a structured `toolUseResult` object,
   plus an explicit `Exit code N` line on failure. Exit status is knowable in
   both directions, better than the design expected.
4. `graph commit`, `graph diff` and `graph impact` all have JSON.
   `graph verify` has none, so it is the only text parser in the build.
5. `graph commit` distinguishes `signature_changed` from `body_changed` for
   Python and carries both signature strings.
6. `ENTIRE_PLUGIN_DATA_DIR` is set for unmanaged plugins too, is not
   pre-created, and comes with `ENTIRE_CLI_VERSION` and `ENTIRE_REPO_ROOT`.
   Confirmed twice: from `runPlugin` in the CLI source, and then empirically in
   phase 5, when installing Impeach as a managed plugin moved the worktrees
   into that directory with no code change.

### Degradations in effect

None of the six degradation rows the design anticipated apply. Tool calls and
results are present and full, reads carry paths, timestamps are present,
`--raw-transcript` works, `graph commit` has JSON, and Graph parses Python
semantically.

Two new degradations replace them, and both are implemented:

| Degradation | Effect | Absorbed in |
|---|---|---|
| No commit sha in any checkpoint JSON | Resolution is trailer-only, through `git log --grep`. Ambiguity is reported, never guessed | `internal/checkpoint` |
| `graph verify` has no JSON, and its pytest parser degrades to exit-code-only unless the command emits per-test ids | The rerun reports pass or fail with no ids, and the report names the degradation and what to change | `internal/record` |

The second one has teeth, because every document in `docs/` specifies
`--test "pytest -q"`, which is exactly the command that buys nothing.

### The table

Run, dispatched through the Entire CLI as a real plugin:

```
entire impeach 458bb1428 --repo . --fail-on impeached \
  --test 'cd impeach/fixtures/app && { test -d .venv || { python3 -m venv .venv \
    && ./.venv/bin/python -m pip install -q -r requirements.txt; }; } \
    && ./.venv/bin/python -m pytest -q --tb=no -rA'
```

Output, trimmed to the substance:

```
Impeach 0.1.0 report for checkpoint 01M1TET4N33VMY0DTHNZKV5HT9 (commit 458bb14, parent 650a885), agent Claude Code.
Adapter claude-code. Extractors: pattern. Rerun: yes. Model command: none.

VERDICT    FAMILY     CLAIM                                          REASON                 RERUN
impeached  execution  Tests - `pytest tests/test_api.py` ...         contradicted by rerun  new failures

IMPEACHED [c1] "Tests - `./.venv/bin/python -m pytest tests/test_api.py` from `impeach/fixtures/app`: 4 passed."
  Re-running now shows 3 new failures.
  command  seq 12  ... pytest tests/test_api.py  ->  pass (exit 0)
           | ============================== 4 passed in 0.00s ===========
  rerun    new_failures  3 new failures: tests/test_rounding.py::test_round_money_negative,
                         tests/test_rounding.py::test_round_money_two_places,
                         tests/test_service.py::test_round_money_half_up
           reproduce: entire graph verify --repo <data>/wt/458bb14287ec/head --test "..." --pre-edit-baseline ...

0 corroborated   1 impeached   0 uncorroborated   0 unverifiable   0 unrequested
```

Exit codes verified: 2 with `--fail-on impeached` when a row is impeached, 0
when none is, 0 with no `--fail-on`, 1 for an unresolvable checkpoint.

### What turned out to be wrong in the docs

Recorded here because the docs are the authority and these are corrections to
them, not new decisions. The first six are already written into
`docs/ARCHITECTURE.md`.

1. **Checkpoint ids are 26-character ULIDs, not 12 hex characters.** The
   Resolve section said 12-hex. Crockford base32 includes letters outside
   `a-f`, so a hex assumption would reject every real id.
2. **The trailer is the only checkpoint-to-commit link.** The design called it
   the fallback and expected `checkpoint list --json` to expose the sha.
3. **`graph verify` needs per-test ids, and every document asks for
   `pytest -q`.** With `-q` or bare `pytest` the parser reports "output format
   not recognised" and returns `exit-code-only` with an empty results map.
   `-v`, or `-q --tb=no -rA`, gets 18 individual results. The PRD's demo, the
   architecture's data flow and the frontend spec's install snippet all use the
   degraded form.
4. **`graph verify` exits 0 even when it reports a regression.** The exit code
   carries no verdict; only the text does.
5. **The transcript record vocabulary is different.** Observed types are
   `user`, `assistant`, `attachment`, `queue-operation`, `last-prompt`. There
   is no `system` or `summary` record, and assistant content can include
   `thinking` blocks. Attachments are harness bookkeeping and carry no file
   content.
6. **Tool `file_path` inputs are absolute**, while Graph and
   `explain --json` report repository-relative paths.
7. **`graph impact` callee resolution can cross languages.** Asked about the
   Python `compute_total`, it resolved the builtin `sum` to a Go function in
   the host repository. Callers were correct, so no verdict is affected, but
   nothing should be built on callees.
8. **The PRD's demo numbers are illustrative, not real.** It describes two new
   failures in `tests/test_service.py`; the real regression is three, because
   `test_round_money_two_places` also turns on a halfway value. The demo script
   in the PRD should be updated to the real figures before it is shown.
9. **`entire impeach HEAD` cannot work for this build's own commits.** A fresh
   clone carries no git hooks, so the session that wrote Impeach is not itself
   checkpointed and its commits carry no trailer. Phase 11 asks for Impeach to
   be run on the checkpoint of the session that built it; that will only work
   for sessions started after `entire configure --force`, so a later phase of
   the build needs its own captured session to audit.

### Two things deliberately not done

- **No `.impeach.json` at the repository root.** The security policy wants the
  test command to come from a flag or a committed file, and the loader reads
  the repository root, but the build may not add files outside `impeach/`. A
  template sits at `fixtures/app/.impeach.json` for a user to copy up. Until
  then the demo passes `--test` explicitly, which is why the demo command is
  self-contained enough to build its own virtualenv.
- **No `--setup` flag.** Setup reaches `graph verify` from `.impeach.json`
  only, because the documented flag surface is fixed and the self-contained
  `--test` command covers the demo.

### State of the four boundaries

- **Checkpoint reader**, `internal/checkpoint`: complete for v1. Resolve by
  trailer or ULID, metadata envelope, transcript fetch with session index.
- **Extractors**, `internal/claims`: execution family only. Structural, safety
  and reading patterns are phase 6. The model extractor is phase 9.
- **Verifiers**, `internal/verify`: execution only. The other three verifiers
  and the unrequested detector are phase 6.
- **Renderers**, `internal/report`: the Report struct and the table. JSON is
  phase 7, HTML phase 8.

`impeach record` and the replaying fixture scenarios are phase 7. The
`Recording` and `Fake` halves of the Runner already exist and are tested
round-trip, so that phase is wiring rather than design.

### Environment notes a fresh session needs

- `entire` is built from source at the repository root and symlinked into
  `/opt/homebrew/bin`, which is on the login PATH. `~/.local/bin` is not, and
  every Entire hook guards on `command -v entire`, so a symlink there makes
  the hooks silently do nothing. That trap cost the first probe attempt.
- `entire-impeach` is installed at
  `~/.local/share/entire/plugins/bin/entire-impeach`. Rebuild it there after
  any change, or `entire impeach` runs a stale binary.
- Pushing is disabled deliberately: `origin` points at upstream
  `entireio/cli` with its push URL set to `DISABLED-no-push`, session push is
  off, telemetry is off. Commits are authored as
  `Mallika <218808583+snowhiteohno@users.noreply.github.com>`, set
  repository-local only.
- The fixture app's virtualenv is gitignored. `sh scripts/setup.sh` rebuilds
  it, and the demo `--test` command rebuilds it inside a worktree on its own.

## End of phase 6, the pre-noon stop

### Current intent

Ship a clean, honest phase 6: all four claim families verified, the
unrequested detector working, and a demo table on a real captured checkpoint.
Phases 7 to 11 are cut. The landing page is cut. Phase 11, running Impeach on
its own build session, is cut for a reason recorded below and in
BUILDATHON.md, not attempted.

### Architecture, unchanged

Four boundaries, each an interface with one default implementation: checkpoint
reader, claim extractors, verifiers, report renderers. One `Runner` for every
external process, with no working-directory field so a recorded call is fully
described by name and argv. Deterministic core, offline by default, model
extractor opt-in only. Missing data is a state, never an error. Impeach never
executes a command found in a transcript.

### What is done

- All four verifiers: execution, structural, safety, reading. Each returns one
  of four verdicts with typed evidence and reason codes.
- The unrequested detector, row type five. Added and signature-changed symbols
  only; body-only changes are never candidates. It reports the prompt tokens
  it searched, so a reader can see why a match failed instead of taking the
  flag on trust. Production symbols sort above tests.
- The safety verifier asks the right question per claim subtype. This is a
  real distinction, not a refinement: containment claims ("no other callers")
  are contradicted by callers, compatibility claims ("no behaviour change")
  are not, and only a signature change under live callers contradicts those.
- 224 Go tests, green. 22 of 25 pytest tests pass; the 3 failures are the
  documented seeded ones from banker's rounding.
- A second demo checkpoint, `01M1TJCCYXR7ZZTK1H8167G249`, from a real captured
  session that added an order-level discount and renamed `line_subtotal`.

### Four false positives, found and fixed

Phase 6's first run against the real checkpoint produced five rows, four of
them wrong. Recorded in full because it is the most useful thing this phase
produced:

1. Two markdown section headings were read as claims. The agent wrote
   `**Other callers, and whether I reviewed them**` as a heading, and the
   reading verifier corroborated it. A wholly emphasized line is now treated
   as a heading and skipped, decided before the emphasis markers are stripped
   because stripping them destroys the only evidence that it was a heading.
2. A lead-in clause ending in a colon was read as a structural claim and
   impeached for `not-in-diff`. A clause ending in a colon introduces what
   follows and asserts nothing.
3. The safety verifier impeached "a plain rename with no behaviour change"
   for `callers-exist`. That is a compatibility claim, and having callers says
   nothing about behaviour. Split into containment and compatibility.

The pattern holds from phase 5: every false positive was found by pointing the
tool at real data, never by reading the code. Unit tests written from the
design cannot find these, because the design does not know that agents write
in markdown headings.

### What is not done

- No JSON or HTML report. The `Report` struct both would render from exists.
- No `impeach record` or committed replay scenarios. The `Recording` and
  replaying `Fake` halves exist and are tested round trip; this is wiring.
- No `--session`, no `--model` extractor, no second adapter.
- No landing page. Cut deliberately.
- No fork, no push, no mirror. Blocked on credentials, see below.

### Open risks

1. **No fork, no mirror, no push. This is the top risk and it is not
   technical.** `gh` is authenticated only as `snowhiteohnopt2`, the
   work-linked account, and git pushes to github.com are routed through
   `gh auth git-credential`, so both the fork and the push would use the wrong
   identity. Entire itself is correctly authenticated as `snowhiteohno`
   (`github/218808583`), and the commit author is set repo-local to match.
   `entire repo mirror create` takes a GitHub URL and registers server-side,
   so it does **not** require an `entire repo clone`; that feared fallback is
   closed. The whole submission chain is waiting on one `gh auth login` as
   `snowhiteohno`, most reliably via a classic PAT with `repo` scope.
2. **The demo table has no impeached row, and the reason is that the agent was
   honest.** Coverage on `01M1TJCCYXR7ZZTK1H8167G249` is 2 corroborated,
   1 uncorroborated, 1 unverifiable, 3 unrequested. The session scoped every
   claim accurately, said plainly which tests it had not run, and flagged its
   own verification gap. There is nothing to impeach. The earlier checkpoint
   `01M1TET4N33VMY0DTHNZKV5HT9` does produce an impeached row, by
   `contradicted-rerun`. So all four verdicts plus unrequested are
   demonstrable, but across two checkpoints rather than one table. Getting all
   five into one table needs either a seeded recorded scenario, which is what
   `fixtures/recorded/<scenario>/` was for in the cut phase 7, or an agent
   session that overclaims, which cannot be arranged honestly by instructing
   one to lie. This is worth stating as a finding rather than hiding: the
   tool's headline row is hardest to produce exactly when the agent under
   audit is careful.
3. **Only two commits carry checkpoints.** The build session predates the git
   hooks. Both demo checkpoints come from `claude -p` subsessions, which are
   captured. Phase 11 therefore cannot run and is not attempted.
4. **The unrequested rows are currently three added test functions**, which is
   noise rather than signal. They are correctly flagged by the specification,
   since the prompt asked for tests generically without naming them, and they
   are tagged `(test)` and sorted last. A production symbol nobody asked for
   would be the interesting case and the fixture does not currently produce
   one.
5. **`graph verify` needs a self-bootstrapping test command** in a detached
   worktree, because the fixture virtualenv is gitignored and absent there.
   Without it the baseline records exit 127 and the rerun degrades to
   `skipped`. The demo command builds the venv if missing; the plain
   `--test 'pytest -q --tb=no -rA'` form reports `skipped` honestly instead of
   claiming a pass.

## Fork, push and mirror, resolved

Risk 1 from the phase 6 stop is closed.

- `gh` re-authenticated as `snowhiteohno` with a classic PAT. A fine-grained
  PAT was tried first and returned HTTP 403 on the fork: fine-grained tokens
  cannot fork a repository owned by someone else. Classic with `repo` scope
  works for both the fork and the push.
- Fork: <https://github.com/snowhiteohno/cli>, parent `entireio/cli`, public.
- Remotes rearranged so nothing can reach upstream by accident: `origin` is
  the fork and is pushable, `upstream` is `entireio/cli` with its push URL set
  to `DISABLED-no-push`. The author identity stays repository-local and the
  global company config is untouched.
- `main` pushed, `3dbdf8b83..ab8ba0eee`.
- Checkpoint sync re-enabled in `.entire/settings.local.json`, which is
  gitignored. The pre-push hook then reported "Pushing 3 checkpoint ref(s) to
  origin" and `entire status` stopped listing anything pending. Verified
  against the remote with `git ls-remote origin 'refs/entire/*'`, which returns
  exactly the three.
- Mirror: `entire://aws-us-east-2.entire.io/gh/snowhiteohno/cli`, ID
  `01M1TKHC7813MNVG5Y543V7P83`, status ready. `entire repo mirror create` took
  a GitHub URL and registered server-side, so the feared requirement for an
  `entire repo clone` never applied.

Two notes worth keeping:

- `entire repo mirror get` wants `<owner>/<repo>`, a mirror ULID or an
  `entire://` clone URL. It rejects a `github.com/...` URL, which is the form
  `mirror create` accepts. Easy to trip over.
- The fork is public, so the checkpoint refs on it are public. That is Entire's
  default storage behaviour, not an Impeach decision, and it is why the
  committed fixtures are scrubbed of paths, user names and author identity.

## Phase 7, and the push that GitHub rejected

Done: JSON report at `--out/impeach.json` following the documented schema, the
credential scrubber the security policy required, `impeach record` and
`--replay` as two implementations of the one `Runner`, two recorded scenarios
from real captured sessions, and nine end-to-end tests that drive the real
entry point offline. 245 Go tests.

### The bug that would have made the fixtures worthless

Scrubbing recorded argv is what makes a scenario portable, and it is also what
broke replay on the very first git call: a recording rewritten to `<repo>` can
never match a live call still carrying the real absolute path. Recording and
replay have to be symmetric. `Fake` gained a `Normalize` hook applying the
identical transformation to each live lookup key. Without it the scenarios
would have been write-only, which is the kind of defect that only shows up if
you actually replay one.

A second, smaller version of the same lesson: the scenarios were recorded
while running as an installed plugin, so their worktree paths sit under
`ENTIRE_PLUGIN_DATA_DIR`. The replay tests pin that variable, because with it
unset `DataDir()` falls back to `~/.cache/impeach` and every worktree call
misses.

### GitHub push protection rejected the push, over our own test fixture

`git push` was declined with `GH013: Repository rule violations found`. The
cause was `internal/report/scrub_test.go:19`, a plausible-looking fake Slack
token used to test the scrubber. GitHub cannot tell a test fixture from a
leak, and it is right not to try.

Fixed properly rather than by clicking the unblock link: every sample in the
scrubber tests is now assembled from parts at run time, so the pattern is
still exercised and no complete token literal exists in the source. Checked
the whole tree afterwards for scannable shapes; none remain.

The offending commit had never reached `main`, since only the checkpoint refs
had gone through, so the tip was amended rather than followed by a fix
commit. That is not a squash and nothing remote depended on the old sha. The
`Entire-Checkpoint` trailer survives an amend because it lives in the message,
so checkpoint `01M1TMG4W06DK3HM424MDAZBY1` still resolves, now to
`4dfb799`. Worth noting the small inconsistency this leaves: the captured
session's own transcript records it committing `bc5cc09`, a sha that no longer
exists.

There is an irony worth keeping. A tool built to catch unverified claims was
blocked from shipping by an automated check that did not believe its
claim that a string was only a test fixture. The check was correct to be
suspicious and the fix was to stop making the claim necessary.

### Scenarios deliberately absent

Three of the five the architecture lists, `stale`, `scope` and `safety`, are
not recorded, because both sessions available were accurate. Those verdicts
are pinned by unit tests against hand-built records in `internal/verify`.
Recording them would mean instructing an agent to make a false statement, and
a fabricated impeachment in the fixtures of a tool about false claims is not a
trade worth making. `fixtures/recorded/README.md` says so plainly.

## Phase 9: session mode, the model extractor, adapter selection

Done: the opt-in `--model` extractor, `--session`, and an explicit adapter
registry. 285 Go tests.

### What the model extractor is allowed to see

This is the only part of Impeach that can send anything off the machine, so
the boundary is narrow and tested directly rather than described:

- One assistant turn at a time. Never a prompt, never a tool output, never a
  file. A test asserts on the actual stdin bytes and fails if a planted
  `SECRET USER PROMPT` or `SECRET TOOL OUTPUT` appears.
- `--model-turns`, default 20, caps how many turns are sent, so enabling the
  flag on a long session cannot quietly become a large amount of outbound
  text. Dropping turns is reported, not silent. This flag is an addition to
  the documented surface and is recorded in `docs/PRD.md`.
- The command runs as argv through the `Runner`. There is no shell, so the
  command line is tokenized in `splitCommand` and an unbalanced quote is an
  error rather than a guess.
- The report header names the exact command, and every model claim is marked
  in the table and the HTML as `family (model)`, because the PRD requires
  model-generated claims to be identifiable. A reader has to be able to tell
  which rows a third party suggested.

What the model is not allowed to decide: anything. It produces the same
`Claim` type the pattern library does and goes through the same verifiers, so
it can suggest what to check but never what the answer is. Even the execution
kind and the safety subtype are decided by the deterministic pattern set
rather than taken from the model's reply, because that library already encodes
those distinctions and a model's opinion about them is not evidence.

Unusable output is dropped with a warning per turn: prose, broken JSON, an
object instead of a list, an unknown family. A fenced code block is tolerated,
because models emit them constantly. A failing command is a dropped turn, not
a failed audit, so the deterministic path always survives it.

### Session mode

`--session` resolves the reference, takes its session id, lists that session's
checkpoints with `entire checkpoint list --json --session <id>`, and loops the
per-checkpoint pipeline oldest first. The CLI lists newest first; a session
reads better in the order the work happened.

One decision worth recording: the prompt corpus for unrequested detection is
the union across the whole session, gathered in a first pass before any
verdict is decided. Scoping it per checkpoint would flag most of a multi-step
session, because a symbol asked for in the first checkpoint and delivered in
the third would look unrequested in the third. A checkpoint that fails to
resolve or whose transcript cannot be read contributes nothing and does not
end the run; the remaining sections are still worth having. `--fail-on` is
sticky across the session, so one impeachment anywhere makes the exit code 2.

### Adapter selection

The registry is explicit now. `--adapter auto` lets the transcript decide,
and an explicit name skips detection, which matters when a transcript is
recognised by more than one adapter or by none. With `auto` and nothing
recognisable the error names what was tried, and the caller turns that into
unverifiable rows rather than a crash, because a transcript nobody can read is
a missing channel and missing data is a state.

Only `claude-code` exists. A second agent is one entry here and nothing else,
which is the claim the four-boundary design has been making all along and is
now the smallest it will ever be to test.

## Final graph diff review

`entire graph diff --base 3dbdf8b --head HEAD --json --max-seconds 0`, the
reviewable half of phase 11. The self-audit half stays cut: the build session
predates the hooks, so Impeach cannot resolve a checkpoint for its own
construction.

Result: 122 files touched, 115 parsed, 1392 entity changes, and every single
one is `added`. No removals, renames, signature changes or body changes, so
the build is purely additive with respect to the fork.

Checked against git rather than believed:

- git: one pre-existing file modified, `README.md`, 4 insertions, 0 deletions.
  Everything else is `A`.
- graph: that same file as status `M` with one change, `added section
  'Impeach'`.

The 7 unparsed files (`impeach/go.mod`, the HTML template, 5 testdata
fixtures) match the 7 `W_UNSUPPORTED_FILE` warnings exactly, and 122 minus 115
is 7, so the warnings account for the whole gap. Graph reports what it could
not read instead of reporting it as empty, which is the same contract Impeach
offers its own users. One consequence worth stating: Graph cannot parse
`report.html.tmpl`, so a structural claim about the HTML report would be
`unverifiable` under Impeach's own rules.

Symbol counts cross-checked against the source, and both deltas resolve in
Graph's favour:

- functions 392 against 392, exact.
- methods 93 against 85 from `grep '^func ('`. The 8 missing are the method
  declarations inside the four boundary interfaces: `Runner.Run`,
  `Adapter.Name`/`Detect`/`Parse`, `Extractor.Name`/`Extract`,
  `Verifier.Family`/`Verify`. A line-anchored grep cannot see interface
  methods. Graph's method count exceeds the naive count by exactly the size of
  the architecture's interface surface, which is a pleasing way to be wrong.
- types 84 against 83. The extra is `pending`, declared inside a function in
  the Claude Code adapter and therefore indented past `^type`.

Test side at the same commit: `go vet` clean, `gofmt` clean, 285 tests across
8 packages, all passing under `-race`, and pytest at 22 passed with the 3
documented seeded failures.

Written into `BUILDATHON.md` under the Graph findings section, with a note on
what the review does not prove: it shows the build added what it claims and
disturbed nothing else, and says nothing about verdict correctness on
transcripts nobody has written yet. The four false positives found in phases
5, 6 and 8 are the evidence that reading the code is not how those get caught.

## The README, and three errors in its own instructions

Wrote `impeach/README.md`: who it is for in the first paragraph, install,
reproduce, what gets checked, limitations, security posture, disclosure.
The repository README pointer now points at it and is still one line.

Every command was run from a clean clone of the fork before being written
down, which turned out to matter. Three of the instructions were wrong, and
none of the three would have been caught by reading:

1. **A plain `git clone` brings no checkpoints.** The refs live under
   `refs/entire/*`, outside git's default refspec, so a fresh clone had 0 of
   the 8 that exist on the remote. `entire impeach` then correctly reports
   that no commit carries the checkpoint, which is accurate and completely
   baffling. The fix is one line, `git fetch origin
   'refs/entire/*:refs/entire/*'`, and it is now step one in both documents.
2. **`entire plugin dir` does not exist.** The install snippet already
   committed in `BUILDATHON.md` used it to locate the managed directory. It is
   the obvious guess and it is not a command.
3. **`entire plugin install <path>` symlinks rather than copies.** Found by
   building, installing, then deleting the local binary as cleanup: the
   managed entry became a broken link and `entire impeach` went back to
   "unknown command". The README now builds straight into
   `~/.local/share/entire/plugins/bin`, which the CLI prepends to PATH at
   startup, so there is no link to break. `/entire-impeach` is gitignored for
   anyone who uses the `plugin install` route anyway.

Also verified from that clean clone: `go test ./...` passes all 8 packages
offline, because the replay scenarios need no Entire, git or agent, and the
demo audit produces the impeached row and exit 2 under `--fail-on impeached`.
The throwaway clone was deleted and the plugin reinstalled from the working
tree.

There is a pattern by now worth naming. Every serious defect in this build
came from running the thing rather than reading it: the four verdict false
positives, the write-only recorded fixtures, the unscrubbed `commands_run`,
the prompt corpus in the report, and now three broken instructions in the
install path. Reading found the typos.

## Constraint change: privacy and completeness

Announced as a curveball. Playbook consulted first, per the process rule. The
relevant entry in `docs/ARCHITECTURE.md` is already close to this: "Redacted or
missing transcript: unverifiable rows with the channel named; unrequested and
structural rows still run from Graph and prompts if prompts survive." So the
change lands where the playbook says it lands. It is not a redesign.

### Step 0: impact analysis, recorded before any edit

Three entry points, disambiguated by file and line because each name is
ambiguous and Graph returns the definition list rather than guessing.

**Extractor boundary**, `Extractor.Extract` at `internal/claims/claim.go:137`.
Zero callers, because dispatch through an interface is dynamic. 4 type
consumers, which are the real affected paths:

```
Claim   internal/claims/claim.go:79        RETURNS_TYPE, USES_TYPE
Event   internal/transcript/event.go:85    PARAM_TYPE, USES_TYPE
```

Concrete implementations: `Pattern.Extract` at `patterns.go:168` and
`Model.Extract` at `model.go:69`.

**Verifier boundary**, `Verifier.Verify` at `internal/verify/verdict.go:90`.
Zero callers, 5 type consumers:

```
Claim    internal/claims/claim.go:79    PARAM_TYPE, USES_TYPE
Verdict  internal/verify/verdict.go:74  RETURNS_TYPE, USES_TYPE
Record   internal/verify/verdict.go:96  USES_TYPE
```

`Record` is used by all four verifiers, which makes it the single insertion
point for completeness gating. Concrete implementations at
`execution.go:79`, `structural.go:26`, `safety.go:22`, `reading.go:23`.

**Renderers.** `Table` at `internal/report/table.go:30` has 7 callers, 6
direct: `emit` at `main.go:388`, five table tests, and `auditOne` at
`main.go:371` transitively. `ToJSON` has 15 callers, of which the non-test
ones are `emit` at `main.go:403`, `HTML` at `html.go:104` and `WriteJSON`.
`HTML` has 21 callers, non-test being `emit` at `main.go:414` and `WriteHTML`.
Both take `Report` at `report.go:16` as PARAM_TYPE.

**Conclusion from the impact data.** Every non-test path converges on three
functions in `cmd/entire-impeach/main.go`: `auditOne`, `assemble` and `emit`.
Nothing outside `internal/{transcript,verify,report}` and that one file needs
to change. No caller outside the module exists, because the module has no
importers. That is the affected-path evidence, and it is why this is a
contained change rather than a rewrite.

### Already compliant, cited not rebuilt

`docs/SECURITY_AND_ACCESS.md` already commits to all of the following, and
none of it is being reimplemented:

- No network on the default path. "The default path makes no network calls."
  `--model` is the only outbound path and is opt in.
- Transcripts never written to `--out`. "Never write a transcript, in whole or
  in part, to `--out`. Evidence excerpts are bounded and scrubbed." Enforced in
  phase 7 and covered by `TestReplayJSONCarriesNoRawTranscript`.
- Bounded scrubbed excerpts. 400 characters per evidence item plus a
  second-pass credential scrub, built in phase 7 and applied to the table, the
  JSON and the HTML.
- Unverifiable as a state, never an error. The invariant the whole tool rests
  on, present since phase 5.

Prompts are also already kept out of reports as full text, tightened in
phase 8 to a corpus size plus the tokens actually searched for.

### Plan, written before editing

Three changes, in the boundary the playbook names.

1. **Sensitive mode is a refusal.** `--sensitive`, and `"sensitive": true` in
   `.impeach.json`. In that mode `--model` is refused: non-zero exit, clear
   message, no call attempted. Not ignored and not warned about, because a
   warning still sends the text. The report header states the mode verbatim.
   Lands in flag validation and `buildRunner`, ahead of any extractor.

2. **Completeness gates the verdict.** Every evidence channel carries an
   explicit state: present, partial, redacted, absent. `Corroborated` is
   reachable only from a `present` channel. A `partial` or `redacted` channel
   can still impeach, because a contradiction survives redaction, but it can
   never corroborate; it degrades to `unverifiable` with the channel named.
   Asymmetric deliberately: absence of evidence is not evidence of honesty.
   Lands on `verify.Record`, which the impact data shows is used by all four
   verifiers, so one field reaches all of them.

3. **A context ledger on the run.** Header line and a JSON field listing each
   channel and its state, plus one sentence when anything is short of present.
   Above the rows in the table, above the summary strip in the HTML. `--fail-on
   incomplete` so CI can gate on partial context. No new exit code; 0, 1 and 2
   are settled.

### Blocked on one input

The instruction says to test with an attached fixture. **No fixture is
attached**, and nothing new appears in the tree or on the Desktop. Everything
else proceeds. A redacted scenario is synthesized here as a stand-in, clearly
labelled as synthetic in `fixtures/recorded/`, so the supplied one can replace
it without touching the tests. The three required assertions do not depend on
which fixture is used.

### What changed, and why the result can be trusted

Implemented in the boundaries the impact analysis named. 311 tests, vet clean,
race clean, pytest at 22 passed with the 3 documented seeded failures.

**Channel states**, `internal/transcript/channels.go`. Four states: present,
partial, redacted, absent. Redaction and truncation are detected from markers
left behind, and the adapter never tries to reconstruct what was removed. The
precedence is absent, then redacted, then partial: a channel with no events
cannot be partially anything, and a removed secret is a stronger statement
about what a reader cannot see than a shortened output is. Records the adapter
could not read, and subagent activity it does not examine, make otherwise
present channels partial, because the dropped record could have been any kind
of event.

**The gate**, on `verify.Record`, which the impact data showed is used by all
four verifiers, so one field reached all of them. Corroborated is reachable
only from a present channel.

Two decisions inside the gate worth defending:

- **Uncorroborated is gated too**, not only corroborated. Uncorroborated means
  the channel was readable and held nothing either way. On a redacted channel
  that is simply the wrong statement: it was not readable, so its silence
  proves nothing. Reporting it as uncorroborated would quietly imply a search
  that never happened.
- **Graph is its own channel.** The first implementation gated structural and
  safety on the edits channel, which was wrong. Graph reads the code at the
  commit, not the transcript, so a redacted transcript leaves a Graph-derived
  verdict intact. What should gate those is Graph failing to answer or failing
  to parse the file, and that is what `ChannelGraph` now carries, set by the
  record layer because only it knows whether Graph answered.

**Sensitive mode** is a refusal at the entry point, before `buildRunner` and
before any extractor exists. `--sensitive` or `"sensitive": true` in a
committed `.impeach.json`, so the repository itself can carry the constraint.
A test asserts zero calls were made, because refusing after the first turn has
been sent would be theatre.

**The ledger** is on the run, reported whether or not anything is missing: a
reader needs to know what the verdicts were computed from, not only what they
were. Above the rows in the table, above the summary strip in the HTML, and a
`channels` map with per-channel reasons in the JSON alongside
`context_complete`, `context_note` and `claims_gated_by_channel`.
`--fail-on incomplete` reuses exit 2; 0, 1 and 2 stay settled.

Rows gated by a channel now expand in the terminal like impeached rows do.
Those are the ones most likely to be misread: "unverifiable" alone looks like
the tool shrugging, when it means the evidence was removed and the row is
saying so.

### A real bug this work exposed

The custom-runner fallback in the execution verifier matched transcript
commands against the first word of the configured test command. For a compound
command like `cd app && pytest -q` that word is `cd`, which appears in almost
every shell command, so an unrelated failing `git status` was being read as a
failing test run and reported as `contradicted-output`. It showed up in the
redacted scenario because redaction turned that git failure into an
unparseable output while leaving `is_error` true.

Fixed two ways: the fallback is skipped entirely when the built-in patterns
already recognise the runner, since they are the precise answer; and when it
is needed, the token is chosen by skipping shell builtins, with an
unmatchable sentinel when nothing distinctive remains. A loose match here
manufactures evidence, which is worse than missing a runner. Three tests pin
it, including one that a weak test command must match nothing.

That bug was a false positive of exactly the kind this build keeps producing
and catching: a verdict invented from an unrelated command. It is the fifth
one, and like the others it surfaced only from running the tool on real data.

### Why the revised behaviour can be trusted

The asymmetry is asserted from both directions on the same scenario, which is
the strongest form available: run `redacted-toollog` with `--no-rerun` and the
claim that its own command output would have supported becomes unverifiable
with `channel-incomplete`; run it with the rerun and the same claim is
impeached by `contradicted-rerun`. Redaction cannot buy a corroboration and
cannot escape a contradiction. The ledger names the compromised channel and
reports the other four as present, so it reads as a statement rather than a
blanket warning.

### Still outstanding

The supplied fixture never arrived, so `redacted-toollog` is derived from the
real `rerun-regression` recording with redaction applied, and says so in its
`PROVENANCE.md`. A real redacted checkpoint can replace that directory without
touching a line of the tests, because the assertions are about behaviour.

### Curveball section written, and the HTML gap it exposed

`BUILDATHON.md` Curveball section filled: the constraint, why the
four-boundary design made it cheap, what was already compliant and cited
rather than rebuilt, the three changes, the two judgement calls that went
against the first implementation, how the asymmetry is proved from both
directions, the fifth false positive, and the fixture that never arrived. All
nine required sections are now present in the guide's fixed order.

Writing it surfaced a gap in the change itself. The HTML template gained the
context section but was never rendered and looked at, and with no ledger it
emitted a bare full stop. Fixed to omit the section entirely in that case, and
four HTML tests added: the ledger names the redacted channel, intact channels
are reported too so it reads as a statement rather than a blanket warning, the
sentence sits above the summary strip, and sensitive mode appears in the
header. Verified by rendering the redacted report and looking at it.

That is now the sixth defect in this build found by running or viewing the
thing rather than reading it.

### The root .impeach.json, and the gap adding it exposed

Added `.impeach.json` at the repository root with the test command, a setup
command that builds the fixture virtualenv, and `"sensitive": false`. The
demo is now just `entire impeach 01M1TET4N33VMY0DTHNZKV5HT9 --repo .`, with no
long `--test` string to copy.

`sensitive` is false here on purpose. This fork is public and its transcripts
are published deliberately, so there is nothing to protect, and setting it
true would only stop a reviewer from trying `--model`. Both directions were
checked end to end: with it false a model extractor runs and its claims are
marked `execution (model)`; flipped true, `--model` is refused with a non-zero
exit and the mode is stated in the header even when `--model` is absent.

Adding the file broke two replay tests, and the reason was a real design gap
rather than a test problem. `--test` could be overridden on the command line
but `setup` could only come from the config, so a committed setup command
changed the `graph verify` argv, missed the recorded calls, and silently
turned the rerun into `skipped`. A committed config that cannot be overridden
is a config that can silently change what a run does.

Fixed by adding `--setup`, using `flag.Visit` so an explicitly empty value
means "no setup" and is distinguishable from not passing the flag at all. The
replay harness now pins `--setup ""`, which makes those tests hermetic against
whatever the repository happens to commit. Recorded in `docs/PRD.md`.

Worth noting how this was caught: `go test ./...` reported `ok (cached)` and
would have gone on doing so. It only surfaced under `-count=1`. Cached test
results are themselves a claim about code that has not been re-run, which is a
fitting thing for this particular tool to have been bitten by.

## The landing page

Built `site/` per FRONTEND_SPEC surface 2: title block, the lead impeachment
live, what a verdict means, what gets checked, install, reproduce, the full
sample report in a sandboxed iframe, limitations, footer. One column, 880px,
the report's own tokens. No external requests, no fonts, no analytics, and no
script element at all, which the tests assert.

It is generated by `internal/report/gen-site`, which takes the `--out`
directory of a real run and does three things: copies
`internal/report/report.css` to `site/style.css`, scrubs machine paths out of
the sample, and lifts the `<section id="lead">` block out of the sample report
**verbatim** into the page.

Lifting rather than rewriting is the whole design. The spec's QA item asks
that "the landing page's hero matches the sample report byte for byte after
gen-site", and lifting makes that true by construction instead of by
discipline. A test asserts it, so hand-editing `site/index.html` fails the
suite. Three more assert no absolute paths anywhere in the committed site, no
external resource references, and that `site/style.css` has not drifted from
`report.css`.

The sample is the `rerun-regression` scenario replayed offline, chosen because
it is the one that produces an impeached row; a sample without one would leave
the hero reading "No claim was impeached." Its paths are scrubbed, which
matters: the raw report carried 10 absolute paths in the JSON and 20 in the
HTML, from the reproduce lines. A locally generated report shows real paths on
purpose so a reader can copy a command; a committed one must not, because it
would publish a home directory to anyone who opens the page. That was the gap
flagged back in phase 8 and it is now closed for the committed artifact.

### Publishing is blocked, and it is not a technical problem

The spec says to serve the page from GitHub Pages with folder
`/impeach/site`. That is not possible: a Pages branch deployment offers only
`/` or `/docs` as the source folder, and `/docs` in this fork already holds
the upstream CLI's own documentation. `.github/workflows` also already exists
and belongs to upstream.

So publishing needs one of three things, all of which touch files outside
`impeach/`: a Pages workflow under `.github/workflows`, a copy of the site at
the repository root or in `/docs`, or a separate repository. None was chosen
unilaterally. Documented in `impeach/README.md` with the local serve command
in the meantime.

This is a real error in FRONTEND_SPEC.md rather than a missing step, and it is
the tenth thing in `docs/` that turned out not to survive contact with the
tools.

### A scrubber that corrupted the thing it was protecting

Checking that `gen-site` was reproducible found a data-corruption bug in it,
which is worth recording because the failure mode is the nastiest kind:
silent, in a file about to be committed and published.

The path scrubber replaced the raw `--repo` argument as given, as well as its
absolute form. Running `gen-site --repo ..` therefore rewrote every literal
`..` in the report, and pytest's progress line `tests/test_api.py ....`
became `tests/test_api.py <repo><repo>`. Real evidence, destroyed, in the
sample report the landing page shows to everyone.

A replacement now has to be an absolute path of at least eight characters to
be applied at all, pairs are sorted longest first so a home directory nested
inside a repository path cannot leave a half-rewritten string, and the user
name is only replaced as a whole path segment rather than as a bare substring.
Five tests pin it, including one that asserts the exact pytest progress line
survives every relative `--repo` form.

Determinism is now verified both ways: regenerating from an absolute path and
from `..` both produce a byte-identical site.

The lesson is the same one this build keeps relearning, in a new place: the
bug was not found by reading the scrubber, which looked obviously correct, but
by running it twice and comparing. A scrubber that damages the output it is
protecting is worse than no scrubber, and only a second run shows it.

## The Pages workflow

`.github/workflows/impeach-pages.yml`, a new file alongside the fourteen
upstream workflows, none of which is touched. This is the fourth thing outside
`impeach/`, after `BUILDATHON.md`, the README pointer and `.impeach.json`, and
like those it was asked for.

It publishes the committed `impeach/site` rather than building it in the
runner, because generating the page needs a real audit, which needs the Entire
CLI, the graph plugin and a checkpoint, none of which exist there.

The interesting part is that it refuses to publish a drifted page. Before
deploying it runs the site tests, then the whole suite offline. If someone
hand-edits `impeach/site`, or changes the report's stylesheet without
regenerating, the hero stops matching the sample report byte for byte and
nothing ships. Verified by tampering with the hero and watching the check
fail, then restoring it: a gate that has never been seen to fail is not known
to be a gate.

Two details worth keeping. The path filter includes
`impeach/internal/report/**` as well as `impeach/site/**`, so a change to the
report that was not regenerated triggers the check rather than sitting
unnoticed until the next site edit. And the test steps use `-count=1`, because
a cached pass is a claim about code that was not re-run, which this repository
has already been bitten by once.

Actions are enabled on the fork and the token carries the `workflow` scope, so
the file pushes and runs. Pages itself is not yet configured: the site has no
URL until the repository's Pages source is set to GitHub Actions, which is a
publishing act on a public repository and was left for the owner to trigger.

### Pages enabled, and the site is live

<https://snowhiteohno.github.io/cli/>

Enabled with `gh api -X POST repos/snowhiteohno/cli/pages -f
build_type=workflow`, which sets the source to GitHub Actions and sidesteps
the folder restriction that made the frontend spec's `/impeach/site`
impossible.

The first run, triggered by the push that added the workflow, failed with
"Get Pages site failed. Please verify that the repository has Pages enabled":
the workflow existed before Pages did. Worth noting where it failed, though.
It failed at `configure-pages`, which runs after the test steps, so the drift
gate and the full suite had already passed. The workflow was correct; the
repository was not configured yet.

Verified after deployment rather than assumed:

- All four paths serve 200: the index, the sample report, the stylesheet and
  the sample JSON.
- The served page carries no `/Users/` path, so the scrubber held.
- **The live hero is byte-identical to the committed sample report's hero**,
  862 bytes, checked by fetching the deployed page and diffing the section.
  The drift guarantee reaches production, not just the test suite.

One warning fixed on the way: `setup-go` was pointed at `impeach/go.sum`,
which does not exist because the module is standard library only, so every run
complained about unresolvable cache paths. `cache: false` says the same thing
without the noise. The remaining annotation is GitHub deprecating Node 20 in
its own actions, which is not ours to fix without pinning versions that may
not exist yet.

## A false claim in the requirements, found by auditing them

Asked what was left, and checked rather than recalled. The check found the
worst remaining item was in `docs/PRD.md` itself.

The "90-second demo" section described a six-row table with rows for
"No other callers are affected", "Added `test_refund_rounding`", "Verified the
migration path", and an unrequested `_legacy_shim`. None of those exist. The
real artifact produces one impeached row on the first demo checkpoint and
four rows plus three unrequested symbols on the second. The symbols named
were never built.

It was a design sketch that survived into the requirements as though it were a
fact. Two smaller claims in that same section had already been corrected, the
test command and "two new failures", and the surrounding paragraph was not
read closely enough at the time. This is precisely the category of error the
tool reports on, sitting in the document that specifies the tool, which is
worth recording plainly rather than fixing quietly.

Rewritten to describe what the tool actually prints, with the counts and the
exit code verified against a live run afterwards rather than transcribed from
memory. The section now also states what the demo is not: there is no single
table with one row of every verdict, and reaching one needs either a seeded
transcript or an agent told to lie. The correction itself is left in the
document, because deleting the wrong version would remove the evidence that
it was ever wrong.

Also documented the six flags that existed but appeared nowhere in the command
surface: `--record`, `--replay`, `--scrub`, `--repo`, `--keep-worktrees` and
`--version`. A re-run of the audit now reports no drift in either direction,
17 flags documented and 17 implemented.

## The site rebuild, six steps in four commits

Recorded after the fact, which is itself the first thing to note. `HANDOFF.md`
says a fresh session should be able to reconstruct the build from the latest
checkpoint plus this file, and for the rebuild it could not: four commits
landed with the full account in their messages and nothing here. The messages
are good and they are the source for what follows, but a commit message is
found by whoever thinks to look for it, and this file is read first. Written
up now, from the four messages plus the tree, not from memory.

Checkpoints, in order: `01M1TWMC6VVVJZEA4XH2R6PYFP` (step 1),
`01M1TX1WZ1E2YBZ6ZS27R8P97Z` (steps 2 and 3),
`01M1TXDQMT3F7S5B2K91E01MJF` (steps 4 and 5),
`01M1TYA01Q0GTN2AQ61BWTSMSF` (step 6, the QA pass).

Nothing outside `impeach/site/` changed except `internal/report/site_test.go`.
The test count stayed at 327 throughout, because the rebuild added no
behaviour to the tool.

### What it is now

A hand written page, not a generated one. Ten sections, two self hosted
variable fonts at 68KB together (latin subset woff2, already subset because
there is no pyftsubset on this machine), and a hero that is a fragment shader
on a full screen quad in raw WebGL2 with no libraries: procedural paper with
two octaves of value noise and a low frequency fiber field, a Canvas2D record
texture sampled through a noise driven domain warp so the ink reads as
absorbed, and up to four bleed rectangles that tint to the report's impeached
red under the lamp. In ordinary light the paper shows nothing. The context
ledger performs the asymmetry it describes: a ragged edged bar of ink sweeps
the tool log, the state word flips to redacted, and the count falls from three
corroborated to one. The terminal types its command and reveals the verdict
table a row at a time, then exits 2.

Every scene is staged by script and settled in CSS. Without JavaScript the
markup already carries the conclusion: rules drawn, terminal complete, figures
at their real values, ledger at its redacted end state. Reduced motion takes
the same path with the transitions removed.

### Four places the brief met the repository

- **Two figures in the brief were stale.** It asked for 197 Go tests and 18
  pytest. The real numbers are 327 and 25, and the real ones are on the page.
  A false figure in 48px type, on a page about false claims, is the one thing
  this project cannot ship.
- **The em dash in the quoted testimony stays.** It is the agent's own
  wording, it lives in `sample/impeach.json`, and the page is tested for
  matching that file byte for byte. Editing quoted evidence to satisfy a house
  style would be falsifying the evidence. Zero em dashes were authored; the
  only ones are inside quoted material, which is how `testdata/` and
  `fixtures/recorded/` are already treated.
- **`site_test.go` wanted the report's lead block byte for byte**, and that
  file was out of the brief's scope. Rather than work around it, those 862
  bytes became the no JavaScript and screen reader layer, which the brief
  asked for independently. One set of bytes now serves the test, the
  accessibility requirement and the no JS requirement, and the anti drift
  guarantee survives.
- **Two assertions in `site_test.go` were narrowed, with sign off.** Byte
  identity with `report.css` became agreement on every shared token, so the
  page still cannot call impeached green. The outright ban on script elements
  became a ban on scripts from another host. Narrowed to the property that
  matters rather than deleted.

### Nine bugs, every one found by looking at the page

This is the lesson the build keeps relearning, and the rebuild is its clearest
run of evidence. Not one of these was found by reading the code.

- **The shader did not compile at all**, and the page served its fallback
  while looking entirely plausible. The inline script block puts a newline
  before the source, so `#version` landed on line 2 and the whole thing
  compiled as GLSL ES 1.00. Every other reported error was downstream of that
  one. Compile and link failures now warn on the console: a fallback for the
  visitor must not be silence for whoever is working on it.
- **The DOM record was painted over the canvas** at full strength, which
  defeats the concept. It is now visibility hidden while staying in the
  layout, because the shader measures those elements to place the ink.
- **The lamp was mirrored vertically** and the bleed rectangles were in the
  wrong space. `gl_FragCoord.y` counts from the bottom while pointer input and
  the Canvas2D texture are top down. Everything is top down by convention now
  and only the uniform upload converts. It reads as a shader bug when it is a
  coordinate bug.
- **The verdict was invisible without JavaScript**, defaulting to opacity 0.
  The landed state is now the CSS default and script opts into staging it, so
  the most important word on the page does not depend on JS.
- **The hatch was specified to sit under the ink**, which is impossible: the
  ink is opaque and covers the bar. It sits on top with a lightened pattern,
  which is what the report does for unverifiable rows, so the two surfaces
  still agree on what "cannot be relied upon" looks like.
- **The redaction's effect preceded its cause.** The state word and hatch were
  applied as the sweep started, so the finished state showed for half a second
  before the ink that supposedly caused it. They land at 460ms, just under the
  sweep.
- **The no WebGL fallback was illegible**, and this is the worst of the nine.
  It masked the record to a cursor following circle at 18 percent opacity, so
  the evidence was somewhere between unreadable and absent, and the mask
  clipped both lines. The opacity could not be undone either, since a child
  cannot raise its parent's opacity, which is what the code attempted. Gating
  legibility on a moving light is incoherent on the one path with no light to
  move. It now prints the record plainly in the record ink, with the
  contradicting line red and underlined so colour is not carrying the meaning
  alone, and the lamp survives as atmosphere behind the text.
- **Reduced motion had the same flaw** in subtler form: the shader owned the
  record, and a fixed lamp means a fixed cone, which clipped lines wider than
  itself. Reduced motion must not cost the reader information, so a new
  `uRecordOn` uniform drops the ink layer and the record becomes ordinary
  selectable text over the paper.
- **The hint was the third instance of the same mistake.** It read "Move the
  light" on all three paths, including the two with no light to move. The
  markup carries the honest static sentence and script upgrades it only after
  the shader has actually compiled.

The pattern across the last three is one mistake, not three: a visual effect
was allowed to be the only carrier of information, so every path without the
effect lost the information. Worth naming, because it will recur in whatever
the next scene is.

### The QA pass, and what it is honest about

Recorded at step 6 and carried here from that commit rather than re-measured
today. Lighthouse went from 93 performance and 96 accessibility to 99 and 100,
with best practices and SEO at 100. Both accessibility failures were real
contrast defects, and both were fixed without moving a shared token, since the
page and the report are tested for agreeing on those:

- The unverifiable word was `#8A8F98` on white, 3.24 to 1 at 15px bold. It is
  now mixed 70 percent toward the ink while the hatch keeps the token exactly.
  Mixing rather than hardcoding means dark mode moves it the other way without
  a second rule.
- Dark mode's impeached red was 4.45 to 1 where the verdict list needed 4.5.
  It is `#DE5C57` at 4.84, and the shader's dark bleed constant moved with it,
  so the ink under the paper and the ink on it stay one colour.

Found by measuring every colour actually in use against its own font size,
rather than by eye. Also: a 404 for a favicon nobody asked for, now an inline
SVG data URI so the request is never made; deferring the one script took total
blocking time to zero and main thread work to 0.4s; at 360px nothing overflows
and the ledger reflows to two rows.

**Two things step 6 could not verify and did not claim.** The macOS reduced
motion and dark mode switches are system settings, so rather than flipping
them the exact media query blocks were extracted from the shipped stylesheet
and applied directly, with `matchMedia` patched so the script took the same
branch. And the automation tool refuses `file://` URLs, so that path is
verified structurally instead: every reference relative, no module scripts,
and the shader inlined so the one fetch is never reached.

The QA harnesses used for this (`qa-snap.js`, `qa-snap.html`, `qa-reduce.html`,
`qa-nowebgl.html`) were scratch and were deleted rather than committed. They
are reconstructable from the description above if the QA pass needs repeating.

### Verified today, separately from the commit messages

- `go test ./... -count=1` green, 327 tests, all nine packages.
- The live page at <https://snowhiteohno.github.io/cli/> is byte identical to
  the committed `impeach/site/index.html`, 20,909 bytes, diffed after fetching.
  The drift gate reaches production.
- The index, the sample report, the sample stylesheet and the sample JSON all
  serve 200.

### The one defect this leaves

`internal/report/gen-site` still renders an `index.html` of its own from a Go
template, so running it overwrites the hand written page with the older
generated version. Nothing runs it and the Pages workflow publishes the
committed directory rather than building it, so the site is not at risk, but
it is a footgun aimed at the next person. `impeach/site/README.md` documents
the safe subset to run in the meantime. The second half of the same fix: the
hero's testimony and record lines are hardcoded in `index.html` to match
`sample/impeach.json` and should be read from that file at build time. Both
are now listed in `BUILDATHON.md` under what is genuinely open, which is where
the audit that prompted this entry found five items that were already done.

## An outside audit, and the four things it was right about

An audit arrived claiming the repository was not submission ready. Four of its
five items held up when checked, one was right about the symptom and wrong
about the cause, and the fifth turned out to be a trap. Recorded in the order
they were worked.

### The blocker was real, and it was ours

`go test ./...` from the repository root failed, on the host CLI's own guard
test, `TestGitMetadataTraversalHasCanonicalOwner`. That guard parses every Go
source in the tree and rejects code that independently inspects a `.git` entry.
It scans the `impeach` module too, and `worktree.go`'s
`os.Stat(filepath.Join(path, ".git"))` was a genuine violation of a host
invariant rather than a false positive.

The guard offers two exits and both were wrong here. Calling
`gitrepo.ResolveWorktreeMetadata` means importing the host's internals, which
breaks the settled decision that Impeach depends only on documented CLI
output. Adding an allowlist entry to the host test means editing a file outside
`impeach/` in order to keep a probe that was answering the wrong question.

So the question is now asked of git: `git worktree list --porcelain` reports
whether a path is a registered worktree of this repository and at what commit,
which is what the caller actually needs to know. Statting `.git` asked "is
something git-shaped here", which also says yes to a directory belonging to a
different repository. Deleting the stat and keeping the `rev-parse` was not an
option either, because `git -C dir rev-parse HEAD` walks up to the enclosing
repository, so a stale directory would have reported a HEAD that was never its
own.

The replacement fixed a case the original got wrong. Git keeps listing a
worktree whose directory has been deleted and still reports its last commit,
so the reuse path would have skipped creating a checkout that is not on disk.
A prunable record now counts as absent, with a test.

### The two versioncheck failures are this machine, not this repository

The audit called them unrelated, which is nearly right but misses where they
come from. `TestUpdateCommand` injects `/opt/homebrew/bin/entire` and expects
brew to be detected. On this machine that path is a symlink to
`/Users/mallikasuri/Desktop/Impeach/entire`, the locally built binary, put
there so `entire` is on `$PATH` for the demo. `normalizedExecPath` resolves
symlinks, so the resolved path contains none of the brew markers, detection
returns unknown, and the test gets the curl command instead.

Two things follow. Impeach changed no host source at all, which
`git diff --stat upstream/main..HEAD -- cmd/ internal/ redact/` confirms as
empty, so this is not ours in any sense. And a judge who clones the fork will
not see it: the failure needs that symlink. Removing it would fix the tests and
take `entire` off `$PATH`, which is why it has been left in place and written
down here instead.

### The Graph review figures were stale, and now name their commits

The recorded review reported 122 files, 115 parsed, 1392 changes and 7
unsupported files. Those were true at `516cf70a` and the section read as
though they were current. Re-run at `a58889a8`: 35 commits from the fork point,
147 files, 138 parsed, 9 unsupported, 1796 changes. The two further unsupported
files are the committed woff2 fonts, and 147 minus 138 is 9, so the gap
symmetry the original review made a point of still holds.

The conclusion did not move, which is the part worth having. All 1796 changes
are still `added`, and both sources still agree that exactly one pre-existing
file was modified: Graph reports `README.md` with the single change
`added section 'Impeach'`, git reports it as the only non-added path at 4
insertions and 0 deletions. Purely additive after twice as many commits.

`BUILDATHON.md` now gives both sets and names the commit each was measured at,
because a figure about "HEAD" is false again on the next commit, which is
exactly how the first set came to sit there reading as current.

### The checkpoint table listed three of twenty-six

The audit was right that the table understated the evidence, and right about
the harder half too: there is no initial-intent checkpoint and there is no
honest way to produce one. The hooks were installed part way through, so phase
0 through phase 5 predate capture. What stands in for it is the six commit
messages, the phase reports in this file and the four design documents, and
`BUILDATHON.md` now says that plainly rather than leaving the gap to be found.

The table now covers the four moments the guide asks for, names the Curveball
checkpoint `01M1TR5GCVA7ZE650KSRY2G35S` and the tip, and gives the one-line
`git log --format` that enumerates all twenty-six against their commits.
Both the Curveball and the earliest build-session checkpoint were opened with
`entire checkpoint explain` before being listed, rather than assumed to
resolve.

### The whitespace item is a trap, and the bytes stay

`git diff --check` reports 58 whitespace diagnostics in the committed site, and
stripping them would damage the thing they are in. All five in `index.html`
fall between lines 54 and 69, which is the `<section id="lead">` block lifted
verbatim from the sample report and pinned by
`TestLandingHeroMatchesTheSampleReportByteForByte`. The other 24 are in
`sample/impeach.html`, the real audit artifact that must not be hand edited.

`report.html.tmpl` has no trailing whitespace of its own, so this is template
execution leaving the indentation of its actions behind, not sloppiness in a
source file. Tidying it would mean either breaking the byte-identity guarantee
or rewriting a generated evidence file to satisfy a linter, which is the same
call already made about the em dash in the quoted testimony: the report is
evidence, and evidence is not reformatted to look neat. Left as is,
deliberately, and recorded here so the next person who runs
`git diff --check` knows it was considered.

### Also, and this one is procedural

Another agent session was committing to this worktree while this work was in
progress. Two commits landed mid-audit and the site files were dirty from
someone else's edit, which is why only `internal/record/worktree.go` and its
test were staged by hand rather than committing everything present.
`HANDOFF.md` says one agent session at a time; that rule was not being followed
during this round, and the mitigation was to stage by path and never by
wildcard.
