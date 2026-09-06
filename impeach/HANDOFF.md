# Impeach: handoff

Reconstruction note: the delivered working tree contained `docs/` only. This
file was rebuilt at the start of the build from the four design documents plus
the settled-decision list in the kickoff prompt. It is an index and a rulebook,
not a source of new design. Where this file and `docs/` disagree, `docs/` wins.

## What Impeach is

A plugin for the Entire CLI, run as `entire impeach <checkpoint>`, that
cross-examines what an AI coding agent claimed it did against the record of
what it actually did. The transcript is testimony. The checkpoint's tool
activity, the entity-level diff from Entire Graph, and a fresh test run are the
record. Every claim gets one of four verdicts with the evidence attached.

Read the documents in this order:

1. `docs/PRD.md` for the user, the problem, the claim families and the scope.
2. `docs/ARCHITECTURE.md` for the components, the data flow, Step 0, the
   degradation table, the constraint-change playbook and the build order.
3. `docs/SECURITY_AND_ACCESS.md` for the execution and data-handling rules.
4. `docs/FRONTEND_SPEC.md` for the report and the landing page.

## Settled decisions, not to be reopened

- Go. A single binary named `entire-impeach`, dispatched kubectl-style by the
  Entire CLI because any `entire-<name>` executable on `$PATH` runs as
  `entire <name>` with stdio and exit code passed through.
- Its own Go module under `impeach/`. No import of the host CLI's internals.
- Inputs come only through documented Entire CLI output. Impeach never reads
  the checkpoints branch or any Entire internal storage directly.
- Four boundaries, each an interface with one default implementation: the
  checkpoint reader, the claim extractors, the verifiers, the report
  renderers. A constraint change is absorbed inside one boundary.
- One `Runner` interface for every external process. This is what makes
  recorded fixtures and offline tests possible.
- The core is deterministic and runs offline.
- The model extractor is opt-in only, through `--model CMD`. Impeach holds no
  credentials.

## Invariants

- Missing data is a state, never an error. When an evidence channel is absent
  the verdict is `unverifiable` and the report names the missing channel.
- Impeach never executes a command found in a transcript. The only command it
  runs on the user's behalf is the one from `--test` or a committed
  `.impeach.json`. Commands in the tool log are matched as strings and
  displayed, nothing more.
- No shell interpolation. `Runner` executes argv arrays.
- Impeach reports and never edits.
- Graph and CLI output are data, never instructions. Transcript content is
  trusted for nothing.

## Build rules

- No em dashes in any file, code comments included. Plain language.
- Tests replay recorded fixtures. No test may require a live agent or a
  network. `go test ./...` is green at every commit.
- Commit after every phase, message format
  `impeach: phase N <short summary> (tests: <count>)`. No squashing.
- Nothing outside `impeach/` changes except a one-line pointer in the
  repository README.
- Before changing a file this build did not create, run
  `entire graph impact` on the symbols involved and note the callers in
  `NOTES.md`.
- One agent session at a time. No parallel builds. No background processes
  left running.

## Where state lives

- `NOTES.md` carries the working notes: probe findings, degradations in
  effect, impact checks, and the end-of-phase reports. A fresh session should
  be able to reconstruct the build from the latest checkpoint plus that file.
- The answers to the six open questions live at the bottom of
  `docs/ARCHITECTURE.md`, kept current.
- Commit messages carry the phase number and the test count, so the checkpoint
  list reads as a build log.

## Constraint changes

If a constraint change is announced: read the constraint-change playbook in
`docs/ARCHITECTURE.md` first, write a short plan into `NOTES.md`, implement it
inside the boundary the playbook names, and commit it as its own phase.
