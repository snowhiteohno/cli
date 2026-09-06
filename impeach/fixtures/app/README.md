# Impeach fixture app

A deliberately small Python service used as the demo target for Impeach. It
exists so every verdict Impeach can produce has something real to rest on.

Layout:

- `app/service.py` holds `compute_total`, `round_money` and `line_subtotal`.
  `compute_total` has three call sites, which is what makes a "no other
  callers" claim checkable.
- `app/api.py` calls `compute_total` from `checkout` and `quote`.
- `app/refunds.py` calls `compute_total` from `refund_amount`.
- `tests/` holds four pytest files, so running one of them is a scope mismatch
  against a claim that all tests pass.

Setup:

```
sh scripts/setup.sh
./.venv/bin/python -m pytest -q
```

State of the suite, which matters because a fixture that lies about itself is
useless for a tool about claims:

- At the phase 0 commit the whole suite is green, 18 passed. That commit is
  the baseline the rerun records.
- At the probe commit `458bb14` the suite has three failures, in
  `tests/test_rounding.py` and `tests/test_service.py`. They are deliberate.
  That commit switched `round_money` to banker's rounding while running only
  `tests/test_api.py`, which is the situation Impeach is built to catch, so it
  is the checkpoint the demo audits.

So a plain `pytest -q` on a current checkout shows three failures. That is the
seeded state, not a broken fixture. Check out the phase 0 commit to see the
green baseline.

When running this suite through `entire graph verify`, use
`pytest -q --tb=no -rA`. A bare `-q` prints no per-test ids, so the verify
parser cannot engage and the result degrades to an exit code.
