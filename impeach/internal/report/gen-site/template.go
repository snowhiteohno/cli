package main

// indexTemplate is the landing page. One column, 880px, same tokens as the
// report, no external requests and no analytics.
//
// The page exists because reviewers rarely clone or run anything. The report
// is the product; this is the shortest honest path to seeing one.
const indexTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="generator" content="` + genMarker + `">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'self' 'unsafe-inline'; frame-src 'self'; img-src data:">
<title>Impeach: cross-examine what an agent said it did</title>
<link rel="stylesheet" href="style.css">
<style>
  .title { font-size: 40px; font-weight: 600; margin: 0 0 12px; padding: 0; border: 0; }
  dl { margin: 0; }
  dt { font-weight: 600; margin-top: 10px; }
  dd { margin: 2px 0 0 0; color: var(--ink-2); }
  iframe { width: 100%; height: 900px; border: 1px solid var(--rule); background: var(--paper); }
  ul { padding-left: 22px; }
  li { margin-bottom: 6px; }
  .lede { max-width: 72ch; }
</style>
</head>
<body>
<main>

<section>
  <h1 class="title">Impeach</h1>
  <p class="lede">Impeach cross-examines what an AI coding agent claimed it did against the
  record of what it actually did. It runs as a plugin for the Entire CLI, reads the
  checkpoint's tool log and the entity-level diff from Entire Graph, and gives every claim a
  verdict with the evidence attached.</p>
</section>

{{.Hero}}

<section>
  <p class="lede">This is a real report from the fixture app in this repository, produced by
  replaying a recorded audit with no network and no agent. The transcript said the tests
  passed, and it was telling the truth: one of four test files ran, it went green, and the
  file was edited into a state that breaks three other tests. The claim was accurate and the
  change was still broken. That is the everyday case.</p>
</section>

<section>
  <h2>What a verdict means</h2>
  <dl>
    <dt>Corroborated</dt>
    <dd>The record supports the claim, and the channel it rests on was intact.</dd>
    <dt>Impeached</dt>
    <dd>The record contradicts the claim, with a reason code: stale, scope mismatch,
    contradicted by output or rerun, callers exist, signature changed, not in diff,
    never read.</dd>
    <dt>Uncorroborated</dt>
    <dd>The evidence channel was readable and held nothing either way.</dd>
    <dt>Unverifiable</dt>
    <dd>The channel is missing, redacted or partial. Missing data is a state, never an
    error, and a redacted channel can never corroborate: what was removed could be exactly
    what would have contradicted the claim.</dd>
  </dl>
  <p class="lede">A fifth row type, unrequested, flags added or signature-changed symbols
  whose names appear in no prompt in the session. That is name matching only and never uses
  a model.</p>
</section>

<section>
  <h2>What gets checked</h2>
  <div class="scroll">
    <table>
      <caption>The four claim families and the evidence each rests on.</caption>
      <thead>
        <tr><th scope="col">Family</th><th scope="col">Example</th><th scope="col">Evidence</th></tr>
      </thead>
      <tbody>
        <tr>
          <td>Execution</td>
          <td class="mono">"All tests pass"</td>
          <td>Command events, their output and their order relative to the last edit, plus a baseline-aware rerun</td>
        </tr>
        <tr>
          <td>Structural</td>
          <td class="mono">"Added parse_refund"</td>
          <td>Entity-level changes from entire graph commit</td>
        </tr>
        <tr>
          <td>Safety</td>
          <td class="mono">"No other callers"</td>
          <td>Callers and signature changes from entire graph impact</td>
        </tr>
        <tr>
          <td>Reading</td>
          <td class="mono">"I reviewed the callers"</td>
          <td>File read events, matched against the caller files Graph found</td>
        </tr>
      </tbody>
    </table>
  </div>
</section>

<section>
  <h2>Install and run</h2>
  <div class="scroll"><pre>git clone https://github.com/snowhiteohno/cli.git
cd cli/impeach
mkdir -p ~/.local/share/entire/plugins/bin
go build -o ~/.local/share/entire/plugins/bin/entire-impeach ./cmd/entire-impeach</pre></div>
  <p class="lede">Requires the Entire CLI with checkpoints enabled and the entire-graph
  plugin. Works offline. No model calls unless you pass <span class="mono">--model</span>,
  and none at all if the repository commits
  <span class="mono">"sensitive": true</span>.</p>
</section>

<section>
  <h2>Reproduce the demo</h2>
  <div class="scroll"><pre>git clone https://github.com/snowhiteohno/cli.git &amp;&amp; cd cli
git fetch origin 'refs/entire/*:refs/entire/*'
entire impeach 01M1TET4N33VMY0DTHNZKV5HT9 --repo . --fail-on impeached
entire impeach 01M1TJCCYXR7ZZTK1H8167G249 --repo .</pre></div>
  <p class="lede">The fetch is not optional. Checkpoint refs live under
  <span class="mono">refs/entire/*</span>, outside git's default refspec, so a plain clone
  brings none of them and the audit has nothing to resolve. No
  <span class="mono">--test</span> is needed because the repository commits one.</p>
  <p class="lede">The first checkpoint carries the impeached row. The second shows the other
  three verdicts and the unrequested block; nothing is impeached on it because that agent was
  accurate, which is worth seeing too. The fixture app is seeded to exercise each verdict and
  every report says so in its footer.</p>
  <p class="lede">To reproduce with no Entire, no git and no agent at all:
  <span class="mono">cd impeach &amp;&amp; go test -count=1 ./...</span>. Every external call
  in three real audits is committed, so the whole pipeline replays offline.</p>
</section>

<section>
  <h2>The full sample report</h2>
  <p class="lede"><a href="sample/impeach.html">Open it in a new tab</a>, or read it below.
  It works with JavaScript disabled: every evidence panel is in the markup and open by
  default.</p>
  <iframe src="sample/impeach.html" title="Sample Impeach report" sandbox loading="lazy"></iframe>
</section>

<section>
  <h2>Limitations</h2>
  <ul>
    <li>Claim detection is pattern based. Vaguely phrased claims are missed. A missed claim
    is silent, not a false one, which is the deliberate direction to fail in.</li>
    <li>Six false positives were found and fixed during the build, every one of them by
    running the tool on real data rather than by reading the code. Two markdown headings
    read as claims, a colon lead-in impeached for a claim it never made, a compatibility
    claim impeached for having callers, and a verdict invented from an unrelated command.
    For a tool whose value is accuracy about other people's accuracy, a false accusation is
    the worst failure available, so they are listed rather than quietly patched.</li>
    <li>Result parsing knows pytest, go test, jest, cargo, rspec, phpunit, maven and gradle
    summaries. Others fall to uncorroborated rather than guessing.</li>
    <li>"Backward compatible" is checked structurally, as a signature change on a symbol
    with callers. A behaviour change behind a stable signature is not detected.</li>
    <li>Unrequested matching is name based and conservative. Body-only changes are never
    flagged, so unrequested behaviour inside an existing function slips through.</li>
    <li>One transcript adapter, claude-code. Other agents degrade to unverifiable rows by
    design, which is the truthful output rather than a failure.</li>
    <li>Secret scrubbing on evidence excerpts is pattern based and cannot catch every shape.
    Excerpts are bounded at 400 characters and no transcript is ever written to the output
    directory.</li>
    <li>Impeach cannot audit its own construction. The session that wrote it predates the
    git hooks, so its commits carry no checkpoint. That is disclosed rather than worked
    around.</li>
  </ul>
</section>

<footer>
  <p>Built on the Entire ecosystem. The source, the checkpoints and the session record are
  all in <a href="https://github.com/snowhiteohno/cli">the repository</a>. This page makes no
  network requests and carries no analytics.</p>
</footer>

</main>
</body>
</html>
`
