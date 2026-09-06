# Recorded scenarios

Each directory here is one real audit, captured call by call. A file `<n>.json`
holds one external command: its name, its argv, the hash of anything sent to
stdin, and what came back. Replaying a directory runs the whole Impeach
pipeline with no Entire, no git, no agent and no network.

This works because of one design decision. The `Runner` interface has no
working-directory field: every command Impeach runs carries its own path
argument (`git -C`, `graph --repo`), so a call is fully described by its name
and argv, and a replay is exact rather than approximate.

## Provenance

Both scenarios were recorded from real captured agent sessions in this
repository, with `entire impeach <checkpoint> --record <dir>`. Nothing here is
hand-written.

| Scenario | Checkpoint | Commit | What it shows |
|---|---|---|---|
| `rerun-regression` | `01M1TET4N33VMY0DTHNZKV5HT9` | `458bb14` | The impeached row. An agent switched `round_money` to banker's rounding, ran only `tests/test_api.py`, and reported 4 passed. A fresh run against a baseline from the parent finds three genuine new failures, so the claim is impeached with `contradicted-rerun`. |
| `discount-rename` | `01M1TJCCYXR7ZZTK1H8167G249` | `0bd3033` | The other three verdicts and the unrequested block. An agent added an order-level discount and renamed `line_subtotal`. It was accurate, so nothing is impeached; the rows are corroborated, uncorroborated and unverifiable, with three unrequested test symbols. |
| `redacted-toollog` | `01M1TET4N33VMY0DTHNZKV5HT9` | `458bb14` | **Derived, not recorded.** A copy of `rerun-regression` with every `Bash` tool result replaced by a redaction marker, standing in for a transcript that secret redaction has passed over. See its `PROVENANCE.md`. |

## Replaying one

The flags have to match the ones the scenario was recorded with, because they
decide which calls the pipeline makes. The exact `--test` string is pinned in
`cmd/entire-impeach/replay_test.go` as `recordedTestCommand`.

```
entire impeach 01M1TET4N33VMY0DTHNZKV5HT9 --repo <repo-root> \
  --test "<the recorded test command>" \
  --replay impeach/fixtures/recorded/rerun-regression
```

`ENTIRE_PLUGIN_DATA_DIR` matters. These were recorded while running as an
installed plugin, so the worktree paths sit under the plugin data directory.
The replay tests pin it; a manual replay should too, or the worktree calls
will not match.

## Scrubbing

Recordings are committed, so `--record` scrubs them by default: the repository
path becomes `<repo>`, the home directory `<home>`, the user name `<user>`, and
every value goes through the same credential scrub the reports use. Replay
applies the identical transformation to each live lookup key, which is what
keeps a portable recording matchable. Without that symmetry the fixtures would
be write-only.

Checked before commit: no absolute paths, no user names, no token-shaped
strings. The only address that remains is the constant
`noreply@anthropic.com` co-author trailer.

## What is not here

Three of the five scenarios the architecture lists, `stale`, `scope` and
`safety`, are absent. They need an agent that overclaims, and both sessions
recorded here were accurate: they scoped their claims to the files they had
actually run and volunteered what they had not. Those verdicts are covered by
unit tests against hand-built records in `internal/verify`, which is where the
`stale` and `scope` logic is pinned. Adding them here would mean instructing an
agent to make a false statement, and a fabricated impeachment in the fixtures
of a tool about false claims is not a trade worth making.
