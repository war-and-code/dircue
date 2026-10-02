#!/usr/bin/env python3
"""Generate the deterministic 128 MiB-class XML formats workload."""
from pathlib import Path
import sys

out = Path(sys.argv[1])
out.parent.mkdir(parents=True, exist_ok=True)
unit = b'<record id="0123456789"><value>directory inventory workload</value></record>\n'
target = 128 * 1024 * 1024
chunk = unit * (1024 * 1024 // len(unit))
with out.open('wb') as stream:
    stream.write(b'<?xml version="1.0" encoding="UTF-8"?><records>\n')
    remaining = target - stream.tell() - len(b'</records>\n')
    while remaining > 0:
        block = chunk[:min(len(chunk), remaining)]
        stream.write(block)
        remaining -= len(block)
    stream.write(b'\n</records>\n')
print(f'{out} {out.stat().st_size}')
