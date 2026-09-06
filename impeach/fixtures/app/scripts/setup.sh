#!/bin/sh
# Create the fixture app's virtualenv and install pytest.
# Run from the fixture app root: sh scripts/setup.sh
set -eu

here=$(cd "$(dirname "$0")/.." && pwd)
cd "$here"

python3 -m venv .venv
./.venv/bin/python -m pip install --quiet --upgrade pip
./.venv/bin/python -m pip install --quiet -r requirements.txt

echo "venv ready. Run the suite with:"
echo "  cd $here && ./.venv/bin/python -m pytest -q"
