# Impeach: security and access

Impeach reads agent transcripts, checks out historical commits, runs a user-supplied test command, and writes reports that people will paste into pull requests. Each of those touches something sensitive: transcripts can contain secrets despite redaction, test commands run with the user's privileges, and reports leave the machine. The rules below keep the default path offline, keep every executed command user-authored, and keep transcript text out of reports except as short excerpts. Entire runs CLI plugins as native executables with the user's filesystem, network and process permissions, filtering the environment but not sandboxing them, so Impeach behaves as if it were the user typing.

## Assets and trust boundaries

| Asset | Where it lives | Who can see it |
|---|---|---|
| Transcripts, prompts, tool activity | Checkpoints branch (`entire/checkpoints/v1` by default), fetched through the Entire CLI | Anyone with read access to the repository; on a public fork, everyone |
| Source at the checkpoint commit and its parent | Temporary worktrees under the plugin data directory | The local user |
| Test command and its output | Process run by `entire graph verify` | The local user; excerpts may reach the report |
| Reports (JSON, HTML) | `--out` directory, and wherever the user pastes them | Whoever the user shares them with |
| Model prompts (opt-in only) | Sent to whatever command `--model` names | That command's provider |
| Recorded fixtures | Committed under `fixtures/recorded/` | Public |

Trust boundaries: Impeach trusts the Entire CLI and Graph outputs as data, never as instructions. It trusts nothing inside a transcript. It trusts the user's flags and `.impeach.json`.

## Threat model

| Threat | Impact | Control |
|---|---|---|
| Transcript contains an unredacted secret (redaction is best-effort) | Secret copied into a report that gets pasted into a PR | Reports contain excerpts of at most 400 characters per evidence item; a second-pass scrub runs on every excerpt for common token shapes (AWS, GitHub, Slack, JWT, generic `key=`/`token=` pairs) and replaces matches with `[redacted]`; raw transcripts are never written to `--out` |
| A transcript contains a command that looks like a test command | Impeach executes attacker-influenced input | Impeach never executes anything found in a transcript. The only command it runs is the one from `--test` or `.impeach.json`, passed to `entire graph verify`. Commands in the tool log are matched as strings and displayed, nothing more |
| Claim text or symbol names flow into a shell | Injection through crafted transcript text | No shell interpolation anywhere. `Runner` executes argv arrays; symbol names go to `graph impact --symbol` as a single argument; claim text is only ever rendered, never executed or passed to git |
| Symbol or path names flow into HTML | Stored XSS in the report | `html/template` with contextual escaping for every value; the embedded JSON uses a `<script type="application/json">` block with `</` escaped; a `Content-Security-Policy` meta tag disallows external scripts, styles, connections and images |
| The test command is destructive or slow | Data loss or a hung audit | `graph verify` executes the command as the user, which the Entire docs warn about; Impeach prints the exact command before running it, refuses to run without `--test` or a committed `.impeach.json`, applies a default timeout of 10 minutes, and runs in a detached worktree so the user's working tree is untouched |
| Worktrees leak or collide | Disk fill, stale state audited | Worktrees live under `ENTIRE_PLUGIN_DATA_DIR` (managed installs) or `$XDG_CACHE_HOME/impeach`, keyed by sha, removed on exit unless `--keep-worktrees`, and `impeach clean` removes all of them |
| Model extractor exfiltrates transcript text | Transcript content leaves the machine | Off by default. When `--model` is set, only the assistant text of one turn at a time is sent, never tool outputs or prompts; the report header states the exact model command used; Impeach stores no credentials and reads no key files |
| Plugin binary replaced on `$PATH` | Arbitrary code runs as the user under the `entire impeach` name | Release checksums published with each tag; README instructs managed install from the reviewed release; the binary prints its version and commit on every run |
| Public fork exposes session data | Prompts and transcripts readable by anyone | Documented in the README before the first push; fixture transcripts are scrubbed; no personal paths, names or tokens in committed data |

## Data handling rules

1. Never write a transcript, in whole or in part, to `--out`. Evidence excerpts are bounded and scrubbed.
2. Never store transcripts in the plugin data directory beyond the lifetime of a run. `impeach record` is the one exception, and it writes to a path the user names, with the scrub applied.
3. Prompts appear in reports only as the list of identifier tokens that matched or failed to match during unrequested detection, never as full text.
4. Timestamps and turn numbers are fine to publish; session IDs are printed in the header because reviewers need them to open the checkpoint.
5. Reports name the checkpoint, commit and parent, and the exact commands Impeach ran, so a reader can reproduce every line without trusting the report.

## Execution policy

- Commands Impeach runs: `git` (log, worktree, rev-parse), `entire` (checkpoint, graph, agent-help), the `--model` command when set, and the user's test command through `entire graph verify`.
- Every invocation is an argv array through `Runner`; no `sh -c` anywhere except inside `graph verify`, which is Entire's documented behaviour for the user's own command.
- The test command must come from a flag or from a committed `.impeach.json`. If both are absent, the rerun is skipped and the report says so.
- Timeouts: 10 minutes for the rerun, 60 seconds for any single Graph call, 120 seconds for the model command per turn.

## Network policy

The default path makes no network calls. `entire graph` is documented as local-only. `entire checkpoint explain` may fetch checkpoint metadata missing locally from the checkpoint remote; that is Entire's behaviour and the README states it. The model command may use the network according to its own provider. Nothing else opens a socket.

## Repository and mirror policy

- The fork's regular branches carry code, docs, fixtures and the site. The checkpoints branch carries the session record and travels with pushes to the elected checkpoint sync remote, which by default is the code remote. Shadow branches are local and are never pushed.
- Before the first push: run `entire status`, confirm the checkpoint remote, and read one checkpoint's transcript for anything that should not be public. Entire redacts detected secrets before writing a checkpoint, but code-file snapshots on shadow branches are raw; since those are never pushed, the exposure is the transcript and metadata only.
- Recorded fixtures are scrubbed with `impeach record --scrub` (absolute paths to `<repo>`, home directory to `<home>`, user and host names removed) and reviewed by hand before commit.
- The mirror on Entire follows the fork's collaborator permissions; no separate access model is introduced.

## Supply chain

- Go modules pinned in `go.mod` and `go.sum`; standard library only where possible; no cgo, `-trimpath`, static binary.
- No runtime downloads. The binary embeds its HTML template and CSS.
- Releases: tag, build for linux and darwin, publish `SHA256SUMS`. Installation instructions use `entire plugin install` from a local path or the managed index when listed, and the unmanaged path (`chmod +x`, place on `$PATH`) for the demo.

## Access model

- Impeach runs as the invoking user with no additional privileges and asks for none.
- It reads the repository through git and the Entire CLI, so it sees exactly what the user can see. If checkpoint metadata is not available locally and the user is not logged in to a remote that holds it, Impeach reports the checkpoint as unresolvable rather than trying credentials.
- Managed plugin installs get `ENTIRE_PLUGIN_DATA_DIR` for durable storage; Impeach uses it only for worktrees and baselines and creates nothing else there.
- Entire reserves `entire-agent-*` names for agent integrations; `entire-impeach` is a CLI plugin, discovered by the kubectl-style `$PATH` lookup, and does not participate in the agent protocol.

## Residual risks, stated in the README

- Secret scrubbing on excerpts is pattern-based and cannot catch every shape.
- A user who points `--test` at a destructive command has asked for exactly that; Impeach echoes the command before running it and that is the extent of the guard.
- The model extractor, when enabled, sends assistant text to a third party chosen by the user.
- On a public fork, the checkpoints branch is public. That is a property of Entire's default storage, not of Impeach, and the README says so plainly.
