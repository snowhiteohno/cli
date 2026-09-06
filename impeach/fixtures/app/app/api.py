"""HTTP-shaped entry points. Two of the three compute_total call sites."""

from app.service import compute_total, line_total


def checkout(payload):
    """Build a checkout response for a request payload.

    An optional "discount" key on the payload is an order-level currency
    amount; absent it, the order is priced undiscounted. The per-line values
    under "lines" are pre-discount, since an order-level discount does not
    belong to any one line.
    """
    items = payload.get("items", [])
    discount = payload.get("discount", 0.0)
    return {
        "total": compute_total(items, discount=discount),
        "lines": [line_total(item) for item in items],
    }


def quote(items, discount=0.0):
    """Price a basket without committing to an order."""
    return {"total": compute_total(items, discount=discount)}
