"""Core pricing logic for the demo service.

This module is the subject of most fixture scenarios. compute_total has three
call sites across api.py and refunds.py, which is what makes a "no other
callers" claim checkable.
"""

from decimal import ROUND_HALF_UP, Decimal

TAX_RATE = 0.08


def round_money(amount):
    """Round a currency amount to two decimal places, half away from zero.

    Goes through Decimal on the shortest repr of the float, so 1.005 rounds to
    1.01 rather than following binary floating point down to 1.00.
    """
    return float(Decimal(str(amount)).quantize(Decimal("0.01"), rounding=ROUND_HALF_UP))


def line_subtotal(item):
    """Price times quantity for a single line item, rounded."""
    return round_money(item["price"] * item["qty"])


def compute_total(items, tax_rate=TAX_RATE):
    """Sum the line items and apply tax.

    items is a sequence of mappings with "price" and "qty" keys.
    """
    subtotal = sum(item["price"] * item["qty"] for item in items)
    return round_money(subtotal * (1 + tax_rate))
