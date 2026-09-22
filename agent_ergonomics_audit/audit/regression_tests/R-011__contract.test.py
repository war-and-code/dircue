#!/usr/bin/env python3
"""R-011 public CLI regression; exit 0 pass, 1 contract absent."""
import argparse
from pathlib import Path
import runpy
import sys
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("binary", help="Candidate or genuine baseline executable")
args = parser.parse_args()
runner = Path(__file__).resolve().parents[2] / "tools/check_recommendation.py"
sys.argv = [str(runner), "R-011", args.binary]
runpy.run_path(str(runner), run_name="__main__")
