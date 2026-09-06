from app.service import compute_total, line_total, round_money


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


def test_line_total():
    assert line_total({"price": 3.33, "qty": 3}) == 9.99


def test_compute_total_applies_discount_before_tax():
    items = [{"price": 10.0, "qty": 2}]
    assert compute_total(items, discount=5.0) == 16.2


def test_compute_total_discount_exceeding_subtotal_floors_at_zero():
    items = [{"price": 10.0, "qty": 2}]
    assert compute_total(items, discount=50.0) == 0.0


def test_compute_total_zero_discount_matches_undiscounted():
    items = [{"price": 10.0, "qty": 2}]
    assert compute_total(items, discount=0.0) == compute_total(items)


def test_round_money_half_up():
    assert round_money(1.005) == 1.01
