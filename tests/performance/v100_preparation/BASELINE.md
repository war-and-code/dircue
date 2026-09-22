# v0.8.0 diagnostic baseline

Optimized unstripped baseline, warm filesystem, fresh process, 8 workers; 3 warmups and 20 retained measurements per lane. Times include CLI startup and output serialization. See README for host and tail limitations.

| Corpus | Lane | p50 ms | p95 ms | Max / p99 sentinel ms | Median RSS MiB | Median CPU % | CV % |
|---|---|---:|---:|---:|---:|---:|---:|
| spring-framework | languages | 1649.62 | 1682.68 | 1798.55 | 157.2 | 191.5 | 2.18 |
| spring-framework | discovery | 1272.71 | 1325.00 | 1384.98 | 45.1 | 121.5 | 2.31 |
| spring-framework | all | 1659.94 | 1677.13 | 1678.56 | 158.1 | 191.5 | 0.66 |
| spring-framework | projects | 1650.49 | 1669.28 | 1681.17 | 163.1 | 193.4 | 0.70 |
| roslyn | languages | 6554.20 | 6748.08 | 6749.01 | 357.8 | 180.0 | 1.23 |
| roslyn | discovery | 3968.48 | 4002.28 | 4094.76 | 77.8 | 117.9 | 0.81 |
| roslyn | all | 9657.42 | 9805.98 | 10004.18 | 356.0 | 159.4 | 1.37 |
| roslyn | projects | 6583.04 | 6811.34 | 6917.32 | 367.9 | 182.5 | 1.49 |
| aspnetcore | languages | 2218.51 | 2337.55 | 2348.39 | 210.4 | 183.5 | 1.86 |
| aspnetcore | discovery | 1536.73 | 1572.40 | 1607.41 | 52.7 | 120.5 | 1.22 |
| aspnetcore | all | 2363.89 | 2393.82 | 2466.93 | 208.6 | 179.7 | 1.15 |
| aspnetcore | projects | 2230.78 | 2282.38 | 2669.07 | 226.5 | 186.6 | 4.39 |
| aspnetcore | optional | 3054.18 | 3203.53 | 3206.50 | 321.9 | 168.0 | 1.68 |
| xml-2gib | languages | 28.70 | 30.17 | 30.36 | 49.6 | 180.9 | 3.57 |
| xml-2gib | discovery | 24.16 | 24.88 | 24.92 | 26.6 | 41.4 | 3.06 |
| xml-2gib | all | 39.60 | 41.36 | 42.16 | 78.6 | 250.9 | 3.65 |
| xml-2gib | projects | 29.84 | 31.51 | 31.63 | 49.8 | 192.6 | 3.35 |
| xml-2gib | optional | 176.50 | 179.13 | 182.19 | 78.5 | 151.4 | 1.47 |

All samples are preserved in the case `*-samples.json.gz` receipts. p99, p99.9 and p99.99 use nearest-rank and equal the maximum at this sample count; none is a reliable tail estimate. CPU 100% means one full core and can exceed 100% across workers. Logical corpus throughput is retained in baseline.json and is not physical disk bandwidth.

The corpus-size comparisons do not establish a scaling law: language mix, Git storage and optional work differ. A 1/10/50/100/500/1000 controlled scaling experiment has not been run. First-half/second-half worst-observed drift is recorded in variance.json as a diagnostic only, not an A/B significance test.
