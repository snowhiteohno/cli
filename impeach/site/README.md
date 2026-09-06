# The landing page

`index.html`, `style.css`, `site.js` and `fonts/` are hand written. They are
not generated.

`sample/impeach.html` and `sample/impeach.json` are inputs. They come from a
real audit and must not be edited by hand.

## One coupling you need to know about

`internal/report/gen-site` still renders an `index.html` of its own from a Go
template. **Running it will overwrite this page** with the older generated
version. It is left alone here because the rebuild brief put everything outside
`impeach/site/` out of scope, so its template could not be updated in the same
change.

Until that is resolved, use gen-site only to refresh the sample report and the
stylesheet copy, not the index:

```
# safe: regenerates sample/ from a real run
entire impeach 01M1TET4N33VMY0DTHNZKV5HT9 --repo . --setup "" \
  --test 'cd impeach/fixtures/app && ./.venv/bin/python -m pytest -q --tb=no -rA' \
  --replay impeach/fixtures/recorded/rerun-regression --out /tmp/sample

# then copy /tmp/sample/impeach.{html,json} into site/sample/ by hand
```

## What the tests still guarantee

`internal/report/site_test.go` checks, on every Pages deploy:

- the report's `<section id="lead">` block appears in `index.html` byte for
  byte, which is why that block is embedded here as the no-JavaScript and
  screen-reader layer;
- `site/style.css` resolves every shared token to the same value as
  `internal/report/report.css`, so the page and the report cannot disagree
  about what a colour means;
- no script or resource is loaded from another host;
- no absolute filesystem path appears anywhere in the committed site.

Two of those assertions were rewritten during the rebuild. The stylesheet
check used to require byte identity with `report.css`, which stopped making
sense once the page had its own type and layout, and the script check used to
forbid script elements outright, which stopped making sense once the hero
became a WebGL scene. Both were narrowed to the property that actually
matters rather than deleted.

## Do not strip the trailing whitespace

`git diff --check` reports whitespace diagnostics in `index.html` and
`sample/impeach.html`. Leave them. The five in `index.html` are inside the
`<section id="lead">` block, which is byte-identical to the sample report and
tested for it, and the ones in `sample/impeach.html` are in a generated audit
artifact. `report.html.tmpl` has none of its own: this is Go template
execution leaving the indentation of its actions behind. Stripping either file
breaks the drift test or edits evidence to please a linter.

## The hero content

The testimony, the record lines and which line contradicts are hardcoded in
`index.html`, matching `sample/impeach.json`. They should be read from that
file at build time instead, which needs `gen-site`, which is out of scope
above. TODO, tracked in the same place as the coupling note.
