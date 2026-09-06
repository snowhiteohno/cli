from app.service import round_money


def test_round_money_two_places():
    assert round_money(2.345) == 2.35


def test_round_money_already_rounded():
    assert round_money(7.25) == 7.25


def test_round_money_zero():
    assert round_money(0.0) == 0.0


def test_round_money_negative():
    assert round_money(-1.005) == -1.01
