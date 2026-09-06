# Impeach

## One-sentence summary

Impeach is a plugin for the Entire CLI that cross-examines what an AI coding
agent claimed it did against the record of what it actually did, giving every
claim one of four verdicts with the evidence attached.

## Problem, intended user and why it matters

Agent transcripts are full of assertions: "all tests pass", "no other callers
are affected", "I reviewed the call sites". Reviewers accept them, because
checking one means reopening the session, re-reading the tool log and
re-running the suite. The cost shows up as merged changes whose tests were
never rerun after the last edit, "safe" refactors that changed a signature
with three live callers, and functions nobody asked for.

The intended user is the person merging agent-authored changes, who has the
diff and a summary but not the time to audit the session. Second is the
developer who ran the session and wants to know what the agent got wrong
before pushing. Third, later, is CI through the exit code, which is why the
exit code contract exists from the first commit even though the Action does
not.

Entire already stores everything needed to check these statements: the
prompts, the full transcript, every tool call with its result and timestamp,
and the files changed. Nothing reads that record back against the transcript's
own claims. `entire review` judges the code. `entire why` and `entire blame`
explain where a line came from. No command asks whether the agent's account of
its own work is true. That is the gap Impeach fills.

The everyday case is not a lying agent. It is an honest one whose claim was
true when it was made and stopped being true two edits later. The demo
checkpoint in this repository is exactly that, and it was produced by a real
agent session that was not told what to claim.

## Selected Entire track and why Entire is essential

Built as an Entire CLI plugin, on Checkpoints and Entire Graph. It is
dispatched kubectl-style: any executable named `entire-<name>` on `$PATH` runs
as `entire <name>` with stdio and the exit code passed through, so
`entire-impeach` becomes `entire impeach`.

Entire is not a convenience here, it is the only source of the record:

- The **tool activity and prompts exist nowhere else.** Impeach needs the
  ordered sequence of reads, edits and commands with their outputs. Git has
  the diff, not the session. Without the checkpoint transcript there is no
  testimony to cross-examine and no tool log to check it against.
- The **entity-level diff and impact come only from Entire Graph.** A textual
  diff cannot say that a function's signature changed rather than its body, or
  that the function has three live callers. Both distinctions decide verdicts.
- The **baseline-aware rerun comes only from `entire graph verify`**, which
  reports which tests changed state rather than what the runner printed, and
  labels failures that predate the edit as PRE-EXISTING. Without that, a
  pre-existing failure gets blamed on the checkpoint.

Remove Checkpoints and Impeach has no testimony. Remove Graph and it has no
record. There is nothing left to compare.

## Architecture and main workflow

A single static Go binary, `entire-impeach`, in its own module under
`impeach/` with no import of the host CLI's internals. Four boundaries, each
an interface with one default implementation, so a change of constraint is
absorbed inside one of them:

1. **Checkpoint reader** (`internal/checkpoint`): reference to checkpoint id,
   commit and parent; transcript fetch.
2. **Claim extractors** (`internal/claims`): a deterministic pattern library
   by default, an opt-in model command producing the same `Claim` type.
3. **Verifiers** (`internal/verify`): one per claim family, pure functions
   over an immutable `Record`.
4. **Report renderers** (`internal/report`): table, and later JSON and HTML,
   all reading one `Report` struct so the surfaces cannot disagree.

Every external process goes through a single `Runner` interface. There is
deliberately no working-directory field: each command carries its own path
argument (`git -C`, `graph --repo`), so a worktree is selected by argv and a
recorded call is fully described by its name and arguments. That is what makes
offline replay tests possible, and it is why `impeach record` can capture a
whole audit and replay it with no Entire, no git and no agent.

The workflow:

```
entire impeach <ref> --test "pytest -q --tb=no -rA"
   |
   |- 1. Resolve    ref -> checkpoint id + commit + parent, via the
   |                Entire-Checkpoint trailer
   |- 2. Testimony  entire checkpoint explain <id> --raw-transcript
   |                -> adapter -> normalized []Event
   |- 3. Record     git worktree add (head, base)
   |                entire graph commit <sha> --json     -> entity changes
   |                entire graph impact --symbol S --format json -> callers
   |                entire graph verify --record-baseline on the parent, then
   |                  --pre-edit-baseline on the head    -> adjudicated rerun
   |- 4. Extract    pattern library over assistant text  -> []Claim
   |- 5. Verify     one verifier per family: Claim + Record -> Verdict
   |- 6. Render     table to stdout, exit code
```

Two invariants run through all of it. **Missing data is a state, never an
error**: an absent evidence channel yields `unverifiable` with the channel
named, rather than a failure or a guess. And **Impeach never executes a
command found in a transcript**: commands in the tool log are matched as
strings and displayed, nothing more. The only command it runs on the user's
behalf is the one from `--test` or a committed `.impeach.json`, and it is
echoed before it runs.

Verdicts are `corroborated`, `impeached`, `uncorroborated` and `unverifiable`.
Every impeachment carries a reason code: `stale`, `scope-mismatch`,
`contradicted-output`, `contradicted-rerun`, `callers-exist`,
`signature-changed`, `not-in-diff`, `never-read`. The rerun is reported in its
own column, independently of the verdict, because the verdict is about the
testimony at commit time and the rerun is about the code now.

Ordering uses a monotonic sequence number rather than timestamps, so a
transcript with no clock still detects staleness correctly.

## Entire Graph findings and verification

A go/no-go probe ran before any product code, against Entire CLI
`0.10.6-nightly.202609050622.61dac01ed` built from source and entire-graph
`v0.4.0`. It answered six open questions and contradicted the design in six
places. Two findings did the most to shape the build:

**`graph commit` distinguishes `signature_changed` from `body_changed` for
Python, and carries both signature strings.** Verified by committing a
signature change plus a new function in a throwaway worktree and reading the
JSON: the change types observed are `added`, `body_changed` and
`signature_changed`, each with `kind`, `name`, line numbers and
`dependents_count`, and a signature change additionally carries
`old_signature` and `new_signature` as strings. That means the safety verifier
can quote the exact signature delta instead of inferring one, and the
unrequested detector can exclude body-only changes with confidence rather than
by heuristic.

**`graph commit`, `graph diff` and `graph impact` all carry JSON, while
`graph verify` does not, which inverted the parsing plan.** The architecture
had planned text parsers for `commit` and `impact` and told the probe to check
for a JSON flag. In fact `graph commit --json`, `graph diff --json` and
`graph impact --format text|json` all exist, and `graph verify` has neither
`--json` nor `--format`. So three of the four record inputs need no text
parsing at all, and `verify` is the single place in the codebase where a text
parser is unavoidable. This was established from the installed binaries'
documented flags and, for the plugin data directory question, from the CLI
source itself rather than assumed.

Two further findings with teeth:

**`pytest -q` degrades the rerun to exit-code-only.** `graph verify`'s pytest
parser needs per-test ids in the output. With `pytest -q`, or a bare `pytest`,
it reports "output format not recognised, so the baseline is exit-code only"
and returns `"parser": "exit-code-only"` with an empty results map. With
`-q --tb=no -rA` it returns `"parser": "pytest"` and 18 individual results.
Every design document originally specified `pytest -q`, which is exactly the
command that buys nothing. **The documented command is now
`pytest -q --tb=no -rA`**, corrected in the PRD, the architecture, the
frontend spec and the fixture app. Impeach does not rewrite a user's command,
per its own execution policy, so instead it detects the degraded parser and
says so in the report rather than quietly presenting a coarse result as a
fine one.

**`graph verify` exits 0 even when it reports a regression.** The probe's
regression run printed `NEWLY FAILING (3)` and `VERDICT: REGRESSION in 3
tests` and still exited 0. The exit code carries no verdict; only the text
does. An early version of the wrapper trusted an emptiness check on the output
and reported a failed runner as a pass, which is corrected and covered by a
test named for the mistake.

Verification of Impeach itself: 197 Go tests and 18 pytest tests, green at
every commit. No test needs a live agent or a network, because the transcript,
Graph and verify outputs are replayed from committed fixtures under
`impeach/testdata/`. The graph parsers are tested against the exact bytes the
installed plugin emitted during the probe, so a format change surfaces as a
failure in one file. Graph's own search was used to answer the plugin data
directory question, which located `runPlugin` in
`cmd/entire/cli/plugin.go` and settled it from a source comment; that answer
was then confirmed empirically when installing Impeach as a managed plugin
moved its worktrees into `ENTIRE_PLUGIN_DATA_DIR` with no code change.

### Final review: the whole build, cross-examined

The last thing built was a review of everything built, using the tool's own
evidence source. `entire graph diff --base 3dbdf8b --head HEAD --json` across
the 17 commits from the fork point, with its conclusions then checked against
git and against the test suite rather than taken on trust. That check is the
point: a structural claim from Graph is testimony too.

Scale: 122 files touched, 115 of them parsed, 1392 entity changes.

**Every one of the 1392 changes is `added`.** Not one `removed`, `renamed`,
`signature_changed` or `body_changed`. So the build is purely additive with
respect to the fork, which is the strongest single statement available about
whether a plugin built inside someone else's repository disturbed it.

Two independent sources agree on that:

- git reports exactly one pre-existing file modified, `README.md`, with 4
  insertions and 0 deletions. Every other path is `A`.
- Graph reports the same file as status `M` carrying a single change, `added
  section 'Impeach'`.

The 7 files Graph did not parse are `impeach/go.mod`, the HTML template, and 5
recorded testdata fixtures (`.jsonl` and `.txt`). They match the 7
`W_UNSUPPORTED_FILE` warnings exactly, and 122 minus 115 is 7, so the warning
list accounts for the entire gap. Nothing was silently dropped. That
symmetry is worth naming, because it is the same contract Impeach offers its
own users: a channel that cannot be read is reported as unreadable rather than
reported as empty. Graph cannot see inside `report.html.tmpl`, so a structural
claim about the HTML report would be `unverifiable` by Impeach's own rules.

Graph's symbol counts were then checked against the source:

| Entity | Graph | Counted from source | Delta |
|---|---|---|---|
| Go functions | 392 | 392 | 0 |
| Go methods | 93 | 85 | +8 |
| Go types | 84 | 83 | +1 |

Both deltas resolve in Graph's favour, and the explanations are exact.

The 8 extra methods are the 8 method declarations inside interfaces:
`Verifier.Family` and `Verifier.Verify`, `Runner.Run`, `Adapter.Name`,
`Adapter.Detect` and `Adapter.Parse`, `Extractor.Name` and `Extractor.Extract`.
A `grep '^func ('` cannot see them because they are not function
declarations. Those 8 are precisely the four boundaries this design rests on,
so Graph's method count exceeds a naive grep by exactly the size of the
architecture's interface surface. The extra type is `pending`, declared inside
a function in the Claude Code adapter and therefore indented past a
line-anchored grep. In both cases the tool was more complete than the
hand-rolled check, which is the useful direction for that to fail in.

Test-side verification at the same commit:

```
go vet ./...        clean
gofmt -l            clean
go test ./...       285 tests, 8 packages, all pass
go test -race ./... all 8 packages pass
pytest              22 passed, 3 failed
```

The 3 pytest failures are the fixture's documented seeded failures, from the
banker's rounding change the demo checkpoint audits. The fixture README states
which three and why. A green fixture would mean the demo had nothing to find.

What this review does not prove. It is a structural and behavioural check, not
a correctness proof: it says the build added what it says it added, disturbed
nothing else, and passes its own tests under the race detector. It says
nothing about whether the verifiers reach the right verdict on transcripts
nobody has written yet, and the four false positives found during phases 5, 6
and 8 are the honest evidence that reading the code is not how those get
caught.

## Noon Curveball: what changed and how we adapted

pending

## Checkpoint links and what each checkpoint proves

Fork: <https://github.com/snowhiteohno/cli>, forked from `entireio/cli`.
Mirror: `entire://aws-us-east-2.entire.io/gh/snowhiteohno/cli`, mirror ID
`01M1TKHC7813MNVG5Y543V7P83`, status ready. Clone it with
`git clone entire://aws-us-east-2.entire.io/gh/snowhiteohno/cli`.

Three checkpoints, all pushed to the fork as `refs/entire/checkpoints/**`:

| Checkpoint | Commit | What it proves |
|---|---|---|
| `01M1TJWAR63MRKCP5AMZ2NQR9H` | `ab8ba0e` | Phase 6, the pre-noon checkpoint. All four verifiers and the unrequested detector, 224 tests. Its commit message records intent, architecture, what is done, what is not, and the five open risks. |
| `01M1TJCCYXR7ZZTK1H8167G249` | `0bd3033` | Demo checkpoint two. A real captured session that added an order-level discount and renamed `line_subtotal`. Audited, it yields corroborated, uncorroborated and unverifiable rows plus three unrequested symbols. |
| `01M1TET4N33VMY0DTHNZKV5HT9` | `458bb14` | Demo checkpoint one. A real captured session that switched `round_money` to banker's rounding while running only `tests/test_api.py`. Audited, it yields the impeached row, by `contradicted-rerun` against three genuine new failures. |

Read any of them with `entire checkpoint explain <id>`, or audit one with
`entire impeach <id>`.

Commits on `main`, newest first:

| Commit | Checkpoint | What it proves |
|---|---|---|
| phase 5 followup | none | The fixture README stopped being true and was corrected. |
| phase 5 report | none | The stop-point report, written so a fresh session can reconstruct the build. |
| phase 5 | none | Execution verifier, pattern extractor and table. First end-to-end path, 197 tests. |
| phase 4 | none | Record layer. Graph JSON parsers and the baseline-aware verify wrapper. |
| phase 3 | none | Claude Code transcript adapter, built against the real probe transcript. |
| phase 2 | none | Skeleton, the Runner boundary, resolve by trailer, worktrees. |
| phase 1 | none | The Step 0 probe and the six answered questions. |
| `458bb14` | `01M1TET4N33VMY0DTHNZKV5HT9` | The demo checkpoint. A real captured agent session that read two files, switched `round_money` to banker's rounding, ran only `tests/test_api.py`, and committed. This is the checkpoint Impeach audits. |
| phase 0 | none | Docs, handoff and the fixture app with a green 18-test baseline. |

The fork is public, which means the checkpoint refs on it are public too.
That is a property of Entire's default storage rather than of Impeach, and it
is the reason the committed fixtures are scrubbed of absolute paths, user
names and author identity.

An honest disclosure, since it is visible in the record and would be noticed
anyway: **only three commits carry checkpoints.** A fresh clone carries no git
hooks, so Entire was enabled in settings but capturing nothing until
`entire configure --force` installed them, which happened during phase 0.
The session that wrote Impeach had already started by then, so its commits
carry no `Entire-Checkpoint` trailer. The three that exist all come from `claude -p`
subsessions, which are captured because they started after the hooks were
installed. Two of them are the demo checkpoints being cross-examined, and the
third is the pre-noon phase 6 checkpoint.

The lesson is worth stating because it is the same class of failure Impeach
exists to catch: the hooks were installed, they ran, and they silently did
nothing, because `entire` was on a path that login shells do not search and
every hook guards itself with `command -v entire`. It looked like it was
working. Nothing said otherwise.

## Setup, run and test instructions

Requires the Entire CLI with Checkpoints enabled, the `entire-graph` plugin,
Go 1.26 or newer, and Python 3 for the fixture app. Works offline. No model
calls unless `--model` is passed.

```
# 1. Build and install the plugin so `entire impeach` dispatches to it.
cd impeach
go build -o "$(entire plugin dir 2>/dev/null || echo ~/.local/share/entire/plugins/bin)/entire-impeach" ./cmd/entire-impeach

# 2. Confirm it is discovered.
entire plugin list
entire impeach --version

# 3. Build the fixture app's virtualenv.
cd fixtures/app && sh scripts/setup.sh && cd -

# 4. Audit the demo checkpoint that carries the impeached row.
#    The test command bootstraps the virtualenv, because the fixture venv is
#    gitignored and so absent from the detached worktrees graph verify uses.
entire impeach 01M1TET4N33VMY0DTHNZKV5HT9 --repo <repo-root> --fail-on impeached \
  --test 'cd impeach/fixtures/app && { test -d .venv || { python3 -m venv .venv && ./.venv/bin/python -m pip install -q -r requirements.txt; }; } && ./.venv/bin/python -m pytest -q --tb=no -rA'

# 5. Audit the second demo checkpoint, for the other three verdicts.
entire impeach 01M1TJCCYXR7ZZTK1H8167G249 --repo <repo-root> \
  --test 'cd impeach/fixtures/app && ./.venv/bin/python -m pytest -q --tb=no -rA'
```

Verdict coverage is spread across the two checkpoints rather than shown in one
table, and the reason is a finding rather than an omission. See the limitations
below.

Tests:

```
cd impeach && go test ./...                     # 197 tests, no network, no agent
cd impeach/fixtures/app && ./.venv/bin/python -m pytest -q
```

The fixture suite shows three failures on a current checkout. That is the
seeded state, not a broken fixture: the probe commit deliberately breaks
`test_round_money_half_up`, `test_round_money_two_places` and
`test_round_money_negative` by switching to banker's rounding. Check out the
phase 0 commit to see the green 18-test baseline.

Exit codes: 0 completed, 2 the `--fail-on` condition was met, 1 a runtime
error such as an unresolvable checkpoint.

## Known limitations and next steps

**A false positive, found by running Impeach against a real checkpoint rather
than by reading the code.** Impeach impeached an honest claim. The probe
session had said, accurately, that it ran `pytest tests/test_api.py` and got 4
passed. Impeach reported `scope-mismatch` against it, because the sentence
also mentioned the directory `impeach/fixtures/app` as context and the scope
check read that directory as an uncovered test target. The claim asserted a
scope it was then punished for not covering. This is fixed: only
runner-addressable targets, meaning files with a source extension or a node
id, can bound a claim's scope, and a test named for the mistake pins it. It is
recorded here because for a tool whose entire value is accuracy about other
people's accuracy, a false accusation is the worst failure mode available, and
the fix landed only because the tool was pointed at real data.

Other limitations, all disclosed in the report footer at runtime:

- **Claim detection is pattern based.** Vaguely phrased claims are missed. A
  missed claim is silent, not a false one, which is the deliberate direction
  to fail in. Three further extraction bugs were found and fixed: "Not all
  tests pass" was read as a claim that they do; "Zero failing tests" was
  silently dropped because a blanket filter treated any failure word as a
  denial; and markdown emphasis survived into quoted testimony.
- **`pytest -q` degrades the rerun to exit-code-only**, as above. The
  documented command is `pytest -q --tb=no -rA`. When the degradation happens
  the report names it and says what to change.
- **Result parsing** knows pytest, go test, jest, cargo, rspec, phpunit, maven
  and gradle summaries. Others fall to uncorroborated rather than guessing.
- **"Backward compatible" is checked structurally**, as a signature change on
  a symbol with callers. A behaviour change with a stable signature is not
  detected.
- **Unrequested matching is name based and conservative.** Body-only changes
  are never flagged, so unrequested behaviour inside an existing function can
  slip through.
- **One transcript adapter**, claude-code. Other agents degrade to
  unverifiable rows by design, which is the truthful output rather than a
  failure.
- **`graph impact` callee resolution can cross languages.** Asked about the
  Python `compute_total`, it resolved the builtin `sum` to a Go function in the
  host repository. Callers were correct, so no verdict is affected, but nothing
  is built on callees.
- **The fixture app is seeded** to exercise each verdict, and the report footer
  says so.

**The demo table has no impeached row on the second checkpoint, because the
agent under audit was honest.** It scoped every claim to the file it actually
ran, and volunteered that it had edited `tests/test_service.py` without
running it. There was nothing to impeach. The first checkpoint does produce an
impeached row, by `contradicted-rerun`, so all four verdicts plus unrequested
are demonstrable across the two. Putting all five in one table needs either a
seeded recorded scenario, which is what `fixtures/recorded/` was for in the
cut phase seven, or an agent that overclaims, which cannot be arranged
honestly by instructing one to lie. Stated plainly because it is the more
interesting fact: this tool's headline row is hardest to produce exactly when
the agent under audit is careful.

Next steps:

- The structural, safety and reading verifiers, and the unrequested detector.
  The record layer already supplies everything they need, so they are verifier
  logic rather than new plumbing.
- JSON and self-contained HTML reports. The `Report` struct that both would
  render from already exists and is what the table renders from today.
- `impeach record` and the five committed replay scenarios. The `Recording`
  and replaying `Fake` halves of the Runner already exist and are tested round
  trip, so this is wiring rather than design.
- `--session`, the opt-in `--model` extractor, and `--adapter auto` across
  more than one adapter.
- A GitHub Action wrapping `entire impeach --fail-on impeached`. The exit code
  contract is already in place.
- **Impeach auditing its own build session is not possible for this repository
  and is not being attempted.** The session that wrote Impeach predates the git
  hooks, so its commits carry no checkpoint and there is nothing to resolve.
  Any session started after the hooks were installed can be audited normally.
  Making the tool audit its own construction would require restarting the build
  under captured hooks, which is a scheduling matter rather than a technical
  one.
