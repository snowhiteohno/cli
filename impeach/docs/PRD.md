# Impeach: product requirements

Impeach is a plugin for the Entire CLI, run as `entire impeach <checkpoint>`, that cross-examines what an AI coding agent claimed it did against the record of what it actually did. The agent's transcript is testimony. The checkpoint's tool activity, the entity-level diff from Entire Graph, and a fresh test run are the record. Every claim gets one of four verdicts (corroborated, impeached, uncorroborated, unverifiable) with the evidence attached, and changed symbols that no prompt asked for get flagged as unrequested. The core is deterministic and runs offline; a model-based claim extractor exists only as an opt-in layer. Nothing in the stack costs money.

## The problem

Agent transcripts are full of assertions: "all tests pass", "no other callers are affected", "added coverage for the refund path", "I reviewed the call sites". Reviewers accept them because verifying each one means reopening the session, re-reading the tool log, and re-running the suite. Entire already captures everything needed to check these statements: prompts, the full transcript, every tool call with its result, the files changed, and per-line attribution. Nobody reads that record against the transcript's claims. `entire review` judges the code. `entire why` and `entire blame` explain where a line came from. No command asks whether the agent's account of its own work is true.

The cost shows up as merged changes whose tests were never rerun after the last edit, "safe" refactors that changed a signature with three callers, and functions nobody asked for.

## Users

- Primary: the person merging agent-authored changes, who has the diff and the transcript summary but not the time to audit the session.
- Secondary: the developer who ran the session and wants to know what the agent got wrong before pushing.
- Later: CI, through the exit code. Not built in v1, but the exit code contract exists from day one so the continuation path is real.

## What Impeach does

1. Resolves a checkpoint (by ID or by any commit-ish carrying an `Entire-Checkpoint` trailer), pulls the transcript and tool activity through the Entire CLI, and checks out the linked commit and its parent into temporary worktrees.
2. Extracts claims from the agent's messages with a pattern library (default) or a model command (opt-in), and builds the record: tool calls with results and timestamps, files read and edited, entity-level changes and impact from Entire Graph, and an optional test rerun through `entire graph verify`.
3. Verifies each claim against the record with deterministic rules, prints a verdict table, and writes a JSON and a self-contained HTML report. Exit codes make the result usable as a gate.

## Claim families

| Family | Example claim | Evidence used | Corroborated when | Impeached when |
|---|---|---|---|---|
| Execution | "All tests pass", "build succeeds", "lint is clean" | Command events in the tool log, their outputs and order; `graph verify` rerun | A matching command ran after the last relevant edit, in the claimed scope, and its output shows success | No matching command; output shows failure; scope narrower than claimed (one of four test files); a relevant file was edited after the last run (stale) |
| Structural | "Added `parse_refund`", "removed the legacy shim", "renamed `foo` to `bar`", "added tests for X" | `graph commit` and `graph diff` entity changes | The named entity appears with the claimed change kind | The named entity does not appear in the entity diff, in a file Graph parsed |
| Safety | "No other callers", "backward compatible", "this change is isolated", "nothing else uses this" | `graph impact` callers, type consumers, signature changes | Zero callers outside the changed set, or no signature change | Callers exist; signature changed on a symbol with callers |
| Reading | "I reviewed the callers", "checked `service.py`", "looked at the existing tests" | File read events (Read, Grep, Glob) and their paths | The named file, or the caller files from `graph impact`, appear in read events | None of them were read |

Row type five, unrequested: an added or signature-changed symbol whose name (or its identifier tokens) appears in no prompt in the session. This is name matching only. It never uses a model.

## Evidence channels and completeness

Every evidence channel carries an explicit state: present, partial, redacted
or absent. The distinction between absent and redacted decides what a verdict
may conclude. An absent channel says nothing happened. A redacted one says
something happened and hid what it was.

Corroborated is reachable only from a channel that is present. A partial or
redacted channel can still impeach, because a contradiction from an intact
channel survives redaction of another, but it can never corroborate: what was
removed could be exactly what would have contradicted the claim. Such a claim
degrades to unverifiable with the channel named and the reason given. The
asymmetry is deliberate. Absence of evidence is not evidence of honesty.

The same reasoning applies to uncorroborated. On a readable channel it means
the record held nothing either way. On a redacted or partial one that is the
wrong statement, because the channel could not be read at all, so it also
degrades to unverifiable.

Graph is its own channel, separate from the transcript ones. Graph reads the
code at the commit rather than the transcript, so redaction of a transcript
leaves a structural or safety verdict intact; what gates those is Graph
failing to answer or failing to parse the file.

Every run reports a context ledger: each channel and its state, plus one
sentence when anything is short of present, in the table, the JSON and the
HTML. `--fail-on incomplete` lets CI refuse a run whose evidence was
incomplete even when nothing was impeached, reusing exit 2.

## Sensitive mode

`--sensitive`, or `"sensitive": true` in a committed `.impeach.json`, forbids
anything leaving the machine. In that mode `--model` is refused with a
non-zero exit and a message naming the command it refused. Not ignored and
not warned about: a warning would still have sent the text. The refusal
happens before any call is made, and the report header states the mode
verbatim.

## Verdicts

- Corroborated: the record supports the claim.
- Impeached: the record contradicts the claim. Every impeachment carries a reason code: `stale`, `scope-mismatch`, `contradicted-output`, `contradicted-rerun`, `callers-exist`, `signature-changed`, `not-in-diff`, `never-read`.
- A ninth reason code, `channel-incomplete`, marks a verdict that was downgraded to unverifiable because the channel it rested on was not intact.
- Uncorroborated: the evidence channel exists but contains nothing that supports or contradicts the claim (for example, a "tests pass" claim with no test command in the log, or output that could not be parsed).
- Unverifiable: the evidence channel is missing (redacted transcript, an agent whose transcript carries no tool records, a language Graph does not parse). Missing data is a state, never an error.

Rerun results are reported in a separate column (pass, new failures, not run) so a claim can be impeached for scope or staleness even when the suite happens to pass today. The verdict is about the testimony at commit time; the rerun is about the code now.

## Command surface (v1)

```
entire impeach <checkpoint-id | commit-ish> [flags]

  --test CMD          test command to rerun in the checkpoint worktree (or from .impeach.json)
  --setup CMD         command run before the tests in each worktree (or from .impeach.json);
                      pass an empty value to override a committed one
  --session           audit every checkpoint in the session, one report section each
  --format table|json|html   default table; html and json also written with --out
  --out PATH          write json and html reports here
  --fail-on impeached|uncorroborated|incomplete   non-zero exit when any row has this
                      verdict, or when any evidence channel was not intact
  --sensitive         refuse anything that would leave the machine; --model becomes an error
  --model CMD         opt-in extractor; runs CMD with a prompt on stdin, expects JSON claims
  --model-turns N     cap on assistant turns sent to --model (default 20)
  --adapter auto|claude-code   transcript adapter; auto detects from the checkpoint
  --no-rerun          skip graph verify
```

Exit codes: 0 completed; 2 the `--fail-on` condition was met; 1 runtime error.

## The 90-second demo

1. `entire impeach a1b2c3d4e5f6 --test "pytest -q --tb=no -rA"` on a checkpoint from the fixture app. The extra flags are required, not cosmetic: with a bare `pytest -q` the `graph verify` parser does not engage and the rerun degrades to an exit code with no test ids.
2. The table shows six rows. Row one, impeached: "All tests pass." Evidence: `pytest tests/test_api.py` ran at 10:42 (one of four test files, scope mismatch); `service.py` was edited at 10:51 and no test ran after that (stale); rerun now: three new failures, in `tests/test_service.py` and `tests/test_rounding.py`.
3. Row two, impeached: "No other callers are affected." Evidence: `graph impact` shows three callers of `compute_total`, and its signature changed.
4. Row three, corroborated: "Added `test_refund_rounding`." Evidence: `graph commit` lists it as an added function in `tests/test_refunds.py`.
5. Row four, uncorroborated: "Verified the migration path." No command or read event matches.
6. Unrequested section: `_legacy_shim` was added and no prompt mentions it.
7. Open the HTML report. The lead impeachment is the hero. Every row expands to the exact commands, timestamps and outputs behind it.
8. Closing beat: run Impeach on the checkpoint of the session that built Impeach.

## Scope

In v1:

- Claude Code transcripts (Entire's default agent; carries tool calls, results and timestamps).
- Four claim families with the pattern library above, plus unrequested symbols.
- Single checkpoint per run by default; `--session` loops.
- Table, JSON and HTML output; exit codes.
- Recorded fixtures and Go unit tests for every verifier; one end-to-end test that runs only when the Entire CLI is present.
- Static landing page with an embedded sample report.

Deliberately out of v1:

- Adapters for Codex, Cursor, Gemini, Copilot transcripts (Cursor transcripts do not carry detailed tool records at all; those checkpoints get unverifiable execution and reading rows, which is the correct answer).
- Subagent transcripts.
- A learned extractor. The model extractor is a thin opt-in and stays thin.
- Any UI beyond the static report and landing page.
- CI packaging. The exit code is the contract; the Action is continuation.
- Auto-fix of anything. Impeach reports; it never edits.

## Success criteria, mapped to judging

| Judging criterion | What Impeach shows |
|---|---|
| Defined user | The merger of agent PRs, named in the README's first paragraph |
| Real problem | Unverified agent claims; the demo's stale, scope-mismatched "tests pass" is the everyday case |
| Scope that finishes | Deterministic core, four families, one adapter; everything else listed as unbuilt |
| Inspectable core | Every verdict prints the command, timestamp and output it rests on; recorded fixtures let a judge replay without an agent |
| Disclosure | Prior-work and AI-generated-component sections in the README and this document |
| Continuation path | Exit codes for CI, adapter interface for other agents, extractor interface for a learned model |
| Entire is essential | Tool activity and prompts exist only in the checkpoint; entity changes and impact come only from Graph; remove either and Impeach has no record to compare against |

## Limitations to disclose

- Claim detection is pattern-based. Vaguely phrased claims are missed; missed claims are silent, not false.
- Result parsing knows pytest, go test, jest and cargo summaries. Other runners fall to "uncorroborated, output not parsed" unless an exit code is recoverable.
- "Backward compatible" is checked structurally (signature change with callers). Behaviour changes without a signature change are not detected.
- Unrequested matching is name-based and conservative: body-only changes are never flagged, so real unrequested behaviour can slip through inside an existing function.
- The fixture app is seeded to produce one of each verdict; that is stated in the report footer.
- One transcript adapter. Others degrade to unverifiable rows by design.

## Prior work and AI disclosure

- Concept lineage: the author's earlier cleanup of fabricated evidence in an incident-response agent environment (MIRR), where agent claims were audited by hand. No code is reused.
- All product code is written during the hackathon inside the fork, with Claude Code as the coding agent, captured in Entire checkpoints. The checkpoints branch is the disclosure.
- The pattern library, verifier rules and fixtures are hand-reviewed; anything model-generated is marked as such in the report's extractor column.

## Continuation path

1. `impeach-action`: a GitHub Action that fetches the checkpoints branch for the PR head and runs `entire impeach --fail-on impeached`.
2. Adapters for Codex and Gemini transcripts; a compliance fixture per agent.
3. Feed the JSON report into `entire review --prompt` so reviewer agents start from the impeachments.
4. A learned extractor trained on claims labelled by the deterministic verifiers.

## Cost

Entire CLI and Graph are open source. Go, Python, pytest and GitHub Pages are free. The default path makes no network calls and no model calls. `--model` accepts any local command (a subscription CLI, a free-tier CLI, or a local model runner); Impeach never holds keys and works fully without it.
