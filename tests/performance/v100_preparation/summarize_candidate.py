#!/usr/bin/env python3
"""Render the completed cache experiment; raw receipts remain authoritative."""
import gzip,json
from pathlib import Path
from benchmark import OUT

def main():
 r=json.loads(gzip.decompress((OUT/'cache-ab.json.gz').read_bytes()))
 rows=['## Interleaved results','',f"Window: {r['started_at']} to {r['finished_at']}. All twenty measured pairs and three warmup pairs per lane matched exact baseline stdout, with empty stderr and zero exit status. Binary and corpus checks passed.",'','| Workload | Baseline / candidate median ms | Paired time ratio [95% bootstrap] | Baseline / candidate p95 ms | Baseline / candidate RSS MiB |','|---|---:|---:|---:|---:|']
 for s in r['scenarios']:
  a,b=s['summary']['baseline'],s['summary']['candidate'];lo,hi=s['paired_bootstrap_median_ratio_95pct']
  rows.append(f"| {s['corpus']} {s['lane']} | {a['p50']*1000:.2f} / {b['p50']*1000:.2f} | {s['paired_median_ratio']:.3f} [{lo:.3f}, {hi:.3f}] | {a['p95']*1000:.2f} / {b['p95']*1000:.2f} | {a['median_peak_rss_bytes']/2**20:.1f} / {b['median_peak_rss_bytes']/2**20:.1f} |")
 rows+=['','Ratios below 1 mean lower elapsed time. CPU percentage, logical throughput, all raw CPU/time/RSS/I/O counters and individual paired ratios are retained in `cache-ab.json.gz`. The paired interval does not repair shared-host bias or imply precision for p95 and rarer tails.','']
 p=OUT/'OPTIMIZATION.md';s=p.read_text();marker='Candidate measurements and reprofile results will be appended after completion. Until then, this document makes no speedup claim.'
 if marker not in s:raise RuntimeError('Refusing to overwrite an already-rendered report')
 p.write_text(s.replace(marker,'\n'.join(rows)))
if __name__=='__main__':main()
