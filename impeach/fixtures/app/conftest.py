"""Present so pytest puts the fixture app root on sys.path.

With pytest's default prepend import mode, the directory holding the rootdir
conftest is inserted into sys.path, which is what makes "import app" resolve
without an installed package.
"""
