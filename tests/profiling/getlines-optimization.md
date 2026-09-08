# Bounded generated-file line selection

This change was accepted for its reduction in allocated bytes after differential helper and generated-predicate tests, full maintained and root suites, managed regeneration, 11 actual Ruby repository comparisons, and a paired scanner experiment. The runtime change is limited to `getLines`; it includes a regression test, maintained patch, and generated provenance. The timing result did not meet the predefined speedup threshold.

The [paired evidence](results/getlines-opt3/README.md) retains all 440 measured samples, 18 excluded warmups and two separate memory profiles. Every result digest matches RC1. Roslyn median allocated bytes per scan decreased from 2,528,089,016 to 2,232,674,120 (11.7%); peak RSS remained about 347 MiB. Scan time changed from 7.4805 s to 7.3310 s, below the predefined 10% speed margin. Small scenarios remain noisy. The separate profiles attribute 297.19 MB versus 2.50 MB to `getLines` in pprof display units; cumulative sampled allocations differ from benchmark B/op and peak RSS.

The candidate was built from committed `390ce5fbcb605eec72af524368d03bdf53715b24` plus four hashed overlay files. The archive includes those source bytes, the prior source inventory, compiler/build receipts, raw outputs, paired confidence intervals, and the isolated proof. Later CLI, sample, and security checks are recorded in the [final validation bundle](../release/results/final/README.md). The 3,388 classifier samples alone do not validate generated-file filtering.

## Baseline and change

The immediate opt2 baseline is the [fresh scanner profile](results/scanner-opt2/README.md): Roslyn `data.getLines` accumulated 275.52 MiB (11.42%) and `bytes.genSplit` 276.06 MiB. Its archive identifies the exact `390ce5f` baseline source and binary. These are cumulative allocated bytes, not peak RSS.

The historical RC1 profile had already ranked splitting fourth at approximately 279 MiB; generated-file checks accounted for 283 MiB, including repeated first-three-line requests. Its earlier opportunity score was 4 impact × 5 confidence / 1 effort = 20. The fresh opt2 evidence, rather than that historical score, establishes the immediate baseline for this experiment.

Only `getLines` changes. Instead of splitting every LF field and then selecting a few, it finds LF delimiters forward for positive requests or backward for negative requests. `rubyLines`, `forEachLine`, every generated predicate, tokenization, classification, file reads, and concurrency remain unchanged. There are 30 current direct calls using positive constants 1, 2, 3, 5, 6, 10, 20, 30, 40 or negative -2; there are no current zero, MinInt or dynamic requests.

## Equivalence and edge cases

The old oracle is the source helper at `data/generated.go`, SHA256 `ada6d75135dc7cb29dc4280df4c16c2fc9ce044836923e14594cae08f770ff6c`, retained as `generated-before.go.txt`. It uses LF-only `bytes.Split` for nonempty data, retaining trailing empty fields. Prefix selection returns the first fields; suffix selection reverses the final fields.

Forward search returns the same bytes before each LF and stops once the requested prefix is complete. When there is no remaining LF, it returns the unsplit final field. Reverse search returns bytes after the final LF, restricts the remaining prefix to bytes before that delimiter, and repeats. This is precisely the old reversed suffix selection, including final blanks and a leading empty field.

- Ordering preserved: prefixes remain first-to-last; suffixes remain last-to-first.
- Tie-breaking, floating point, production RNG: not involved and unchanged elsewhere.
- Empty content and nonnegative `n`: nil outer result, regardless of whether input was nil or an empty allocated slice.
- Empty content and negative `n`: non-nil empty outer result.
- Nonempty content and `n=0`: non-nil empty outer result.
- `n=MinInt`: negation still overflows and the same negative-capacity `make` operation panics, even on empty input.
- Large positive/negative requests: stop at actual fields without eagerly allocating an unbounded requested count. Initial header capacity is at most eight, growing only for selected fields.
- Raw bytes: only LF separates fields. CR, NUL, BOM bytes, malformed UTF-8 and all other bytes remain unchanged.
- Inner slices: remain views into the original input. A delimiter-ended field has capacity equal to its length; the final suffix retains the input's remaining capacity, matching Go 1.26.6 `bytes.genSplit`. Reverse traversal caps the retained prefix at the delimiter to preserve these same per-field capacities.
- Outer slice capacity intentionally differs: the former complete split reserved headers for every field, even when few were returned. Reducing that backing allocation is the measured objective. No current caller observes outer capacity; elements, order, nil distinctions and inner capacities are preserved.
- Correctness evidence: focused frozen-helper differential and generated-predicate tests PASS; the full isolated maintained suite PASS. All 11 pinned public repositories subsequently matched actual Ruby inclusion, paths, percentages and bytes; paired allocation acceptance is recorded above.

## Tests

`data/generated_lines_test.go` contains a frozen implementation of the old helper with its original `rubyLines` behavior inlined. It compares results and panic type/message against the candidate, including inner slice capacity and mutations through returned slices into independent equal-capacity source buffers.

Coverage includes every string up to length five over text/LF/CR/NUL, empty/nil/spare-capacity inputs, MinInt/MinInt+1/MaxInt, positive and negative boundary counts, 5,000 seeded random byte inputs, long single lines, many blank fields and CRLF-heavy data. Predicate checks cover marker positions around Haxe 3, Go 40, go-to-protobuf 20, protobuf 3 and Thrift 6; minimum field requirements; JNI/Racc positioning; source-map footer trailing blanks; exact CoffeeScript reverse footer/raw CR handling; and VCR second-to-last fields.

The focused data-package tests and full maintained suite passed; the linked validation receipt retains commands, logs, and source hashes. The 3,388 sample checks validate labels and ordered tokens, excluding `IsGenerated`, so they cannot alone accept this change. The actual Ruby public-repository comparison and paired Git/raw-flat checks, including Roslyn, subsequently passed. Separate memory profiles confirmed the allocation reduction before work began on another optimization. The final integrated binary has separate release validation.

## Managed integration and rollback

`getlines.patch` is root-relative and contains only the helper delta plus its new regression test. The tokenizer experiment was accepted first. Apply the verified overlay only after checking all prior hashes. Include the new data test in the maintained patch. The updater automatically discovers managed test files from real adjacent patch headers through `patched_test_files`; no separate managed-test list edit is required. Regeneration and provenance verification are required before release. Do not copy the mirror's stale manifest. The four integrated files are `third_party/go-enry/data/generated.go`, its new `generated_lines_test.go`, `third_party/patches/enry-linguist-9.7.patch`, and `third_party/go-enry/PROVENANCE.json`. Candidate regeneration already proved the managed output equals those bytes. Roll back by reverting the dedicated getLines commit.
