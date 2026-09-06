#!/usr/bin/env bash
# Show the checkpoint evidence: what Entire captured while this was built,
# and what one checkpoint actually contains.
#
# Read only. Runs nothing that writes to the repository.
#
#   sh impeach/scripts/demo-checkpoints.sh            # the default walkthrough
#   sh impeach/scripts/demo-checkpoints.sh <id>       # drill into one checkpoint
set -u

ENTIRE=${ENTIRE:-$(command -v entire || echo ./entire)}
CP=${1:-01M1TET4N33VMY0DTHNZKV5HT9}
CURVEBALL=01M1TR5GCVA7ZE650KSRY2G35S
FORK_POINT=3dbdf8b

step() { printf '\n\033[1m== %s\033[0m\n' "$1"; }
run()  { printf '\033[2m$ %s\033[0m\n' "$*"; "$@"; }

step "1. How many checkpoints exist, and where they live"
printf '\033[2m$ git ls-remote origin "refs/entire/checkpoints/*" | wc -l\033[0m\n'
git ls-remote origin 'refs/entire/checkpoints/*' | wc -l | tr -d ' '
echo "checkpoints pushed to the fork, under refs/entire/checkpoints/**"
echo "A plain clone does not fetch these. That is what the README's"
echo "  git fetch origin 'refs/entire/*:refs/entire/*'"
echo "line is for."

step "2. Every commit of the build, against the checkpoint it carries"
run git log --format='%h  %<(58)%s  %(trailers:key=Entire-Checkpoint,valueonly,separator=%x20)' "$FORK_POINT"..HEAD

step "3. The list Entire itself gives"
printf '\033[2m$ %s checkpoint list --json | head -30\033[0m\n' "$ENTIRE"
"$ENTIRE" checkpoint list --json 2>/dev/null | head -30

step "4. What one checkpoint contains: session, agent, model, files, tokens"
run "$ENTIRE" checkpoint explain "$CP" --json

step "5. The same checkpoint, human readable"
printf '\033[2m$ %s checkpoint explain %s --no-pager | head -40\033[0m\n' "$ENTIRE" "$CP"
"$ENTIRE" checkpoint explain "$CP" --no-pager 2>/dev/null | head -40

step "6. The transcript inside it, which is the testimony every claim comes from"
printf '\033[2m$ %s checkpoint explain %s --raw-transcript | head -c 600\033[0m\n' "$ENTIRE" "$CP"
"$ENTIRE" checkpoint explain "$CP" --raw-transcript 2>/dev/null | head -c 600
printf '\n[truncated]\n'

step "7. The Curveball checkpoint, which is a build moment rather than a demo one"
printf '\033[2m$ %s checkpoint explain %s --no-pager | head -12\033[0m\n' "$ENTIRE" "$CURVEBALL"
"$ENTIRE" checkpoint explain "$CURVEBALL" --no-pager 2>/dev/null | head -12

step "The gap to be honest about"
cat <<'TXT'
There is no initial-intent checkpoint. The git hooks were installed part way
through the build, so phase 0 through phase 5 predate capture: nothing in that
range resolves with `entire checkpoint explain`. The commit messages, NOTES.md
and docs/ carry that reasoning instead, and back-dating one would be the exact
class of unbacked claim this tool exists to catch.
TXT
