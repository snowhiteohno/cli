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
