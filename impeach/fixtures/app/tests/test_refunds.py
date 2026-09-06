from app.refunds import parse_refund, refund_amount


def test_parse_refund_normalises_record():
    record = {"order_id": "A-1", "items": [{"price": 4.0, "qty": 1}]}
    parsed = parse_refund(record)
    assert parsed["order_id"] == "A-1"
    assert parsed["reason"] == "unspecified"


def test_parse_refund_keeps_reason():
    record = {"order_id": "A-2", "reason": "damaged"}
    assert parse_refund(record)["reason"] == "damaged"


def test_refund_amount_full():
    assert refund_amount([{"price": 10.0, "qty": 1}]) == 10.8


def test_refund_amount_with_restocking_fee():
    assert refund_amount([{"price": 10.0, "qty": 1}], restocking_fee=1.0) == 9.8
