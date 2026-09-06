# Impeach

Impeach is for the person merging agent-authored changes: someone holding a
diff and a transcript summary, without the time to reopen the session, re-read
the tool log and re-run the suite. It cross-examines what the agent claimed it
did against the record of what it actually did, and gives every claim a verdict
with the evidence attached.

It runs as a plugin for the Entire CLI. The agent's transcript is testimony.
The checkpoint's tool activity, the entity-level diff from Entire Graph, and a
fresh test run are the record.

```
VERDICT    FAMILY     CLAIM                                    REASON                 RERUN
impeached  execution  "pytest tests/test_api.py: 4 passed."    contradicted by rerun  new failures

IMPEACHED [c1]
  Re-running now shows 3 new failures.
  command  seq 12  ... pytest tests/test_api.py  ->  pass (exit 0)
           | ============================== 4 passed in 0.00s ===========
  rerun    new_failures  3 new failures: tests/test_rounding.py::test_round_money_negative, ...
           reproduce: entire graph verify --repo <worktree> --pre-edit-baseline ...
```

Every claim gets one of four verdicts. **Corroborated**: the record supports
it. **Impeached**: the record contradicts it, with a reason code.
**Uncorroborated**: the evidence channel exists but says nothing either way.
**Unverifiable**: the channel is missing. Missing data is a state, never an
error. A fifth row type, **unrequested**, flags added or signature-changed
symbols that no prompt asked for.

## Install

Requires the Entire CLI with checkpoints enabled, the `entire-graph` plugin,
Go 1.26 or newer, and Python 3 for the fixture app.

```
git clone https://github.com/snowhiteohno/cli.git
cd cli/impeach
mkdir -p ~/.local/share/entire/plugins/bin
go build -o ~/.local/share/entire/plugins/bin/entire-impeach ./cmd/entire-impeach
```

The Entire CLI prepends that managed directory to `$PATH` at startup, so the
binary is discovered with no further step. Check it:

```
entire plugin list        # lists "impeach"
entire impeach --version
```

`entire plugin install ./entire-impeach` also works and is the documented
route, but **it creates a symlink rather than copying**, so the binary you
built has to stay where it is. Delete it, or build it in a temporary
directory, and the plugin silently becomes a broken link and `entire impeach`
goes back to reporting an unknown command. Building straight into the managed
directory avoids that entirely, which is why it is the instruction above. If
you do use `plugin install`, add `--force` to replace an existing entry, and
note that `/entire-impeach` is gitignored here for exactly this reason.

Two things that do not work, so you do not lose time on them. `go install
github.com/...` fails either way: the module path is
`github.com/entireio/cli/impeach` while the code lives in a fork, so the paths
disagree. And there is no `entire plugin dir` command to ask for the managed
path, despite it being the obvious guess.

## Reproduce the demo

**One step is easy to miss.** A plain `git clone` brings no checkpoints. The
refs live under `refs/entire/*`, which is outside git's default refspec, so
they have to be asked for:

```
git fetch origin 'refs/entire/*:refs/entire/*'
entire checkpoint list        # 8 checkpoints, or the audit has nothing to resolve
```

Without that fetch, `entire impeach` correctly reports that no commit carries
the checkpoint, which is accurate and confusing at the same time.

Then audit the demo checkpoint:

```
entire impeach 01M1TET4N33VMY0DTHNZKV5HT9 --repo . --fail-on impeached
```

No `--test` is needed, because the repository commits one. `.impeach.json` at
the root carries the test command and a `setup` command that builds the
fixture virtualenv, which matters because `graph verify` runs in detached
worktrees where the gitignored `.venv` does not exist.

Both are overridable: `--test` replaces the command, and `--setup` replaces
the setup step, including with an empty value to turn a committed one off.
That override exists because a committed config that cannot be overridden
silently changes what a run does, which was found when adding this file broke
the offline replay tests.

That prints the impeached row above and exits 2. What happened: a real
captured agent session switched `round_money` to banker's rounding, ran only
`tests/test_api.py`, and reported "4 passed", which was true. Re-running the
whole suite against a baseline recorded from the parent commit finds three
tests that used to pass and now fail. The claim was accurate and the change was
still broken, which is the everyday case this tool is for.

Add `--out ./impeach-out` for the JSON and the self-contained HTML report:

```
entire impeach 01M1TET4N33VMY0DTHNZKV5HT9 --repo . --out ./impeach-out
open ./impeach-out/impeach.html
```

The second demo checkpoint, `01M1TJCCYXR7ZZTK1H8167G249`, shows the other three
verdicts and the unrequested block. There is no impeached row on it, and the
reason is worth reading: that agent was accurate. It scoped every claim to the
file it had actually run and volunteered which tests it had not. There was
nothing to impeach.

**Three things matter more than they look.**

`--tb=no -rA` in the committed test command is not cosmetic. `entire graph
verify` needs per-test ids to adjudicate; with a bare `pytest -q` its parser
does not engage, the rerun degrades to an exit code, and the report says so
instead of claiming a pass.

`--fail-on` is the CI contract: `impeached`, `uncorroborated`, or `incomplete`
to gate on evidence that was not intact. Exit 0 completed, 2 the condition was
met, 1 a runtime error. There is no fourth exit code.

`"sensitive": true` in `.impeach.json` makes the constraint a property of the
repository rather than a flag someone has to remember. In that mode `--model`
is refused with a non-zero exit and a message naming the command it refused,
before any call is made. It is `false` here deliberately: this fork is public
and its transcripts are published on purpose, so there is nothing to protect
and refusing `--model` would only stop a reviewer trying it. Flip that one
word, or pass `--sensitive`, in a repository where it matters.

### Reproduce with no Entire, no git and no agent

Every external call in two real audits is committed under
`fixtures/recorded/`, so the whole pipeline replays offline:

```
cd impeach && go test ./...
```

285 tests, no network, no agent. The end-to-end tests among them drive the real
entry point against those recordings. `fixtures/recorded/README.md` explains
the provenance and how to replay one by hand.

## What it checks

| Family | Example claim | Evidence |
|---|---|---|
| Execution | "All tests pass", "the build succeeds" | Command events and their output, order relative to the last edit, and a baseline-aware rerun |
| Structural | "Added `parse_refund`", "renamed `foo` to `bar`" | Entity-level changes from `entire graph commit` |
| Safety | "No other callers", "backward compatible" | Callers and signature changes from `entire graph impact` |
| Reading | "I reviewed the callers", "checked `service.py`" | File read events, matched against the caller files Graph found |

Reason codes on an impeachment: `stale`, `scope-mismatch`,
`contradicted-output`, `contradicted-rerun`, `callers-exist`,
`signature-changed`, `not-in-diff`, `never-read`.

The rerun is reported in its own column, independent of the verdict, because
the verdict is about the testimony at commit time and the rerun is about the
code now.

Every run also reports a context ledger: each evidence channel and whether it
was present, partial, redacted or absent. **Corroborated is reachable only
from a channel that is present.** A partial or redacted channel can still
impeach, because a contradiction from an intact channel survives redaction of
another, but it can never corroborate: what was removed could be exactly what
would have contradicted the claim. Such a verdict degrades to unverifiable
with the channel named, carrying the reason code `channel-incomplete`. The
asymmetry is deliberate. Absence of evidence is not evidence of honesty.

## Limitations

- **Claim detection is pattern based.** Vaguely phrased claims are missed. A
  missed claim is silent, not a false one, which is the deliberate direction to
  fail in.
- **Four false positives were found and fixed during the build**, all of them
  by pointing the tool at real transcripts rather than by reading the code. Two
  markdown section headings were read as claims; a clause ending in a colon was
  impeached for a claim it never made; and a compatibility claim was impeached
  for having callers, when callers say nothing about whether behaviour changed.
  For a tool whose whole value is accuracy about other people's accuracy, a
  false accusation is the worst failure available, so they are listed rather
  than quietly patched.
- **Result parsing** knows pytest, go test, jest, cargo, rspec, phpunit, maven
  and gradle summaries. Others fall to uncorroborated rather than guessing.
- **"Backward compatible" is checked structurally**, as a signature change on a
  symbol with callers. A behaviour change behind a stable signature is not
  detected.
- **Unrequested matching is name based and conservative.** Body-only changes
  are never flagged, so unrequested behaviour inside an existing function slips
  through.
- **One transcript adapter**, `claude-code`. Other agents degrade to
  unverifiable rows by design, which is the truthful output rather than a
  failure.
- **`entire graph impact` callee resolution can cross languages.** Asked about
  the Python `compute_total` it resolved the builtin `sum` to a Go function in
  the host repository. Callers were correct, so no verdict is affected, but
  nothing is built on callees.
- **The fixture app is seeded** to exercise each verdict, and the report footer
  says so. A plain `pytest` there shows three failures; those are deliberate.
- **Secret scrubbing on excerpts is pattern based** and cannot catch every
  shape. Reports are meant to be pasted into pull requests, so excerpts are
  bounded and scrubbed, but the limit is real.

## Security posture

Impeach reads transcripts, checks out historical commits, runs a test command
and writes reports that people paste into pull requests. Three rules follow
from that:

- **It never executes a command found in a transcript.** Commands in the tool
  log are matched as strings and displayed, nothing more. The only command run
  on your behalf is the one from `--test` or a committed `.impeach.json`, and it
  is echoed before it runs.
- **There is no shell anywhere.** Every external process goes through one
  `Runner` as an argv array. Symbol names reach `graph impact` as a single
  argument.
- **The default path makes no model calls and no network calls.** `--model` is
  opt in; when set, only the assistant text of one turn at a time is sent,
  never a prompt, never a tool output, never a file, capped by `--model-turns`.
  The report header names the exact command, and model-derived claims are
  marked in the table.

Prompts never appear in a report as full text: what is published is the size of
the prompt corpus and the identifier tokens actually searched for. See
`docs/SECURITY_AND_ACCESS.md`.

## Disclosure

**Prior work.** The concept comes from the author's earlier cleanup of
fabricated evidence in an incident-response agent environment, where agent
claims were audited by hand. No code is reused.

**AI-generated components.** All the product code here was written by Claude
Code inside this fork, and the Entire checkpoints on the fork are the record of
that. Two of them are the demo checkpoints being cross-examined; the rest are
the build itself. The pattern library, verifier rules and fixtures were
hand-reviewed. Anything a model extracted at run time is marked as such in the
report's family column and named in its header.

One honest gap: only the commits made from captured `claude -p` subsessions
carry checkpoints. A fresh clone has no git hooks, so Entire was enabled in
settings but capturing nothing until the hooks were installed, by which point
the session writing Impeach had already started. Its commits carry no
`Entire-Checkpoint` trailer, which is also why Impeach cannot audit its own
construction.

**On the fork being public.** The checkpoint refs on it are public too, so the
agent transcripts are readable by anyone. That is a property of Entire's
default storage rather than a choice made here, and it is why the committed
fixtures are scrubbed of absolute paths, user names and author identity.

## The landing page

`site/` is a single self-contained page for someone who will not clone
anything: the lead impeachment, what each verdict means, what gets checked,
install and reproduce commands, the full sample report in an iframe, and the
limitations. It makes no network requests and carries no analytics. It does
ship two self-hosted woff2 fonts and one script, which drives the WebGL hero;
everything the page says is in the markup and readable with scripts disabled.

**It is written by hand.** `index.html`, `style.css`, `site.js` and `fonts/`
are authored files. `sample/impeach.html` and `sample/impeach.json` are not:
they come from a real audit and must not be edited.

The two are coupled by one guarantee. The page's `<section id="lead">` block
is the same bytes as the report's, so the two surfaces cannot drift into
giving different accounts of the same audit. A test asserts that byte match,
and others assert no absolute paths, no cross-host resources, and that
`site/style.css` and `internal/report/report.css` resolve every shared token
to the same value.

`internal/report/gen-site` produces the sample report and, from an older
template, a generated page of its own. Running it against `site/` used to
replace the hand-written page and stylesheet with that older version while
every test stayed green, because the generated page satisfies the same
assertions. It now refuses: generated files carry a marker and gen-site will
not write over an existing `index.html` or `style.css` that lacks one. To
refresh the sample, generate elsewhere and copy by hand. See
[`site/README.md`](site/README.md) for the procedure and the reasoning.

**It is live at <https://snowhiteohno.github.io/cli/>.**

Published by `.github/workflows/impeach-pages.yml`, which exists because the
frontend spec's instruction to serve from folder `/impeach/site` is not
possible: a Pages branch deployment offers only `/` or `/docs`, and `/docs` in
this fork already holds the upstream CLI's documentation. A Pages Actions
workflow sidesteps the folder restriction entirely.

The workflow publishes the committed site rather than building it, because
generating the page needs a real audit and therefore the Entire CLI, the graph
plugin and a checkpoint, none of which exist in a runner. What it does instead
is refuse: it runs the site tests and then the whole suite before deploying,
so a hand-edited or drifted page never ships. The deployed hero has been
checked byte for byte against the committed sample report, so the guarantee
holds in production and not only in the test suite.

To serve it locally instead:

```
cd impeach/site && python3 -m http.server
```

## Reading further

- `docs/PRD.md`: the user, the problem, the claim families, what is out of scope.
- `docs/ARCHITECTURE.md`: the four boundaries, the data flow, and the Step 0
  probe findings, including the six answered open questions.
- `docs/SECURITY_AND_ACCESS.md`: threat model, execution and data-handling rules.
- `docs/FRONTEND_SPEC.md`: the report's design tokens and structure.
- `NOTES.md`: the build log, every phase, and what turned out to be wrong.
- `../BUILDATHON.md`: the submission summary and the final review.
