# Impeach: technical architecture

Impeach is a single static Go binary named `entire-impeach`, living in the fork of `entireio/cli` as its own module under `impeach/`, and dispatched by the Entire CLI as `entire impeach` because any executable named `entire-<name>` on `$PATH` runs kubectl-style with stdio and exit code passed through. It consumes documented Entire CLI output only and never reads the checkpoints branch directly. Four boundaries carry the design: a checkpoint reader, claim extractors, verifiers, and report renderers. Each is an interface with one default implementation, so a constraint change is absorbed inside one boundary. All external commands go through a single `Runner` interface, which is what makes recorded fixtures and offline tests trivial.

## Components

```
impeach/
  go.mod                         separate module; no import of cli internals
  cmd/entire-impeach/main.go     flag parsing, wiring, exit codes
  internal/runner/               Runner interface: exec, and a replaying fake for tests
  internal/checkpoint/           resolve id <-> commit, fetch transcript/prompts via entire CLI
  internal/transcript/           normalized Event model; claudecode adapter (JSONL)
  internal/record/               worktrees, graph commit/impact/diff, verify rerun, result parsers
  internal/claims/               Claim model; pattern extractor; model extractor (opt-in)
  internal/verify/               one verifier per family; unrequested detector
  internal/report/               table, json, html renderers
  fixtures/app/                  tiny Python service with four pytest files (demo target)
  fixtures/recorded/<scenario>/  recorded inputs for replay tests
  site/                          static landing page (GitHub Pages)
  testdata/                      transcript slices, graph outputs, verify outputs
```

## Data flow

```
entire impeach <ref> --test "pytest -q"
   │
   ├─ 1. Resolve       ref -> checkpoint id + commit sha + parent sha
   ├─ 2. Testimony     entire checkpoint explain <id> --raw-transcript  -> adapter -> []Event
   │                   entire checkpoint explain <id> --full            -> prompts (fallback: Prompt events)
   ├─ 3. Record        git worktree add (head, base)
   │                   entire graph commit <sha>            -> entity changes
   │                   entire graph impact --symbol S       -> callers, consumers, signature info
   │                   entire graph verify --test CMD       -> rerun with baseline from base worktree
   ├─ 4. Extract       pattern extractor over AssistantText events -> []Claim
   │                   (optional) model extractor -> more []Claim, tagged
   ├─ 5. Verify        verifier per family: Claim + Record -> Verdict
   │                   unrequested detector: entity changes + prompts -> []Unrequested
   └─ 6. Render        table to stdout; json + html to --out; exit code
```

### 1. Resolve

Accept a 12-hex checkpoint ID, a prefix, or a commit-ish.

- From commit-ish: `git log -1 --format=%B <ref>` and parse the `Entire-Checkpoint: <id>` trailer.
- From checkpoint ID: `git log --all --grep="Entire-Checkpoint: <id>" --format=%H`; if several, take the newest and record the ambiguity in the report.
- Cross-check with `entire checkpoint list --json` when available; if the JSON exposes the commit, prefer it. The trailer path is the fallback that needs nothing beyond git.
- Parent: `<sha>^`. Merge commits are out of scope for v1; report and continue with the first parent.

### 2. Testimony

`entire checkpoint explain <id> --raw-transcript` returns the stored transcript (JSONL). `--full` returns the parsed transcript, `--short` the summary. The adapter boundary owns the format.

Normalized event model (`internal/transcript`):

```go
type Kind int // Prompt, AssistantText, ToolCall, ToolResult, FileRead, FileEdit, Command

type Event struct {
    Seq    int          // stable ordering even when timestamps are missing
    TS     time.Time    // zero when the transcript has none
    Kind   Kind
    Turn   int          // assistant turn index, for "as of turn N" reasoning
    Text   string       // Prompt, AssistantText
    Call   *ToolCall    // ToolCall
    Result *ToolResult  // ToolResult
    Path   string       // FileRead, FileEdit
    Cmd    *Command     // Command: Cmd, Output, ExitKnown, ExitCode, Targets []string
}

type Adapter interface {
    Name() string
    Detect(raw []byte) bool
    Parse(raw []byte) ([]Event, error)
}
```

Claude Code adapter mapping (verify against a real file before trusting):

- Records are JSONL objects with `type` (`user`, `assistant`, `system`, `summary`), `timestamp`, and `message.content` as an array of blocks: `text`, `tool_use` (`id`, `name`, `input`), `tool_result` (`tool_use_id`, `content`, sometimes `is_error`).
- `Read`, `Glob`, `Grep` tool calls become `FileRead` (Grep and Glob yield the matched paths from the result when present, else the `path` input).
- `Edit`, `Write`, `MultiEdit`, `NotebookEdit` become `FileEdit` with `input.file_path`.
- `Bash` becomes `Command`; the paired `tool_result` supplies `Output`. Exit codes are usually not explicit; `ExitKnown` is false unless the output carries an "exit code" marker. `Targets` are parsed from the command line (paths, `-k`, `::` selectors, package patterns).
- Subagent records, when present, are ignored in v1 and counted in the report header ("N subagent records not examined").
- Redacted spans (Entire's secret redaction replaces matches before write) pass through as text; the adapter never tries to reconstruct them.

Prompts come from `Prompt` events; if `--full` exposes a cleaner prompt list, prefer it.

### 3. Record

Worktrees, because Graph and the rerun must see the checkpoint's commit, not today's HEAD:

```
git worktree add --detach <data>/wt/<sha>/head <sha>
git worktree add --detach <data>/wt/<sha>/base <sha>^
```

`<data>` is `ENTIRE_PLUGIN_DATA_DIR` when Entire sets it (managed plugin), else `$XDG_CACHE_HOME/impeach`. Worktrees are removed on exit unless `--keep-worktrees`.

Graph calls (all with `--repo <worktree>`):

- `entire graph commit <sha>`: entity-level changes relative to the first parent (added, removed, renamed, signature-changed, body-changed) with dependent counts. Preferred source for structural changes.
- `entire graph diff --base <sha>^ --head <sha>`: the same information between refs; used when `commit` output is not parseable.
- `entire graph impact --symbol <S> --exclude-tests` and without the flag: callers (direct and transitive), callees, type consumers, data flows, co-change files. Run once per changed symbol named in a safety claim, plus once per added symbol for the unrequested severity.
- `entire graph verify --repo <base> --test "<cmd>" --record-baseline <data>/baseline.json` then `entire graph verify --repo <head> --test "<cmd>" --pre-edit-baseline <data>/baseline.json`: tests that changed state, so pre-existing failures are not blamed on the checkpoint.
- Optional warm-up: `entire graph index --repo <head>` before the batch.

Output shapes: Graph text output is documented; JSON formats exist for `search`, `snapshot`, `symbols` and `edges`. For `commit`, `impact` and `verify`, check `--help` on the installed binary for a `--format json` or `--json` flag. If absent, parse the text (the documented example for `impact` is stable enough: an `Impact:` line, a `Blast radius:` line, then sections). Parsers live in `internal/record/parse_*.go` with fixtures, so a format change is a one-file fix. Last resort for structural changes: `entire graph symbols --repo <base>` and `--repo <head>` as NDJSON, diffed by stable symbol identity inside Impeach.

### 4. Extract

```go
type Family int // Execution, Structural, Safety, Reading

type Claim struct {
    ID        string
    Text      string   // exact quoted sentence
    Family    Family
    Subject   Subject  // symbol name, file path, or scope word set
    Turn      int
    Seq       int      // event seq of the AssistantText it came from
    Extractor string   // "pattern" or "model:<cmd>"
}

type Extractor interface {
    Extract(events []Event) ([]Claim, error)
}
```

Pattern extractor: sentence-split each `AssistantText`, then match a family's pattern set. Patterns are anchored on verbs and objects, not on tone. Starter set (regular expressions with named groups; keep them in `patterns.go` with a table test each):

- Execution: `(all|the|every)\s+tests?\s+(now\s+)?(pass|passing|green)`, `test suite (passes|is green)`, `(ran|run)\s+(the\s+)?tests?`, `build (succeeds|passes|is clean)`, `lint(er)?\s+(passes|is clean)`, `\d+ tests? passed`. Scope words captured: `all|entire|full|whole` -> ALL; `for (?P<subject>[\w./]+)` -> SUBSET(subject); else UNSPECIFIED.
- Structural: `(added|created|introduced)\s+(a\s+)?(function|method|class|type|helper|test)?\s*` + backticked or CamelCase/snake_case identifier; `(removed|deleted)\s+...`; `renamed\s+X\s+to\s+Y`; `(added|wrote)\s+tests?\s+for\s+X`.
- Safety: `no other (callers|call sites|usages)`, `nothing else (calls|uses)`, `only (used|called) (in|by)`, `backward[s]? compatible`, `no behaviou?r change`, `(fully|completely)? isolated`, `safe to (merge|change)`. Subject: identifier in the sentence, else the changed symbols in the same turn's edits.
- Reading: `(reviewed|checked|inspected|looked at|read|examined)\s+(the\s+)?(callers|call sites|tests|existing tests|`path`|identifier)`.

Model extractor (opt-in, `--model CMD`): runs `CMD` through the Runner with a prompt on stdin containing the assistant text of one turn and a strict instruction to return a JSON array `[{"text","family","subject"}]`. Anything that is not valid JSON is dropped with a warning. Claims are tagged `model:<cmd>` and go through the same verifiers. Works with any local command: a subscription CLI in print mode, a free-tier CLI, or a local runner. No key handling in Impeach.

### 5. Verify

```go
type Status int // Corroborated, Impeached, Uncorroborated, Unverifiable

type Verdict struct {
    ClaimID  string
    Status   Status
    Reason   string      // reason code, empty for corroborated
    Summary  string      // one plain-English sentence
    Evidence []Evidence  // typed: command, edit, read, entity, impact, rerun
    Rerun    RerunResult // Pass, NewFailures, NotRun, Skipped
}

type Verifier interface {
    Family() Family
    Verify(c Claim, r *Record) Verdict
}
```

Verifier matrix:

| Family | Evidence channel | Corroborated | Impeached (reason) | Uncorroborated | Unverifiable |
|---|---|---|---|---|---|
| Execution | Command events + parsed outputs; rerun | matching command after last relevant edit, scope satisfied, output success | output failure (`contradicted-output`); relevant edit after last run (`stale`); command scope narrower than claim (`scope-mismatch`); rerun shows new failures and claim says pass (`contradicted-rerun`) | no matching command; output unparsed | transcript has no Command events (adapter says tool records absent) |
| Structural | Entity changes | named entity present with claimed kind | named entity absent and its file was parsed by Graph (`not-in-diff`) | entity name could not be resolved (ambiguous) | file's language not parsed by Graph |
| Safety | impact + signature flags | zero callers outside changed set; or no signature change when claim is compatibility | callers exist for "no callers" claims (`callers-exist`); signature changed with callers for compatibility claims (`signature-changed`) | subject symbol not identified | Graph cannot resolve the symbol |
| Reading | FileRead events, impact caller files | named file read; or at least one caller file read for "reviewed callers" | zero matching reads (`never-read`) | claim names nothing resolvable | transcript has no read events |

Execution verification, step by step:

1. Relevant files = files with `FileEdit` events in the session that also appear in the checkpoint's changed-file list.
2. Candidate commands = `Command` events whose `Cmd` matches a test-runner pattern (`pytest`, `go test`, `npm test`, `jest`, `cargo test`, `make test`, or the configured `--test` command). Same idea for build and lint claims with their own pattern sets.
3. Supporting command = the last candidate command before the checkpoint commit whose parsed output is a success (pytest summary with `failed` absent and `passed` present; `go test` with `ok` lines and no `FAIL`; jest `Tests: ... passed`; cargo `test result: ok`). Unparsed output means the command is not supporting, and the verdict falls to uncorroborated unless another command supports it.
4. Staleness: if any `FileEdit` on a relevant file has `Seq` greater than the supporting command's `Seq`, the claim is impeached with `stale`. Ordering uses `Seq`, so missing timestamps do not break it; timestamps are displayed when present.
5. Scope: classify the claim (ALL, SUBSET(x), UNSPECIFIED) and the supporting command (FULL if it has no path, `-k`, `::`, `-m`, or package filter beyond the repository root, else SUBSET(targets)). ALL vs SUBSET -> `scope-mismatch`. SUBSET(x) vs SUBSET(targets not covering x's test file, resolved by name and by `graph impact` on x) -> `scope-mismatch`. UNSPECIFIED vs SUBSET -> corroborated, summary notes "supported for the subset only".
6. Rerun: when `--test` is set and `--no-rerun` is not, run `graph verify` with baseline. New failures with a pass claim -> `contradicted-rerun`, in addition to any earlier reason. The rerun column is always filled independently of the verdict.

Unrequested detector:

1. Candidates = entities from `graph commit` with kind added or signature-changed. Body-only changes are never candidates.
2. Mention corpus = all `Prompt` texts, lowercased, plus identifiers extracted from them.
3. An entity is mentioned if its exact name appears, or if all of its identifier tokens (split on case and underscores, dropping tokens shorter than three characters and a small stopword list) appear in the corpus, or if its file's basename is named in a prompt.
4. Unmentioned candidates are reported with severity from `graph impact` dependent counts (none, low, high) and tagged `test` when they live in a test file.

### 6. Render

- Table: one row per claim, columns Verdict, Family, Claim (truncated to 80 chars), Reason, Rerun. Then the unrequested block. Then a one-line summary with counts.
- JSON: the full report schema below, written to `--out/impeach.json`.
- HTML: a single self-contained file (`--out/impeach.html`) rendered from `html/template` with the JSON embedded; see FRONTEND_SPEC.md.

```json
{
  "impeach_version": "0.1.0",
  "checkpoint": {"id": "a1b2c3d4e5f6", "commit": "…", "parent": "…", "session_ids": ["…"], "agent": "claude-code"},
  "inputs": {"adapter": "claude-code", "extractors": ["pattern"], "test_command": "pytest -q", "rerun": true,
             "channels": {"commands": true, "reads": true, "graph": true}},
  "claims": [
    {"id": "c1", "text": "All tests pass.", "family": "execution", "turn": 7, "extractor": "pattern",
     "verdict": "impeached", "reasons": ["scope-mismatch", "stale"],
     "summary": "One of four test files ran, and service.py was edited afterwards without a rerun.",
     "evidence": [
       {"type": "command", "seq": 41, "ts": "…", "cmd": "pytest tests/test_api.py", "output_excerpt": "4 passed", "parsed": "pass"},
       {"type": "edit", "seq": 58, "ts": "…", "path": "app/service.py"},
       {"type": "rerun", "status": "new_failures", "tests": ["tests/test_service.py::test_rounding", "…"]}
     ],
     "rerun": "new_failures"}
  ],
  "unrequested": [
    {"symbol": "_legacy_shim", "file": "app/service.py", "kind": "added", "severity": "low", "dependents": 0}
  ],
  "counts": {"corroborated": 1, "impeached": 2, "uncorroborated": 1, "unverifiable": 0, "unrequested": 1},
  "limitations": ["pattern-based extraction", "fixture seeded for demo"],
  "commands_run": ["entire checkpoint explain a1b2c3d4e5f6 --raw-transcript", "entire graph commit …", "…"]
}
```

Exit codes: 0 completed; 2 `--fail-on` condition met; 1 runtime error (missing Entire CLI, unresolvable checkpoint, worktree failure).

## The Runner boundary and recorded fixtures

Every external process (git, entire, the model command, the test command via verify) goes through:

```go
type Runner interface {
    Run(ctx context.Context, name string, args []string, stdin []byte) (stdout, stderr []byte, exit int, err error)
}
```

`impeach record <ref> --out fixtures/recorded/<scenario>/` runs a real audit and writes every Runner call as `<n>.json` (name, args, stdin hash, stdout, stderr, exit). The replaying fake serves those by matching name and args. Tests build a `Record` and run the full pipeline against fixtures with no Entire, no git, no agent.

Scenarios recorded from the fixture app (`fixtures/app`, a small Python service with `app/service.py`, `app/api.py`, `app/refunds.py` and four pytest files):

1. `honest`: full suite run after the last edit, claims match; everything corroborated.
2. `stale`: suite run, then one more edit to `service.py`, claim repeated; `stale`.
3. `scope`: only `tests/test_api.py` run, "all tests pass" claimed; `scope-mismatch`.
4. `safety`: signature of `compute_total` changed, "no other callers" claimed; `callers-exist`, `signature-changed`.
5. `unrequested`: `_legacy_shim` added without any prompt mentioning it.

Fixture transcripts are scrubbed before commit (absolute paths replaced with `<repo>`, no user names, no tokens).

Unit tests: pattern extractor table tests (each pattern has positive and negative sentences); scope classifier; output parsers for each runner; staleness ordering; each verifier against hand-built `Record`s; unrequested tokenizer. End-to-end test: builds the binary, runs it against `fixtures/app` only when `entire` and `entire-graph` are on `$PATH`, otherwise `t.Skip`.

## Step 0: the go/no-go probe

Before any product code, in the fork with Entire enabled and Graph installed:

1. Run a short agent session on `fixtures/app`: read two files, edit one, run `pytest tests/test_api.py`, commit.
2. `entire checkpoint list --json` and note the fields. `entire checkpoint explain <id> --raw-transcript > /tmp/t.jsonl`. `entire checkpoint explain <id> --full > /tmp/t.txt`. `entire agent-help checkpoint explain --json` for the installed flags.
3. Inspect `/tmp/t.jsonl` for: (a) `tool_use` blocks with `name` and `input`, (b) `tool_result` blocks paired by id with output content, (c) `Read`/`Grep` inputs carrying file paths, (d) timestamps.
4. `entire graph commit HEAD --repo .` and `entire graph impact --symbol <edited function> --repo .`; check for a JSON flag in `--help`.
5. `entire graph verify --repo . --test "pytest -q"` once, to see the output shape.

Degradations, decided by the probe:

| Finding | Effect | Where absorbed |
|---|---|---|
| Tool calls present, results absent | Execution claims cannot be corroborated by output; they become uncorroborated unless `--test` rerun corroborates or contradicts | Execution verifier (rerun becomes primary evidence) |
| No read events with paths | Reading claims unverifiable; unrequested and everything else unaffected | Reading verifier |
| No timestamps | Ordering by `Seq`; report shows turn numbers instead of times | Adapter |
| `--raw-transcript` unavailable, `--full` only | Adapter parses the parsed-transcript text; format known in one file | Adapter |
| `graph commit` has no JSON | Text parser with fixture; fallback to `symbols` NDJSON diff | Record parsers |
| Graph does not parse the fixture language | Switch fixture app to Go or Python (both are in Graph's semantic language set); structural rows unverifiable for unparsed files | Fixture choice |

## Constraint-change playbook

Likely attacks on the design, and where each is absorbed:

- Audit a checkpoint from another agent: new `Adapter`. If the transcript carries no tool records (Cursor), execution and reading rows are unverifiable, which is the truthful output.
- No model calls permitted: already the default; `--model` is opt-in and the report header states whether it was used.
- Audit the whole session, not one checkpoint: `--session` loops the per-checkpoint pipeline and concatenates sections; unrequested matching uses the union of prompts.
- Output for a machine consumer: JSON and exit codes exist from the first commit.
- Redacted or missing transcript: unverifiable rows with the channel named; unrequested and structural rows still run from Graph and prompts if prompts survive; if prompts are missing too, unrequested is skipped and the report says so.
- Must work on a repository without a test command: `--no-rerun`; execution claims rest on the tool log alone.
- Must handle a merge commit: first-parent diff, flagged in the header.
- Must run inside CI: the binary is static, needs git, entire and the graph plugin; document the fetch of the checkpoints branch; nothing else changes.

## Build order

Phases, each ending in a commit that is a valid checkpoint. No clock; the order is what matters.

1. Docs and probe. Commit the four documents. Run Step 0. Record findings at the bottom of this file.
2. Skeleton. Module, `main.go` with flags, `Runner`, resolve by trailer, worktree add/remove. Test: resolve from trailer.
3. Adapter. Claude Code JSONL to `[]Event`. Test: parse the probe transcript.
4. Record. `graph commit` parser, `impact` parser, `verify` wrapper with baseline. Tests with recorded outputs.
5. Execution verifier and pattern extractor for execution claims. The `stale` and `scope` scenarios must pass. First end-to-end table output. Stable checkpoint.
6. Structural, safety, reading verifiers and their patterns. Unrequested detector. Scenarios `safety`, `unrequested`, `honest`.
7. JSON report and exit codes. `impeach record` and the replaying fake; commit the five recorded scenarios; replay tests green.
8. HTML report. Stable checkpoint.
9. `--session`, `--model` extractor, `--adapter auto`.
10. Landing page with the embedded sample report; README with install, reproduce, limitations, disclosure.
11. Final semantic-diff review: `entire graph diff --base <first commit> --head HEAD`, its conclusions checked against source and `go test ./...`, written into the README's verification section. Run Impeach on the checkpoint of the session that built it and ship that report.

## Reconstruction checklist

A fresh agent session, given only `entire checkpoint explain <latest>`, `entire graph commit <latest>` and this `docs/` directory, must be able to state:

- the user and the problem (PRD, first two sections);
- the four boundaries and which one owns the current task (this file, Components and Data flow);
- which scenarios pass and which do not (`go test ./...` output recorded in the latest commit message);
- what is deliberately unbuilt (PRD, Scope);
- the probe findings and any degradation in effect (bottom of this file).

Every commit message states the phase number and the test count, so the checkpoint list reads as a build log.

## Open questions

Fill these in after Step 0 and keep them current.

- Does `entire checkpoint list --json` expose the linked commit sha and session ids?
- Does `entire checkpoint explain` have a machine-readable flag in the installed version, or is `--raw-transcript` the only structured path?
- Do `tool_result` blocks in the stored transcript carry full command output, truncated output, or none?
- Do `graph commit`, `graph impact` and `graph verify` have JSON output in the installed plugin version?
- Does `graph commit` report signature changes distinctly from body changes for Python?
- Is `ENTIRE_PLUGIN_DATA_DIR` set for unmanaged plugins, or only for managed installs?
