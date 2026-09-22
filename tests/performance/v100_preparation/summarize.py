#!/usr/bin/env python3
"""Summarize preserved raw measurements without removing outliers."""
import gzip,json,statistics
from benchmark import OUT,save

def main():
 r=json.loads((OUT/'baseline.json').read_text());lines=['# v0.8.0 diagnostic baseline','','Optimized unstripped baseline, warm filesystem, fresh process, 8 workers; 3 warmups and 20 retained measurements per lane. Times include CLI startup and output serialization. See README for host and tail limitations.','','| Corpus | Lane | p50 ms | p95 ms | Max / p99 sentinel ms | Median RSS MiB | Median CPU % | CV % |','|---|---|---:|---:|---:|---:|---:|---:|']
 drift={}
 for c in r['cases']:
  raw=json.loads(gzip.decompress((OUT/(c['name']+'-samples.json.gz')).read_bytes()))
  for lane,s in c['summary'].items():
   lines.append(f"| {c['name']} | {lane} | {s['p50']*1000:.2f} | {s['p95']*1000:.2f} | {s['max']*1000:.2f} | {s['median_peak_rss_bytes']/2**20:.1f} | {s['median_cpu_percent']:.1f} | {s['cv']*100:.2f} |")
   samples=raw['samples'][lane];early=sorted(x['seconds'] for x in samples[:10])[-1];late=sorted(x['seconds'] for x in samples[10:])[-1]
   drift[c['name']+'/'+lane]={'early10_p95':early,'late10_p95':late,'relative_change':late/early-1,'over10percent':abs(late/early-1)>.1}
 lines+=['','All samples are preserved in the case `*-samples.json.gz` receipts. p99, p99.9 and p99.99 use nearest-rank and equal the maximum at this sample count; none is a reliable tail estimate. CPU 100% means one full core and can exceed 100% across workers. Logical corpus throughput is retained in baseline.json and is not physical disk bandwidth.','', 'The corpus-size comparisons do not establish a scaling law: language mix, Git storage and optional work differ. A 1/10/50/100/500/1000 controlled scaling experiment has not been run. First-half/second-half worst-observed drift is recorded in variance.json as a diagnostic only, not an A/B significance test.']
 (OUT/'BASELINE.md').write_text('\n'.join(lines)+'\n');save(OUT/'variance.json',drift)
if __name__=='__main__':main()
