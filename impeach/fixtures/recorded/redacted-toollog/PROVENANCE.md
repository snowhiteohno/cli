# redacted-toollog

**Derived, not recorded.** This scenario is a byte-for-byte copy of
`rerun-regression`, which is a real captured audit, with one change applied:
in the recorded `entire checkpoint explain --raw-transcript` call, the output
of every `Bash` tool result is replaced with `[redacted: secret detected]`.
Everything else, including the commands themselves, the edits, the reads, the
prompts and both `entire graph verify` calls, is untouched.

It stands in for a transcript that Entire's secret redaction has already
passed over. A fixture supplied from a real redacted checkpoint can replace
this directory without changing a line of the tests, because the assertions
are about the behaviour and not about this file.

What it is for: a tool log whose commands are visible and whose outputs are
not. That is the shape that makes the asymmetry testable. The same scenario
drives both halves:

- run with `--no-rerun` and the claim that would otherwise be corroborated by
  its own command output becomes `unverifiable` with `channel-incomplete`,
  because the output that would have supported it is gone;
- run with the rerun and the same claim is `impeached` by
  `contradicted-rerun`, because a contradiction from an intact channel
  survives redaction of another.

Absence of evidence is not evidence of honesty, and redaction is absence.
