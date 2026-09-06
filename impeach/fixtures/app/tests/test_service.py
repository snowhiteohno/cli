from app.service import compute_total, line_subtotal, round_money


def test_compute_total_applies_tax():
    items = [{"price": 10.0, "qty": 2}]
    assert compute_total(items) == 21.6


def test_compute_total_multiple_lines():
    items = [{"price": 5.0, "qty": 3}, {"price": 2.5, "qty": 2}]
    assert compute_total(items) == 21.6


def test_compute_total_empty_basket():
    assert compute_total([]) == 0.0


def test_compute_total_zero_tax():
    items = [{"price": 10.0, "qty": 2}]
    assert compute_total(items, tax_rate=0.0) == 20.0


def test_line_subtotal():
    assert line_subtotal({"price": 3.33, "qty": 3}) == 9.99


def test_round_money_half_up():
    assert round_money(1.005) == 1.01
