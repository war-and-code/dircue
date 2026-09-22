# Agent Ergonomics Scorecard

Generated: 2026-09-21T23:26:01Z
Source: `tests/ergonomics_v100/audit/agent_surfaces_pre_pass1.jsonl` (pass 1)


## Per-surface scores

| surface_id | weighted | intu | ergo | ease | parse | error | intent | safe | det | self | comp | regr |
|------------|----------|------|------|------|-------|-------|--------|------|-----|------|------|------|
| env__GOMAXPROCS | 518 | 650 | 600 | 225 | 1000 | 300 | 125 | 1000 | 575 | 225 | 625 | 375 |
| env__SystemRoot | 518 | 650 | 600 | 225 | 1000 | 300 | 125 | 1000 | 575 | 225 | 625 | 375 |
| error__compare-arguments | 688 | 1000 | 1000 | 1000 | 500 | 250 | 250 | 1000 | 1000 | 250 | 800 | 525 |
| error__focus-selection | 790 | 1000 | 1000 | 1000 | 500 | 750 | 550 | 1000 | 1000 | 600 | 800 | 500 |
| error__metrics-prerequisite | 790 | 1000 | 1000 | 1000 | 500 | 750 | 550 | 1000 | 1000 | 600 | 800 | 500 |
| error__path-arguments | 686 | 1000 | 1000 | 1000 | 500 | 250 | 250 | 1000 | 1000 | 250 | 800 | 500 |
| error__plan-arguments | 688 | 1000 | 1000 | 1000 | 500 | 250 | 250 | 1000 | 1000 | 250 | 800 | 525 |
| error__plan-selection | 681 | 1000 | 1000 | 1000 | 500 | 200 | 250 | 1000 | 1000 | 250 | 800 | 500 |
| error__saved-report-open | 686 | 1000 | 1000 | 1000 | 500 | 250 | 250 | 1000 | 1000 | 250 | 800 | 500 |
| error__scan-flag-scope | 765 | 1000 | 1000 | 1000 | 500 | 625 | 500 | 1000 | 1000 | 500 | 800 | 500 |
| error__structure-worker | 686 | 1000 | 1000 | 1000 | 500 | 250 | 250 | 1000 | 1000 | 250 | 800 | 500 |
| error__unknown-analysis | 720 | 1000 | 1000 | 1000 | 500 | 475 | 250 | 1000 | 1000 | 400 | 800 | 500 |
| error__unknown-flag | 686 | 1000 | 1000 | 1000 | 500 | 250 | 250 | 1000 | 1000 | 250 | 800 | 500 |
| exit__0 | 713 | 1000 | 700 | 350 | 750 | 400 | 1000 | 1000 | 825 | 325 | 825 | 675 |
| exit__1 | 713 | 1000 | 700 | 350 | 750 | 400 | 1000 | 1000 | 825 | 325 | 825 | 675 |
| flag__analyze__discovery | 609 | 725 | 625 | 575 | 675 | 425 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__analyze__files | 615 | 725 | 625 | 575 | 675 | 425 | 250 | 1000 | 725 | 425 | 750 | 600 |
| flag__analyze__functions | 613 | 725 | 625 | 575 | 675 | 425 | 250 | 1000 | 725 | 425 | 750 | 575 |
| flag__analyze__hotspots | 613 | 725 | 625 | 575 | 675 | 425 | 250 | 1000 | 725 | 425 | 750 | 575 |
| flag__analyze__metrics-max-file-bytes | 622 | 725 | 625 | 575 | 675 | 500 | 250 | 1000 | 725 | 425 | 750 | 600 |
| flag__analyze__metrics-scope | 622 | 725 | 625 | 575 | 675 | 500 | 250 | 1000 | 725 | 425 | 750 | 600 |
| flag__analyze__rules-file | 615 | 725 | 625 | 575 | 675 | 500 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__analyze__structural-max-file-bytes | 620 | 725 | 625 | 575 | 675 | 500 | 250 | 1000 | 725 | 425 | 750 | 575 |
| flag__analyze__structural-timeout | 620 | 725 | 625 | 575 | 675 | 500 | 250 | 1000 | 725 | 425 | 750 | 575 |
| flag__analyze__structural-worker | 620 | 725 | 625 | 575 | 675 | 500 | 250 | 1000 | 725 | 425 | 750 | 575 |
| flag__analyze__syft-report | 615 | 725 | 625 | 575 | 675 | 500 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__analyze__syft-report-max-bytes | 615 | 725 | 625 | 575 | 675 | 500 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__analyze__syft-report-sha256 | 615 | 725 | 625 | 575 | 675 | 500 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__analyze__syft-root | 615 | 725 | 625 | 575 | 675 | 500 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__analyze__syft-source-tree | 615 | 725 | 625 | 575 | 675 | 500 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__analyze_all_hfa99fd5a__availability | 609 | 725 | 625 | 575 | 675 | 425 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__analyze_all_hfa99fd5a__declarations | 609 | 725 | 625 | 575 | 675 | 425 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__analyze_all_hfa99fd5a__environments | 609 | 725 | 625 | 575 | 675 | 425 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__analyze_all_hfa99fd5a__formats | 609 | 725 | 625 | 575 | 675 | 425 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__analyze_all_hfa99fd5a__graph | 609 | 725 | 625 | 575 | 675 | 425 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__analyze_all_hfa99fd5a__metrics | 615 | 725 | 625 | 575 | 675 | 425 | 250 | 1000 | 725 | 425 | 750 | 600 |
| flag__analyze_all_hfa99fd5a__projects | 609 | 725 | 625 | 575 | 675 | 425 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__analyze_all_hfa99fd5a__registries | 609 | 725 | 625 | 575 | 675 | 425 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__analyze_all_hfa99fd5a__structure | 613 | 725 | 625 | 575 | 675 | 425 | 250 | 1000 | 725 | 425 | 750 | 575 |
| flag__analyze_explain_h38b2582c__file | 615 | 725 | 625 | 575 | 675 | 500 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__analyze_explain_h38b2582c__project | 615 | 725 | 625 | 575 | 675 | 500 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__analyze_explain_h38b2582c__report | 615 | 725 | 625 | 575 | 675 | 500 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__analyze_focus_hcf64d841__affected-by | 615 | 725 | 625 | 575 | 675 | 500 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__analyze_focus_hcf64d841__metrics | 615 | 725 | 625 | 575 | 675 | 425 | 250 | 1000 | 725 | 425 | 750 | 600 |
| flag__analyze_focus_hcf64d841__project | 615 | 725 | 625 | 575 | 675 | 500 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__analyze_focus_hcf64d841__related-project | 615 | 725 | 625 | 575 | 675 | 500 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__b | 600 | 725 | 675 | 425 | 675 | 425 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__breakdown | 600 | 725 | 675 | 425 | 675 | 425 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__h | 625 | 775 | 675 | 575 | 625 | 475 | 250 | 1000 | 725 | 425 | 750 | 600 |
| flag__help | 625 | 775 | 675 | 575 | 625 | 475 | 250 | 1000 | 725 | 425 | 750 | 600 |
| flag__j | 634 | 825 | 675 | 575 | 650 | 475 | 250 | 1000 | 725 | 425 | 750 | 625 |
| flag__json | 634 | 825 | 675 | 575 | 650 | 475 | 250 | 1000 | 725 | 425 | 750 | 625 |
| flag__max-file-bytes | 602 | 725 | 625 | 425 | 675 | 500 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__on-error | 602 | 725 | 625 | 425 | 675 | 500 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__plan__input | 584 | 725 | 625 | 450 | 675 | 275 | 250 | 1000 | 725 | 375 | 750 | 575 |
| flag__plan__module | 584 | 725 | 625 | 450 | 675 | 275 | 250 | 1000 | 725 | 375 | 750 | 575 |
| flag__plan__project | 584 | 725 | 625 | 450 | 675 | 275 | 250 | 1000 | 725 | 375 | 750 | 575 |
| flag__plan__question | 584 | 725 | 625 | 450 | 675 | 275 | 250 | 1000 | 725 | 375 | 750 | 575 |
| flag__r | 606 | 725 | 675 | 425 | 675 | 500 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__rev | 606 | 725 | 675 | 425 | 675 | 500 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__s | 595 | 725 | 625 | 425 | 675 | 425 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__source | 645 | 775 | 675 | 425 | 675 | 725 | 250 | 1000 | 725 | 500 | 750 | 600 |
| flag__strategies | 595 | 725 | 625 | 425 | 675 | 425 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__t | 602 | 725 | 625 | 425 | 675 | 500 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__tree | 602 | 725 | 625 | 425 | 675 | 500 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__tree-size | 602 | 725 | 625 | 425 | 675 | 500 | 250 | 1000 | 725 | 425 | 750 | 525 |
| flag__v | 625 | 775 | 675 | 575 | 625 | 475 | 250 | 1000 | 725 | 425 | 750 | 600 |
| flag__version | 625 | 775 | 675 | 575 | 625 | 475 | 250 | 1000 | 725 | 425 | 750 | 600 |
| flag__workers | 602 | 725 | 625 | 425 | 675 | 500 | 250 | 1000 | 725 | 425 | 750 | 525 |
| schema__availability | 622 | 700 | 375 | 250 | 750 | 400 | 1000 | 1000 | 775 | 325 | 625 | 650 |
| schema__capabilities | 618 | 700 | 375 | 250 | 750 | 400 | 1000 | 1000 | 775 | 325 | 625 | 600 |
| schema__comparison | 609 | 700 | 375 | 250 | 750 | 400 | 1000 | 1000 | 775 | 325 | 625 | 500 |
| schema__declarations | 622 | 700 | 375 | 250 | 750 | 400 | 1000 | 1000 | 775 | 325 | 625 | 650 |
| schema__environments | 609 | 700 | 375 | 250 | 750 | 400 | 1000 | 1000 | 775 | 325 | 625 | 500 |
| schema__explanation | 622 | 700 | 375 | 250 | 750 | 400 | 1000 | 1000 | 775 | 325 | 625 | 650 |
| schema__findings | 622 | 700 | 375 | 250 | 750 | 400 | 1000 | 1000 | 775 | 325 | 625 | 650 |
| schema__focus | 622 | 700 | 375 | 250 | 750 | 400 | 1000 | 1000 | 775 | 325 | 625 | 650 |
| schema__formats | 622 | 700 | 375 | 250 | 750 | 400 | 1000 | 1000 | 775 | 325 | 625 | 650 |
| schema__hotspots | 622 | 700 | 375 | 250 | 750 | 400 | 1000 | 1000 | 775 | 325 | 625 | 650 |
| schema__languages | 600 | 700 | 375 | 250 | 600 | 400 | 1000 | 1000 | 775 | 325 | 625 | 550 |
| schema__planning | 618 | 700 | 375 | 250 | 750 | 400 | 1000 | 1000 | 775 | 325 | 625 | 600 |
| schema__profile | 622 | 700 | 375 | 250 | 750 | 400 | 1000 | 1000 | 775 | 325 | 625 | 650 |
| signal__SIGINT | 681 | 1000 | 600 | 300 | 1000 | 450 | 1000 | 1000 | 700 | 300 | 675 | 475 |
| signal__SIGTERM | 681 | 1000 | 600 | 300 | 1000 | 450 | 1000 | 1000 | 700 | 300 | 675 | 475 |
| verb__analyze | 604 | 600 | 525 | 350 | 1000 | 450 | 250 | 1000 | 750 | 375 | 750 | 600 |
| verb__analyze__all | 652 | 775 | 800 | 575 | 700 | 425 | 250 | 1000 | 750 | 550 | 750 | 600 |
| verb__analyze__availability | 631 | 750 | 600 | 575 | 700 | 425 | 250 | 1000 | 750 | 550 | 750 | 600 |
| verb__analyze__declarations | 631 | 750 | 600 | 575 | 700 | 425 | 250 | 1000 | 750 | 550 | 750 | 600 |
| verb__analyze__discovery | 631 | 750 | 600 | 575 | 700 | 425 | 250 | 1000 | 750 | 550 | 750 | 600 |
| verb__analyze__ecosystems | 631 | 750 | 600 | 575 | 700 | 425 | 250 | 1000 | 750 | 550 | 750 | 600 |
| verb__analyze__environments | 618 | 750 | 600 | 575 | 700 | 425 | 250 | 1000 | 750 | 550 | 750 | 450 |
| verb__analyze__explain | 595 | 750 | 600 | 350 | 700 | 425 | 250 | 1000 | 750 | 375 | 750 | 600 |
| verb__analyze__focus | 631 | 750 | 600 | 575 | 700 | 425 | 250 | 1000 | 750 | 550 | 750 | 600 |
| verb__analyze__formats | 631 | 750 | 600 | 575 | 700 | 425 | 250 | 1000 | 750 | 550 | 750 | 600 |
| verb__analyze__frameworks | 631 | 750 | 600 | 575 | 700 | 425 | 250 | 1000 | 750 | 550 | 750 | 600 |
| verb__analyze__graph | 631 | 750 | 600 | 575 | 700 | 425 | 250 | 1000 | 750 | 550 | 750 | 600 |
| verb__analyze__languages | 634 | 775 | 600 | 575 | 650 | 425 | 250 | 1000 | 800 | 550 | 750 | 600 |
| verb__analyze__metrics | 634 | 775 | 600 | 575 | 700 | 425 | 250 | 1000 | 750 | 550 | 750 | 600 |
| verb__analyze__packages | 631 | 750 | 600 | 575 | 700 | 425 | 250 | 1000 | 750 | 550 | 750 | 600 |
| verb__analyze__projects | 631 | 750 | 600 | 575 | 700 | 425 | 250 | 1000 | 750 | 550 | 750 | 600 |
| verb__analyze__registries | 631 | 750 | 600 | 575 | 700 | 425 | 250 | 1000 | 750 | 550 | 750 | 600 |
| verb__analyze__rules | 631 | 750 | 600 | 575 | 700 | 425 | 250 | 1000 | 750 | 550 | 750 | 600 |
| verb__analyze__structure | 631 | 750 | 600 | 575 | 700 | 425 | 250 | 1000 | 750 | 550 | 750 | 600 |
| verb__capabilities | 618 | 750 | 600 | 350 | 700 | 425 | 250 | 1000 | 775 | 600 | 750 | 600 |
| verb__compare | 584 | 675 | 600 | 350 | 700 | 325 | 250 | 1000 | 750 | 425 | 750 | 600 |
| verb__dircue | 600 | 775 | 600 | 375 | 650 | 425 | 250 | 1000 | 800 | 375 | 750 | 600 |
| verb__help | 622 | 750 | 600 | 350 | 1000 | 425 | 250 | 1000 | 750 | 375 | 750 | 600 |
| verb__plan | 593 | 700 | 725 | 350 | 725 | 300 | 250 | 1000 | 750 | 375 | 750 | 600 |

## Distribution histogram

### Weighted score distribution (per surface)

```
   0- 99 │  (0)
 100-199 │  (0)
 200-299 │  (0)
 300-399 │  (0)
 400-499 │  (0)
 500-599 │ ███████████ (11)
 600-699 │ ███████████████████████████████████████████████████████████████████████████████████████████ (91)
 700-799 │ ██████ (6)
 800-899 │  (0)
 900-999 │  (0)
1000     │  (0)
```

## Below-Polish-Bar surfaces (weighted < 750)

- env__GOMAXPROCS (weighted: 518)
- env__SystemRoot (weighted: 518)
- error__compare-arguments (weighted: 688)
- error__path-arguments (weighted: 686)
- error__plan-arguments (weighted: 688)
- error__plan-selection (weighted: 681)
- error__saved-report-open (weighted: 686)
- error__structure-worker (weighted: 686)
- error__unknown-analysis (weighted: 720)
- error__unknown-flag (weighted: 686)
- exit__0 (weighted: 713)
- exit__1 (weighted: 713)
- flag__analyze__discovery (weighted: 609)
- flag__analyze__files (weighted: 615)
- flag__analyze__functions (weighted: 613)
- flag__analyze__hotspots (weighted: 613)
- flag__analyze__metrics-max-file-bytes (weighted: 622)
- flag__analyze__metrics-scope (weighted: 622)
- flag__analyze__rules-file (weighted: 615)
- flag__analyze__structural-max-file-bytes (weighted: 620)
- flag__analyze__structural-timeout (weighted: 620)
- flag__analyze__structural-worker (weighted: 620)
- flag__analyze__syft-report (weighted: 615)
- flag__analyze__syft-report-max-bytes (weighted: 615)
- flag__analyze__syft-report-sha256 (weighted: 615)
- flag__analyze__syft-root (weighted: 615)
- flag__analyze__syft-source-tree (weighted: 615)
- flag__analyze_all_hfa99fd5a__availability (weighted: 609)
- flag__analyze_all_hfa99fd5a__declarations (weighted: 609)
- flag__analyze_all_hfa99fd5a__environments (weighted: 609)
- flag__analyze_all_hfa99fd5a__formats (weighted: 609)
- flag__analyze_all_hfa99fd5a__graph (weighted: 609)
- flag__analyze_all_hfa99fd5a__metrics (weighted: 615)
- flag__analyze_all_hfa99fd5a__projects (weighted: 609)
- flag__analyze_all_hfa99fd5a__registries (weighted: 609)
- flag__analyze_all_hfa99fd5a__structure (weighted: 613)
- flag__analyze_explain_h38b2582c__file (weighted: 615)
- flag__analyze_explain_h38b2582c__project (weighted: 615)
- flag__analyze_explain_h38b2582c__report (weighted: 615)
- flag__analyze_focus_hcf64d841__affected-by (weighted: 615)
- flag__analyze_focus_hcf64d841__metrics (weighted: 615)
- flag__analyze_focus_hcf64d841__project (weighted: 615)
- flag__analyze_focus_hcf64d841__related-project (weighted: 615)
- flag__b (weighted: 600)
- flag__breakdown (weighted: 600)
- flag__h (weighted: 625)
- flag__help (weighted: 625)
- flag__j (weighted: 634)
- flag__json (weighted: 634)
- flag__max-file-bytes (weighted: 602)
- flag__on-error (weighted: 602)
- flag__plan__input (weighted: 584)
- flag__plan__module (weighted: 584)
- flag__plan__project (weighted: 584)
- flag__plan__question (weighted: 584)
- flag__r (weighted: 606)
- flag__rev (weighted: 606)
- flag__s (weighted: 595)
- flag__source (weighted: 645)
- flag__strategies (weighted: 595)
- flag__t (weighted: 602)
- flag__tree (weighted: 602)
- flag__tree-size (weighted: 602)
- flag__v (weighted: 625)
- flag__version (weighted: 625)
- flag__workers (weighted: 602)
- schema__availability (weighted: 622)
- schema__capabilities (weighted: 618)
- schema__comparison (weighted: 609)
- schema__declarations (weighted: 622)
- schema__environments (weighted: 609)
- schema__explanation (weighted: 622)
- schema__findings (weighted: 622)
- schema__focus (weighted: 622)
- schema__formats (weighted: 622)
- schema__hotspots (weighted: 622)
- schema__languages (weighted: 600)
- schema__planning (weighted: 618)
- schema__profile (weighted: 622)
- signal__SIGINT (weighted: 681)
- signal__SIGTERM (weighted: 681)
- verb__analyze (weighted: 604)
- verb__analyze__all (weighted: 652)
- verb__analyze__availability (weighted: 631)
- verb__analyze__declarations (weighted: 631)
- verb__analyze__discovery (weighted: 631)
- verb__analyze__ecosystems (weighted: 631)
- verb__analyze__environments (weighted: 618)
- verb__analyze__explain (weighted: 595)
- verb__analyze__focus (weighted: 631)
- verb__analyze__formats (weighted: 631)
- verb__analyze__frameworks (weighted: 631)
- verb__analyze__graph (weighted: 631)
- verb__analyze__languages (weighted: 634)
- verb__analyze__metrics (weighted: 634)
- verb__analyze__packages (weighted: 631)
- verb__analyze__projects (weighted: 631)
- verb__analyze__registries (weighted: 631)
- verb__analyze__rules (weighted: 631)
- verb__analyze__structure (weighted: 631)
- verb__capabilities (weighted: 618)
- verb__compare (weighted: 584)
- verb__dircue (weighted: 600)
- verb__help (weighted: 622)
- verb__plan (weighted: 593)
