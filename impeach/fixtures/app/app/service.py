"""Core pricing logic for the demo service.

This module is the subject of most fixture scenarios. compute_total has three
call sites across api.py and refunds.py, which is what makes a "no other
callers" claim checkable.
"""

from decimal import ROUND_HALF_EVEN, Decimal

TAX_RATE = 0.08


def round_money(amount):
    """Round a currency amount to two decimal places, half to even.

    Uses banker's rounding, the convention accounting systems normally follow:
    a value exactly halfway between two cents goes to the even cent, so 1.005
    rounds to 1.00 and 1.015 rounds to 1.02. Goes through Decimal on the
    shortest repr of the float so the halfway case is seen as a true halfway
    case rather than following binary floating point.
    """
    return float(Decimal(str(amount)).quantize(Decimal("0.01"), rounding=ROUND_HALF_EVEN))


def line_total(item):
    """Price times quantity for a single line item, rounded."""
    return round_money(item["price"] * item["qty"])


def compute_total(items, tax_rate=TAX_RATE, discount=0.0):
    """Sum the line items, apply an order-level discount, then apply tax.

    items is a sequence of mappings with "price" and "qty" keys.

    discount is a currency amount taken off the whole order, not a rate and
    not a per-line adjustment. It is applied before tax, which is the order
    accounting systems expect: tax is owed on what the customer actually
    pays. A discount larger than the subtotal floors the taxable amount at
    zero rather than producing a negative total.
    """
    subtotal = sum(item["price"] * item["qty"] for item in items)
    discounted = max(subtotal - discount, 0.0)
    return round_money(discounted * (1 + tax_rate))
