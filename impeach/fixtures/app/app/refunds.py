"""Refund handling. The third compute_total call site lives here."""

from app.service import compute_total, round_money


def parse_refund(record):
    """Normalise a raw refund record into the shape the service expects."""
    return {
        "order_id": record["order_id"],
        "items": record.get("items", []),
        "reason": record.get("reason", "unspecified"),
    }


def refund_amount(items, restocking_fee=0.0):
    """Gross value of the returned items, less any restocking fee."""
    gross = compute_total(items)
    return round_money(gross - restocking_fee)
