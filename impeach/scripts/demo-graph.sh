#!/usr/bin/env bash
# Show what Entire Graph gives that a line diff cannot: entity-level changes,
# caller impact, a baseline-aware test rerun, and a structural review of the
# whole build.
#
# Read only, except that `graph verify` runs the fixture's test command in a
# detached worktree under Entire's own plugin data directory.
#
#   sh impeach/scripts/demo-graph.sh
#
# Warm the index first, or step 2 pays ~32s on stage:
#   entire graph impact --repo . --symbol compute_total --format json --exclude-tests >/dev/null
set -u

ENTIRE=${ENTIRE:-$(command -v entire || echo ./entire)}
DEMO_COMMIT=${1:-458bb142}
FORK_POINT=3dbdf8b

step() { printf '\n\033[1m== %s\033[0m\n' "$1"; }
run()  { printf '\033[2m$ %s\033[0m\n' "$*"; "$@"; }
have_python() { command -v python3 >/dev/null 2>&1; }

step "0. Which Graph is answering"
run "$ENTIRE" graph --version

step "1. The entity-level diff: not 'a file changed' but 'this function changed'"
run "$ENTIRE" graph commit "$DEMO_COMMIT" --repo . --json
echo
echo "body_changed on round_money, with dependents_count 8. A line diff would"
echo "have said service.py was modified and stopped there."

step "2. Caller impact: what 'no other callers are affected' has to survive"
printf '\033[2m$ %s graph impact --repo . --symbol compute_total --format json --exclude-tests\033[0m\n' "$ENTIRE"
if have_python; then
  "$ENTIRE" graph impact --repo . --symbol compute_total --format json --exclude-tests 2>/dev/null \
    | python3 -c '
import json,sys
d=json.load(sys.stdin)
f=d["focus"]
print("focus    %s  %s:%s" % (f["name"], f["file_path"], f["start_line"]))
c=d["callers"]
print("callers  %d total, %d direct" % (c["total"], c["direct"]))
for e in c["entries"]:
    ep=e["endpoint"]
    cs=e.get("call_site") or {}
    print("         %-16s %s:%s" % (ep["name"], cs.get("file_path","?"), cs.get("line","?")))
'
else
  "$ENTIRE" graph impact --repo . --symbol compute_total --format json --exclude-tests 2>/dev/null | head -c 800
fi
echo
echo "Three callers, each with a call site. That is a verdict instead of a nod."
echo "Callees are deliberately not used: Graph resolved the Python builtin sum"
echo "to a Go function in the host repo, so nothing is built on them."

step "3. The baseline-aware rerun, which is what produces the impeached row"
cat <<'TXT'
Two calls on two detached worktrees, the parent for the baseline and the
checkpoint's own commit for the rerun:

  entire graph verify --repo <parent-worktree> --test "<cmd>" --record-baseline   <path>
  entire graph verify --repo <head-worktree>   --test "<cmd>" --pre-edit-baseline <path>

Baseline-aware matters: a test already failing before the agent touched
anything must not be counted against it. Impeach builds both worktrees itself
and prints the exact reproduce command for every row, so any verdict can be
re-run by hand.
TXT
echo
echo "Seen end to end, with the verdict it produces:"
run "$ENTIRE" impeach 01M1TET4N33VMY0DTHNZKV5HT9 --repo . --fail-on impeached
echo "exit $?  (2 is correct here: --fail-on impeached found one)"

step "4. Graph turned on this build, rather than on the fixture"
printf '\033[2m$ %s graph diff --base %s --head HEAD --json\033[0m\n' "$ENTIRE" "$FORK_POINT"
if have_python; then
  "$ENTIRE" graph diff --base "$FORK_POINT" --head HEAD --json 2>/dev/null \
    | python3 -c '
import json,sys,collections
d=json.load(sys.stdin)
files=d["files"]
w=d.get("warnings") or []
types=collections.Counter(c["type"] for f in files for c in (f.get("changes") or []))
status=collections.Counter(f.get("status") for f in files)
print("files touched      %d" % len(files))
print("files parsed       %d" % (len(files)-len(w)))
print("unsupported        %d   (go.mod, the HTML template, two fonts, five fixtures)" % len(w))
print("entity changes     %d" % sum(types.values()))
print("change types       %s" % dict(types))
print("file statuses      %s" % dict(status))
for f in files:
    if f.get("status") == "M":
        ch = ", ".join("%s %s %s" % (c["type"], c["kind"], c["name"]) for c in (f.get("changes") or []))
        print("only pre-existing file touched: %s -> %s" % (f.get("path"), ch))
'
else
  echo "(python3 not found; run the raw command and read the JSON)"
fi
echo
echo "Every change is 'added'. Two independent sources have to agree on that,"
echo "so cross-check Graph against git rather than taking it on trust:"
printf '\033[2m$ git diff --name-status %s..HEAD | grep -v "^A"\033[0m\n' "$FORK_POINT"
git diff --name-status "$FORK_POINT"..HEAD | grep -v '^A'
printf '\033[2m$ git diff --numstat %s..HEAD -- README.md\033[0m\n' "$FORK_POINT"
git diff --numstat "$FORK_POINT"..HEAD -- README.md
echo
echo "One pre-existing file, one added section, 4 insertions, 0 deletions."
echo "Both sources agree, so a plugin built inside the host CLI's own fork"
echo "disturbed exactly one line of it."
