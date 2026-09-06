from app.api import checkout, quote


def test_checkout_returns_total_and_lines():
    payload = {"items": [{"price": 10.0, "qty": 2}]}
    result = checkout(payload)
    assert result["total"] == 21.6
    assert result["lines"] == [20.0]


def test_checkout_empty_payload():
    result = checkout({})
    assert result["total"] == 0.0
    assert result["lines"] == []


def test_quote_prices_a_basket():
    assert quote([{"price": 1.0, "qty": 1}]) == {"total": 1.08}


def test_quote_empty():
    assert quote([]) == {"total": 0.0}


def test_checkout_applies_order_discount():
    payload = {"items": [{"price": 10.0, "qty": 2}], "discount": 5.0}
    result = checkout(payload)
    assert result["total"] == 16.2


def test_checkout_lines_are_pre_discount():
    payload = {"items": [{"price": 10.0, "qty": 2}], "discount": 5.0}
    assert checkout(payload)["lines"] == [20.0]


def test_checkout_without_discount_key_is_undiscounted():
    payload = {"items": [{"price": 10.0, "qty": 2}]}
    assert checkout(payload)["total"] == 21.6


def test_quote_applies_order_discount():
    assert quote([{"price": 10.0, "qty": 2}], discount=5.0) == {"total": 16.2}
