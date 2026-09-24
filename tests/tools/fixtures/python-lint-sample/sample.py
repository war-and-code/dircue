"""Python sample fixture for ruff SARIF and semgrep SARIF oracle testing."""
import os
import sys


# Unused variable (will trigger ruff F841)
def process_data(data):
    result = []
    unused_var = "never used"
    for item in data:
        if item is not None:
            result.append(item)
    return result


# Missing whitespace (E225 / ruff)
def another_function():
    x = 1+2
    return x


# Bare except (will trigger semgrep bare-except rule)
def risky():
    try:
        return int("abc")
    except:
        return 0


# Hardcoded /tmp path (will trigger semgrep hardcoded-tmp-path rule)
def get_path():
    tmpfile = "/tmp/mydata"
    return tmpfile


class MyClass:
    def method(self):
        pass
