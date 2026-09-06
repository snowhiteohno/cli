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

The suite is green as committed. Individual fixture scenarios seed specific
failures; each one says so where it is recorded.
