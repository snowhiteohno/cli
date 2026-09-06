# Impeach: frontend specification

Two surfaces, both static. The HTML report is one self-contained file that `entire impeach` writes next to the JSON: no build step, no external requests, the report data embedded inline, readable offline and in print. The landing page is a single page on GitHub Pages that explains Impeach in two sentences, shows the demo as an embedded sample report, and gives install and reproduce commands. The report is the product; the page exists because reviewers rarely clone or run anything. Both use the same tokens so a screenshot of one reads as the other.

## Design direction

The subject is a cross-examination: testimony on one side, the record on the other. The design should read like a well-kept case file, not a dashboard. Text does the work; colour is spent on verdicts and nowhere else. One memorable element per surface: on the report it is the lead impeachment, typeset as testimony followed by the record; on the landing page it is the same block, live, from the sample report.

Things this design does not do: no cream-and-terracotta palette, no near-black with a neon accent, no gradient washes, no identical rounded cards, no all-caps eyebrow labels, no numbered markers except where content is a sequence (the evidence timeline is one), no middle-dot separators, no arrows appended to links, no page-load animations.

### Tokens

Colour (light):

- `--paper: #FFFFFF` page
- `--ink: #1C1B1A` text
- `--ink-2: #5B5854` secondary text
- `--rule: #D9D6D0` borders and table rules
- `--corroborated: #1F6B3A`
- `--impeached: #8C1D18`
- `--uncorroborated: #4A5568`
- `--unverifiable: #8A8F98` (rendered with a hatched background, since colour alone must not carry meaning)
- `--focus: #1A3D6B` links and focus rings

Colour (dark, via `prefers-color-scheme: dark`):

- `--paper: #17191C`, `--ink: #E8E6E1`, `--ink-2: #A9A6A0`, `--rule: #33363B`
- verdict colours lightened one step: `#4FA86E`, `#D9534F`, `#9AA5B8`, `#8A8F98`, focus `#7FA6DE`

Type (system stacks only, because the report must not fetch anything):

- Body and headings: `ui-sans-serif, system-ui, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif`
- Testimony, commands, paths, symbol names: `ui-monospace, "SF Mono", Menlo, Consolas, "Liberation Mono", monospace`
- Scale: 15px body, 1.55 line-height; 13px for table cells and evidence; 22px for section headings; the lead testimony at 28px monospace with 1.3 line-height. Weight is 400 everywhere except 600 for verdict words and section headings.
- Measure: body text capped at 72 characters; tables may exceed it.

Layout:

- Single column, 880px max width, left-aligned, 24px page padding, 48px between sections.
- Headings sit flush left with a 1px `--rule` underline that spans the column; that rule is the only decoration.
- Verdict colour appears as: the verdict word itself, a 4px left border on the row's expanded evidence panel, and the counts in the summary strip. Never as a fill behind text.

## Surface 1: the HTML report

File: `impeach.html`, produced by `html/template` from the same struct that produces `impeach.json`. The JSON is embedded as `<script type="application/json" id="impeach-data">` with `</` escaped, so a reader can copy the data out of the page. A `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; img-src data:">` tag sits in the head. Inline CSS and a small inline script (under 120 lines) handle expand and filter. The page works with scripts disabled: all evidence panels are present in the markup and open by default without JS; the script collapses them and adds the filter.

### Structure, top to bottom

1. Header line. Plain text: `Impeach 0.1.0 report for checkpoint a1b2c3d4e5f6 (commit 3f9c2e1, parent 8b1d0a7), session 2 of 2, agent claude-code.` Second line: `Adapter claude-code. Extractors: pattern. Test command: pytest -q. Rerun: yes. Model command: none.` When a model command was used, this line names it verbatim.

2. Lead impeachment (the hero). Only rendered when at least one impeached claim exists; otherwise the section reads `No claim was impeached.` in the heading style. Content:

   - Testimony (label in `--ink-2`, sentence case): the claim quoted in 28px monospace, with the turn number after it in small type: `"All tests pass."  turn 7`.
   - Record (same label style): a short timeline, numbered because it is a sequence, in 15px:
     1. `10:42  pytest tests/test_api.py  →  4 passed  (1 of 4 test files)`
     2. `10:51  edited app/service.py`
     3. `10:53  commit 3f9c2e1  (no test run after 10:51)`
     4. `now    rerun pytest -q  →  2 new failures in tests/test_service.py`
   - Verdict line: `Impeached: scope mismatch, stale.` with "Impeached" in `--impeached` at 600 weight.

   Selection rule for the lead: the impeached claim with the most reasons; ties broken by rerun new failures, then by earliest turn.

3. Summary strip. One row of five counts as plain text separated by two spaces of whitespace, not dots: `1 corroborated   2 impeached   1 uncorroborated   0 unverifiable   1 unrequested`. Each count is a filter toggle (button element, styled as text with an underline on hover and a visible focus ring). Active filters are shown by the count word gaining its verdict colour; "show all" resets.

4. Claims table. Columns: Verdict, Family, Claim, Reason, Rerun, Turn. Rows sorted impeached first, then uncorroborated, unverifiable, corroborated; within a group by turn. The Claim cell is monospace, wrapped, never truncated in HTML (truncation is for the terminal only). The Reason cell lists reason codes as readable phrases (`scope mismatch`, `stale`, `callers exist`). Rerun shows `pass`, `2 new failures`, `not run`, or `skipped`. Every row is a `<details>` element: the `<summary>` is the row, and the expanded body is the evidence panel.

5. Evidence panel (inside each row). Left border 4px in the verdict colour. Contents:
   - One-sentence summary in body type.
   - Evidence items as a list, each with its type in `--ink-2` and its content in monospace: commands with a 400-character output excerpt in a `<pre>` (scrolls horizontally, never wraps), edits and reads as paths with seq and time, entity changes as `kind name (file:line)`, impact as `3 callers (2 direct, 1 transitive), signature changed`, rerun as the test IDs that changed state.
   - The exact command a reader can run to see the same evidence, for example `entire graph impact --symbol compute_total --repo <worktree>`.

6. Unrequested changes. A short table: Symbol, File, Kind, Dependents, Severity, plus a `Prompt tokens searched` line under the table listing the identifier tokens that were looked for, so a reader can see why the match failed. If empty: `Every added or signature-changed symbol was named in a prompt.`

7. Footer. Three short paragraphs in `--ink-2`: limitations pulled from the JSON (`limitations` array as one sentence each); reproduction (`Commands run:` followed by the `commands_run` list in monospace); a single line `Report generated by entire impeach; nothing in this file was produced by a model unless the header names a model command.`

### States

- No claims found: the table section reads `No claims matched the pattern library. Run with --model CMD to add an extractor, or read the transcript with entire checkpoint explain <id> --full.`
- Channel missing: the header adds a line such as `This transcript carries no tool records; execution and reading claims are unverifiable.` and those rows show `unverifiable` with the channel named in Reason.
- Rerun not run: the Rerun column shows `not run` and the footer says why (`--no-rerun`, no test command, or verify failed with its stderr excerpt).
- Session mode: one report section per checkpoint, separated by a heading with the checkpoint id and commit; the summary strip and lead impeachment apply to the whole session.

### Interaction

- Rows expand and collapse with native `<details>`; the script only adds "expand all" and "collapse all" text buttons above the table.
- Filters are additive toggles; keyboard reachable; `aria-pressed` reflects state.
- Nothing animates except the native disclosure.

### Accessibility and print

- All colour-coded verdicts also appear as the verdict word; unverifiable rows carry the hatched background so the four states are distinguishable in greyscale.
- Focus rings visible (`outline: 2px solid var(--focus); outline-offset: 2px`).
- Table has a caption (`Claims extracted from the agent's transcript and their verdicts`), `scope="col"` headers, and the evidence panels are `<details>` so screen readers announce expanded state.
- `@media print`: expand all details, hide the filter buttons, set the column to 100% width, keep verdict colours, avoid page breaks inside a row.
- Reduced motion is respected trivially because there is no motion.

## Surface 2: the landing page

File: `site/index.html`, published with GitHub Pages from the fork (Settings, Pages, deploy from `main`, folder `/impeach/site`). Same tokens, same type stacks. May use `site/style.css` as a separate file; still no external requests, no fonts, no analytics.

Sections and their exact copy (edit freely, keep the shape):

1. Title block. `Impeach` at 40px, then one paragraph: `Impeach cross-examines what an AI coding agent claimed it did against the record of what it actually did. It runs as a plugin for the Entire CLI, reads the checkpoint's tool log and the entity-level diff from Entire Graph, and gives every claim a verdict with the evidence attached.`

2. The lead impeachment, live. The same hero block as the report, rendered from `site/sample/impeach.json` at build time (copy the report's HTML for that block into the page; do not fetch at runtime). Under it, one sentence: `This is a real report from the fixture app in this repository. The transcript said the tests passed. The record shows one of four test files ran, and the file was edited afterwards.`

3. What a verdict means. Four short definitions in a two-column definition list: corroborated, impeached, uncorroborated, unverifiable. Then one line about unrequested symbols.

4. What gets checked. The claim-family table from the PRD, trimmed to Family, Example, Evidence.

5. Install and run. Three commands in one `<pre>`:
   ```
   go install github.com/<owner>/cli/impeach/cmd/entire-impeach@main
   entire impeach HEAD --test "pytest -q" --out ./impeach-out
   open ./impeach-out/impeach.html
   ```
   Followed by: `Requires the Entire CLI with Checkpoints enabled and the entire-graph plugin. Works offline. No model calls unless you pass --model.`

6. Reproduce the demo. Four commands: clone the fork, `cd impeach/fixtures/app`, the checkpoint id of the committed demo scenario, the `entire impeach` line. State that the fixture is seeded to produce one of each verdict.

7. Full sample report. An `<iframe>` of `site/sample/impeach.html` with `sandbox` set (no scripts needed for reading, since the report works without JS), height 900px, plus a plain link to open it in a new tab.

8. Limitations. The PRD's list, verbatim, as a bulleted list.

9. Footer. `Built during Bengaluru Tech Week on the Entire ecosystem. Source, checkpoints and the session record are in the repository.` with a link to the fork. No badges, no logos.

Page width 880px, left-aligned, one column throughout. The hero block is the only large type on the page.

## Build notes

- Report template: `internal/report/report.html.tmpl`, embedded with `embed.FS`. CSS in `internal/report/report.css`, inlined at render time. Script in `internal/report/report.js`, inlined.
- The landing page copies the report's CSS at build time via a tiny `go run ./internal/report/gen-site` step that also renders the sample report from the committed fixture JSON, so the two surfaces cannot drift.
- All values rendered through `html/template` contextual escaping. The embedded JSON goes through a function that replaces `</` with `<\/` and `<!--` with `<\!--`.
- No external URLs in the report at all. The landing page links only to the repository and to the sample report.

## QA checklist

- Report opens from `file://` with scripts disabled and shows every evidence panel.
- Report opens with scripts enabled, panels collapsed, filters work by keyboard.
- Dark mode via OS setting renders all four verdicts legibly; greyscale print shows the hatch on unverifiable rows.
- No network requests in the browser's network tab for either surface.
- Lighthouse accessibility score at or above 95 on both surfaces.
- A claim text containing `<script>` and a symbol named `</script>` render as text.
- The landing page's hero matches the sample report byte for byte after `gen-site`.
- Narrowest supported width 360px: the table scrolls horizontally; the hero wraps; nothing overflows the page.
