# Showing the Entire usage

A runbook for demonstrating this build on the Entire ecosystem: which Entire
features it uses, and the command that proves each one. Every output below was
captured from a real run in this repository, at `8401484e` unless stated.

For the record itself, the checkpoint table and the Graph review of the whole
build, see [`../BUILDATHON.md`](../BUILDATHON.md). This file is the live
walkthrough, so it holds commands and their output rather than a second copy of
the facts.

## Two scripts, if you would rather not type

```
sh impeach/scripts/demo-checkpoints.sh     # what Entire captured, and what is inside one checkpoint
sh impeach/scripts/demo-graph.sh           # entity diff, caller impact, the rerun, and the build review
```

Timed on this machine: the checkpoints one takes 4 seconds, the graph one 43,
almost all of which is Graph building its index on the first `impact` call.
Warm that first and it drops to about 12.

Both echo each command before running it, so the screen shows what is being
asked as well as the answer. Both are read only, apart from `graph verify`
inside the second, which runs the fixture's tests in a detached worktree under
Entire's own plugin data directory. Either takes one argument to point it at a
different checkpoint or commit.

## The short version, if there is time for one thing

```
entire impeach 01M1TET4N33VMY0DTHNZKV5HT9 --repo . --fail-on impeached
```

Five and a half seconds, offline, no model call. It prints one impeached row,
the evidence under it, and then a **`Commands run:`** ledger naming every
Entire and git command the audit used. That ledger is the demonstration: the
tool does not describe its use of Entire, it lists it, at the bottom of every
report it produces.

Exit code 2, because `--fail-on impeached` found one.

The second checkpoint shows the other verdicts:

```
entire impeach 01M1TJCCYXR7ZZTK1H8167G249 --repo .
```

2 corroborated, 1 uncorroborated, 1 unverifiable, 3 unrequested. All four
verdicts plus unrequested symbols are demonstrable across the two, and the
reason they are not in one table is worth saying out loud: the agent under
audit was honest on the second one, so there was nothing to impeach.

## The four Entire features, and the command for each

### 1. Checkpoints, as the record of what an agent did

```
entire checkpoint explain 01M1TET4N33VMY0DTHNZKV5HT9 --json
```

```json
{
  "checkpoint_id": "01M1TET4N33VMY0DTHNZKV5HT9",
  "strategy": "manual-commit",
  "files_touched": ["impeach/fixtures/app/app/service.py"],
  "session_count": 1,
  "sessions": [
    {
      "session_id": "1c439358-7307-4721-8422-949e897dbfba",
      "agent": "Claude Code",
      "model": "claude-opus-5",
      "created_at": "2026-09-06T04:14:50.654932Z",
      "files_touched": ["impeach/fixtures/app/app/service.py"],
      "token_usage": {
        "input_tokens": 14,
        "output_tokens": 1783,
        "cache_read_tokens": 436469,
        "cache_creation_tokens": 56001
      }
    }
  ]
}
```

This is where the audit starts: which session, which agent, which files. There
is no commit sha in it, which is why checkpoint-to-commit resolution goes
through the `Entire-Checkpoint:` trailer instead.

### 2. The transcript, as testimony

```
entire checkpoint explain 01M1TET4N33VMY0DTHNZKV5HT9 --raw-transcript
```

The agent's own words, which is what every claim is extracted from. Worth
saying while it scrolls: this is the only input Impeach treats as untrusted.
Transcript content is matched as strings and displayed, never executed.

### 3. Entire Graph, for the entity-level diff

```
entire graph commit 458bb142 --repo . --json
```

```json
{
  "files": [
    {
      "path": "impeach/fixtures/app/app/service.py",
      "status": "M",
      "language": "Python",
      "changes": [
        {
          "type": "body_changed",
          "kind": "function",
          "name": "round_money",
          "dependents_count": 8
        }
      ]
    }
  ]
}
```

A line diff says a file changed. This says *`round_money`'s body changed, and 8
things depend on it*, which is what makes a structural claim checkable.

### 4. Entire Graph, for impact

```
entire graph impact --repo . --symbol compute_total --format json --exclude-tests
```

Three callers, each with a call site: `checkout` and `quote` in `api.py`, and
`refund_amount` in `refunds.py`. This is how "no other callers are affected"
gets a verdict instead of a nod. Note the first run takes ~32s to build the
index and is fast afterwards, so warm it up before presenting.

### 5. Entire Graph, for the test rerun

```
entire graph verify --repo <worktree> --test "<cmd>" --record-baseline <path>
entire graph verify --repo <worktree> --test "<cmd>" --pre-edit-baseline <path>
```

Two calls, on two detached worktrees: the parent commit for the baseline, the
checkpoint's commit for the rerun. Baseline-aware, so a test that was already
failing before the agent touched anything is not counted against it. On demo
one this produces the impeached row: 3 new failures against a transcript that
said 4 passed.

Every report prints the exact reproduce command, so any row can be re-run by
hand.

### 6. The plugin dispatch itself

```
entire impeach --version
```

There is no `impeach` command in the Entire CLI. Any `entire-<name>` binary on
`$PATH` runs as `entire <name>` with stdio and exit code passed through, which
is how this ships as a single Go binary that never imports the host CLI.

## The checkpoint evidence

30 checkpoints are pushed to the fork as `refs/entire/checkpoints/**`, one for
every commit from `ab8ba0ee` onward. Enumerate them against their commits:

```
git log --format='%h %s %(trailers:key=Entire-Checkpoint,valueonly)' 3dbdf8b..HEAD
```

The four moments, the honest gap included, are tabulated in
[`../BUILDATHON.md`](../BUILDATHON.md#checkpoint-links-and-what-each-checkpoint-proves).
The one to be ready to answer for: **there is no initial-intent checkpoint**,
because the git hooks were installed part way through the build, so phases 0 to
5 predate capture. The commit messages, `NOTES.md` and `docs/` carry that
reasoning instead. Back-dating one would be the exact class of unbacked claim
this tool exists to catch.

Any single checkpoint opens with:

```
entire checkpoint explain 01M1TR5GCVA7ZE650KSRY2G35S    # the Curveball response
entire checkpoint list --json | head -40                 # the whole list
```

## Graph on the build itself

The last review was Graph turned on this build, rather than on the fixture:

```
entire graph diff --base 3dbdf8b --head HEAD --json
```

At `a58889a8`: 147 files touched, 138 parsed, 9 unsupported, 1796 entity
changes, **every one of them `added`**. Not one `removed`, `renamed`,
`signature_changed` or `body_changed`. Two independent sources agree that
exactly one pre-existing file was touched: Graph reports `README.md` with a
single `added section 'Impeach'`, and git reports it as the only non-added path
in the range, at 4 insertions and 0 deletions.

That is the strongest available statement about whether a plugin built inside
someone else's repository disturbed it, and it is the answer to the obvious
question about working in a fork of the host CLI.

## Before presenting

```
# 1. The binary the CLI will dispatch to
cd impeach && go build -o ~/.local/share/entire/plugins/bin/entire-impeach ./cmd/entire-impeach

# 2. Warm the graph index, so the 32s first run does not happen on stage
entire graph impact --repo . --symbol compute_total --format json --exclude-tests >/dev/null

# 3. Warm the fixture virtualenv, which graph verify needs in its worktrees
sh impeach/fixtures/app/scripts/setup.sh 2>/dev/null || true

# 4. Confirm both demos still pass
entire impeach 01M1TET4N33VMY0DTHNZKV5HT9 --repo . --fail-on impeached ; echo "exit $?"
entire impeach 01M1TJCCYXR7ZZTK1H8167G249 --repo .
```

Expect exit 2 on the first and exit 0 on the second. If a rerun reports
`skipped` rather than a pass or fail, the fixture virtualenv is missing from
the detached worktree; the committed `.impeach.json` at the repository root
carries a `setup` command that builds it, so run through that rather than
passing `--test` by hand.

## If the live run fails

Point at the landing page, which is a real report from this repository and
needs nothing installed:

<https://snowhiteohno.github.io/cli/>

Its hero block is byte-identical to `impeach/site/sample/impeach.html`, checked
in the test suite and verified against the deployed page, so what is on screen
is the same evidence the tool produced rather than a mock-up of it.
