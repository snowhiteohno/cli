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
