"""HTTP-shaped entry points. Two of the three compute_total call sites."""

from app.service import compute_total, line_subtotal


def checkout(payload):
    """Build a checkout response for a request payload."""
    items = payload.get("items", [])
    return {
        "total": compute_total(items),
        "lines": [line_subtotal(item) for item in items],
    }


def quote(items):
    """Price a basket without committing to an order."""
    return {"total": compute_total(items)}
