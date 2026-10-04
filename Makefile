VERSION ?= 1.3.0-dev
REFERENCE_IMAGE ?= dircue-linguist:9.7.0
RELEASE_DIR ?= dist
WHEEL_DIR ?= $(RELEASE_DIR)/wheels
ATLAS_CACHE ?= .cache/atlas-repos
ATLAS_OUTPUT ?= .cache/atlas

.PHONY: build test check bench test-bench bench-cli reference conformance public-conformance classifier-window samples release release-archives hostile-fs forest-e2e atlas-fetch atlas atlas-smoke accuracy-cards golden corpus-availability fetch-receipts

build:
	CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags '-s -w -X github.com/war-and-code/dircue/internal/cli.Version=$(VERSION)' -o bin/dircue .

test:
	go test -race ./...

check:
	go vet ./...
	go test -race ./...
	cd third_party/go-enry && go test ./...
	cd third_party/go-git && go test -race . ./plumbing/format/packfile ./storage/filesystem ./storage/filesystem/dotgit

bench:
	go test ./pkg/scanner -run '^$$' -bench . -benchmem

# Self-tests for the benchmark correctness gate and PyPI readiness validator.
test-bench:
	python3 -m unittest tests/bench/test_run.py tests/release/test_pypi_index_ready.py tests/ci/test_fuzz_campaign_target.py

# Paired whole-CLI baseline/candidate comparison. Supply explicit binaries and
# a manifest; the harness never builds or selects a baseline automatically.
BENCH_BASELINE ?=
BENCH_CANDIDATE ?=
BENCH_MANIFEST ?= tests/bench/scenarios.json
BENCH_CORPUS ?=
BENCH_OUTPUT ?= .cache/bench/report.json
BENCH_RUNS ?= 20
BENCH_WARMUP ?= 3
BENCH_WORKER ?=
BENCH_WORKER_FLAG = $(if $(BENCH_WORKER),--worker "$(BENCH_WORKER)")
bench-cli:
	python3 tests/bench/run.py --baseline "$(BENCH_BASELINE)" --candidate "$(BENCH_CANDIDATE)" \
	  --manifest "$(BENCH_MANIFEST)" --corpus-root "$(BENCH_CORPUS)" \
	  --runs "$(BENCH_RUNS)" --warmup "$(BENCH_WARMUP)" --output "$(BENCH_OUTPUT)" $(BENCH_WORKER_FLAG)

hostile-fs: build
	python3 tests/hostile_fs/run.py --binary bin/dircue

forest-e2e: build
	python3 tests/forest/run.py --binary bin/dircue

reference:
	docker build -t $(REFERENCE_IMAGE) -f tests/conformance/Dockerfile tests/conformance

conformance: reference
	python3 tests/conformance/run.py --image $(REFERENCE_IMAGE)
	python3 tests/conformance/public.py --image $(REFERENCE_IMAGE)
	python3 tests/conformance/classifier_window.py --image $(REFERENCE_IMAGE)

public-conformance: reference
	python3 tests/conformance/public.py --image $(REFERENCE_IMAGE)

classifier-window: reference
	python3 tests/conformance/classifier_window.py --image $(REFERENCE_IMAGE)

samples: reference
	python3 tests/conformance/samples.py --image $(REFERENCE_IMAGE) --require-match

release:
	@python3 -c 'import sys; sys.path.insert(0, "scripts"); from wheels import python_version; python_version(sys.argv[1])' "$(VERSION)"
	$(MAKE) release-archives
	python3 scripts/wheels.py --release-dir "$(RELEASE_DIR)" --output "$(WHEEL_DIR)"

release-archives:
	python3 scripts/release.py --version "$(VERSION)" --output "$(RELEASE_DIR)"

# ---------------------------------------------------------------------------
# Fuzz campaign (#87)
# ---------------------------------------------------------------------------
#
# Run every Go fuzz target for FUZZ_TIME seconds each.
# Invoked by .github/workflows/fuzz-campaign.yml and by engineers locally.
#
#   make fuzz-campaign                           # 60s per target, all packages
#   make fuzz-campaign FUZZ_TIME=5               # 5s per target (quick smoke)
#   make fuzz-campaign FUZZ_PKG=./pkg/mapdiff    # restrict to one package
#   make fuzz-campaign FUZZ_CACHE=/tmp/fuzz-out  # store corpus in a known dir
#
# FUZZ_CACHE is passed as -test.fuzzcachedir so crashers are written there.
# If FUZZ_CACHE is empty the Go default ($GOCACHE/fuzz) is used.

FUZZ_TIME  ?= 60
FUZZ_PKG   ?= ./...
FUZZ_CACHE ?=

.PHONY: fuzz-campaign

fuzz-campaign: ## Run each Go fuzz target for FUZZ_TIME seconds (FUZZ_PKG=./... FUZZ_CACHE=dir)
	@set -e; \
	set -- "-fuzztime=$(FUZZ_TIME)s"; \
	if [ -n "$(FUZZ_CACHE)" ]; then \
	  mkdir -p "$(FUZZ_CACHE)"; \
	  _fuzz_cache=$$(cd "$(FUZZ_CACHE)" && pwd); \
	  set -- "$$@" "-test.fuzzcachedir=$$_fuzz_cache"; \
	fi; \
	_tmpdir=$$(mktemp -d); \
	trap 'rm -rf "$$_tmpdir"' EXIT HUP INT TERM; \
	if ! CGO_ENABLED=0 go list $(FUZZ_PKG) >"$$_tmpdir/packages"; then \
	  echo "Failed to list Go packages for $(FUZZ_PKG)" >&2; exit 1; \
	fi; \
	sort -u "$$_tmpdir/packages" >"$$_tmpdir/packages.sorted"; \
	_found=0; \
	while IFS= read -r _pkg; do \
	  [ -n "$$_pkg" ] || continue; \
	  if ! CGO_ENABLED=0 go test -list '^Fuzz' "$$_pkg" >"$$_tmpdir/targets"; then \
	    echo "Failed to discover fuzz targets in $$_pkg" >&2; exit 1; \
	  fi; \
	  while IFS= read -r _t; do \
	    case "$$_t" in Fuzz*) ;; *) continue ;; esac; \
	    _found=1; \
	    echo "==> $$_pkg: $$_t ($(FUZZ_TIME)s)"; \
	    CGO_ENABLED=0 go test "$$_pkg" -run='^$$' -fuzz='^'"$$_t"'$$' "$$@"; \
	  done <"$$_tmpdir/targets"; \
	done <"$$_tmpdir/packages.sorted"; \
	if [ "$$_found" -eq 0 ]; then echo "No fuzz targets found in $(FUZZ_PKG)" >&2; exit 1; fi

# ---------------------------------------------------------------------------
# Atlas parity and accuracy targets
# ---------------------------------------------------------------------------

# Restore bulky evidence receipt files from the GitHub evidence-archive-1 release.
# Requires network access. The files are listed in tests/receipts/evidence-archive.json.
# Each file is verified by sha256 after download; already-correct files are skipped.
fetch-receipts: ## Restore archived evidence receipts from GitHub release evidence-archive-1
	python3 scripts/fetch_receipts.py

# Run the release smokes that need only the core binary (declarations, formats,
# targeted analysis, context) against a version-stamped build. The release
# workflow runs them on packaged artifacts; running them here catches drift
# before a release. Offline.
RELEASE_SMOKE_VERSION ?= 1.0.0-rc.1
RELEASE_SMOKE_DIR ?= .cache/release-smoke
.PHONY: release-smoke
release-smoke: ## Run the offline core release smokes against a stamped binary
	@mkdir -p "$(RELEASE_SMOKE_DIR)"
	CGO_ENABLED=0 go build -mod=readonly -buildvcs=false -trimpath -ldflags "-X github.com/war-and-code/dircue/internal/cli.Version=$(RELEASE_SMOKE_VERSION)" -o "$(RELEASE_SMOKE_DIR)/dircue" .
	ln -sf dircue "$(RELEASE_SMOKE_DIR)/dirq"
	@dircue_v="$$("$(RELEASE_SMOKE_DIR)/dircue" --version 2>&1)"; \
	 dirq_v="$$("$(RELEASE_SMOKE_DIR)/dirq" --version 2>&1)"; \
	 echo "dircue --version: $$dircue_v"; \
	 echo "  dirq --version: $$dirq_v"; \
	 dircue_num="$$(echo "$$dircue_v" | grep -oE '[0-9][^ ]*$$')"; \
	 dirq_num="$$(echo "$$dirq_v" | grep -oE '[0-9][^ ]*$$')"; \
	 [ "$$dircue_num" = "$$dirq_num" ] || (echo "error: dirq version number differs from dircue ($$dircue_num vs $$dirq_num)" >&2 && exit 1)
	python3 scripts/declarations_release_smoke.py --candidate "$(RELEASE_SMOKE_DIR)/dircue" --version "$(RELEASE_SMOKE_VERSION)" --output "$(RELEASE_SMOKE_DIR)/declarations.json"
	python3 scripts/formats_release_smoke.py --candidate "$(RELEASE_SMOKE_DIR)/dircue" --version "$(RELEASE_SMOKE_VERSION)" --output "$(RELEASE_SMOKE_DIR)/formats.json"
	python3 scripts/targeted_release_smoke.py --candidate "$(RELEASE_SMOKE_DIR)/dircue" --version "$(RELEASE_SMOKE_VERSION)" --output "$(RELEASE_SMOKE_DIR)/targeted.json"
	python3 scripts/context_release_smoke.py --candidate "$(RELEASE_SMOKE_DIR)/dircue" --version "$(RELEASE_SMOKE_VERSION)" --platform "$$(go env GOOS)-$$(go env GOARCH)" --output "$(RELEASE_SMOKE_DIR)/context.json"

# Fetch the full corpus (~50 repos) into the cache directory.
# Requires network access; dircue itself never fetches.
atlas-fetch: ## Fetch all pinned corpus repos into ATLAS_CACHE
	python3 tests/atlas/fetch.py --cache "$(ATLAS_CACHE)" --all

# Run the differential atlas on the smoke subset (5 repos).
# Includes Linguist comparison when the Docker image is present (make reference to build it).
# Requires: bin/dircue, scc 4.1.0 in GOBIN or PATH, and the smoke repos in ATLAS_CACHE.
atlas-smoke: build ## Run differential atlas on the smoke subset (5 repos); includes Linguist when image present
	@if docker image inspect $(REFERENCE_IMAGE) >/dev/null 2>&1; then \
	  _linguist_flag=""; \
	else \
	  echo "WARNING: $(REFERENCE_IMAGE) not found; skipping Linguist (run 'make reference' to build it)"; \
	  _linguist_flag="--no-linguist"; \
	fi; \
	if [ -z "$(SCC_BIN)" ]; then \
	  echo "Installing scc 4.1.0..."; \
	  _scc_dir=$$(mktemp -d /tmp/dircue-scc-XXXXX); \
	  CGO_ENABLED=0 GOBIN="$$_scc_dir" go install github.com/boyter/scc/v4@v4.1.0; \
	  python3 tests/atlas/run.py --candidate bin/dircue --scc "$$_scc_dir/scc" \
	    --cache "$(ATLAS_CACHE)" --output "$(ATLAS_OUTPUT)" --smoke $$_linguist_flag; \
	else \
	  python3 tests/atlas/run.py --candidate bin/dircue --scc "$(SCC_BIN)" \
	    --cache "$(ATLAS_CACHE)" --output "$(ATLAS_OUTPUT)" --smoke $$_linguist_flag; \
	fi

# Run the full atlas (all corpus repos). Requires Docker for Linguist comparison.
# Pass NO_LINGUIST=1 to skip the Linguist comparison (e.g. when Docker is unavailable).
atlas: build ## Run the full differential atlas (all corpus repos)
	@if [ -z "$(SCC_BIN)" ]; then \
	  echo "Installing scc 4.1.0..."; \
	  _scc_dir=$$(mktemp -d /tmp/dircue-scc-XXXXX); \
	  CGO_ENABLED=0 GOBIN="$$_scc_dir" go install github.com/boyter/scc/v4@v4.1.0; \
	  _scc="$$_scc_dir/scc"; \
	else \
	  _scc="$(SCC_BIN)"; \
	fi; \
	_flags=""; \
	[ -n "$(NO_LINGUIST)" ] && _flags="$$_flags --no-linguist"; \
	python3 tests/atlas/run.py --candidate bin/dircue --scc "$$_scc" \
	  --cache "$(ATLAS_CACHE)" --output "$(ATLAS_OUTPUT)" --all $$_flags

# Generate accuracy cards from hand-labeled ground truth.
# Requires: bin/dircue and the labeled corpus repos in ATLAS_CACHE.
accuracy-cards: build ## Generate accuracy cards into docs/ACCURACY.md and internal/atlas/accuracy_data.json
	python3 tests/atlas/accuracy.py \
	  --binary bin/dircue \
	  --corpus-root "$(ATLAS_CACHE)" \
	  --output docs/ACCURACY.md \
	  --data internal/atlas/accuracy_data.json

# Run the initially blind-labeled map regression gate (issue #75).
# Requires pinned repo clones. See tests/map_corpus/golden_expectations.json
# for the repo list, commit pins, and oracle-file SHA-256s.
# Fetch repos with: python3 tests/map_corpus/fetch_golden.py --dest .cache/golden-repos
# Not added to per-PR CI; run manually or via workflow_dispatch.
GOLDEN_REPOS ?= .cache/golden-repos
golden: build ## Run golden map-corpus gate (needs pinned repo clones in GOLDEN_REPOS)
	@echo "==> Running golden gate (repos: $(GOLDEN_REPOS))"
	@python3 tests/map_corpus/verify_golden.py \
	  --labels tests/map_corpus/golden_expectations.json \
	  --binary bin/dircue \
	  --repos "$(GOLDEN_REPOS)" \
	  --output tests/map_corpus/golden_results.json

# Check that dircue map exits 0 and emits a valid JSON map document for every
# immediate subdirectory under CORPUS_ROOT.  Run after building.
# Example: make corpus-availability CORPUS_ROOT=.cache/corpus
CORPUS_ROOT ?= .cache/corpus
corpus-availability: build ## Check map availability over every repo in CORPUS_ROOT
	@echo "==> Running corpus availability gate (root: $(CORPUS_ROOT))"
	@python3 tests/map_corpus/verify_corpus_availability.py \
	  --binary bin/dircue \
	  --root "$(CORPUS_ROOT)"

# ---------------------------------------------------------------------------
# Issue #84: Syft oracle (independent package-coverage oracle)
#
# Verifies that `dircue map --attach syft-json=<report>` correctly imports
# each committed Syft report and reflects it in the packages coverage question.
#
# The Syft reports in tests/syft-oracle/fixtures/ are generated with:
#   SYFT_IMAGE="anchore/syft@sha256:500e2d872ac019436926e8322b4fc1f39441d94d21f6f4046c6ff29b30e8cb02"
#   docker run --rm --network none \
#     -v "$PWD/tests/syft-oracle/fixtures/go-single:/src:ro" \
#     "$SYFT_IMAGE" scan dir:/src -o syft-json -q \
#     > tests/syft-oracle/fixtures/go-single.syft.json
#   docker run --rm --network none \
#     -v "$PWD/tests/syft-oracle/fixtures/multi:/src:ro" \
#     "$SYFT_IMAGE" scan dir:/src -o syft-json -q \
#     > tests/syft-oracle/fixtures/multi.syft.json
#
# Usage:
#   make syft-oracle              # run against committed reports
SYFT_BINARY ?= bin/dircue

.PHONY: syft-oracle

syft-oracle: build ## Run Syft oracle: validate package-coverage binding against committed Syft reports
	python3 tests/syft-oracle/run.py --binary $(SYFT_BINARY)

# ---------------------------------------------------------------------------
# Issues #77/#78/#79/#82: Tool oracle (Noir, ruff, semgrep)
#
# Validates dircue's ingestion of real analyzer reports:
#   #77: OWASP Noir JSON/SARIF on web-framework fixtures
#   #78: coverage ledger blind spots (not_run, unsupported_language, ...)
#   #79: analyzer routing per component
#   #82: dircue map locate with three SARIF emitters (Noir, ruff, semgrep)
#
# Fast committed-report checks; no Docker required.
# To regenerate reports: make regenerate-tool-fixtures
TOOL_ORACLE_BINARY ?= bin/dircue

.PHONY: tool-oracles regenerate-tool-fixtures

tool-oracles: build ## Run tool oracle: validate Noir/ruff/semgrep report ingestion and SARIF locate
	python3 tests/tools/run.py --binary $(TOOL_ORACLE_BINARY)

regenerate-tool-fixtures: ## Regenerate tool fixture reports with pinned images (requires Docker + network for pull)
	@echo "==> Pulling pinned images (network required; subsequent runs use --network none)"
	docker pull "ghcr.io/owasp-noir/noir@sha256:4f39307465326433b281508b5ffc433ec31cd150d7fd8f69167946c8ffb689ab"
	docker pull "ghcr.io/astral-sh/ruff@sha256:45cb2b28f0ad694917b159c65d058f1eeafdda0cb155a30374194c4c4b6c56df"
	docker pull "semgrep/semgrep@sha256:f435f06d2332f24d76a93791c8c5bd8c5bef7b426061eb04ff452a9d41e1b596"
	@echo "==> Regenerating OWASP Noir reports (network=none)"
	for fixture in flask-app express-app spring-app; do \
	  docker run --rm --network none \
	    -v "$(PWD)/tests/tools/fixtures/$${fixture}:/app:ro" \
	    "ghcr.io/owasp-noir/noir@sha256:4f39307465326433b281508b5ffc433ec31cd150d7fd8f69167946c8ffb689ab" \
	    noir scan /app -f json 2>/dev/null \
	    > "tests/tools/fixtures/$${fixture}.noir.json"; \
	  docker run --rm --network none \
	    -v "$(PWD)/tests/tools/fixtures/$${fixture}:/app:ro" \
	    "ghcr.io/owasp-noir/noir@sha256:4f39307465326433b281508b5ffc433ec31cd150d7fd8f69167946c8ffb689ab" \
	    noir scan /app -f sarif 2>/dev/null \
	    > "tests/tools/fixtures/$${fixture}.noir.sarif.json"; \
	done
	@echo "==> Regenerating ruff SARIF reports (network=none)"
	docker run --rm --network none \
	  -v "$(PWD)/tests/tools/fixtures/python-lint-sample:/src" \
	  -w /src \
	  "ghcr.io/astral-sh/ruff@sha256:45cb2b28f0ad694917b159c65d058f1eeafdda0cb155a30374194c4c4b6c56df" \
	  check --output-format sarif . 2>/dev/null \
	  > tests/tools/fixtures/python-lint-sample.ruff.sarif.json; true
	docker run --rm --network none \
	  -v "$(PWD)/tests/tools/fixtures/flask-app:/src" \
	  -w /src \
	  "ghcr.io/astral-sh/ruff@sha256:45cb2b28f0ad694917b159c65d058f1eeafdda0cb155a30374194c4c4b6c56df" \
	  check --output-format sarif . 2>/dev/null \
	  > tests/tools/fixtures/flask-app.ruff.sarif.json; true
	@echo "==> Regenerating Semgrep SARIF reports (network=none)"
	docker run --rm --network none \
	  -v "$(PWD)/tests/tools/fixtures/python-lint-sample:/src:ro" \
	  -v "$(PWD)/tests/tools/fixtures/semgrep-rules:/rules:ro" \
	  "semgrep/semgrep@sha256:f435f06d2332f24d76a93791c8c5bd8c5bef7b426061eb04ff452a9d41e1b596" \
	  semgrep --config /rules/python-checks.yaml --metrics off --sarif /src 2>/dev/null \
	  > tests/tools/fixtures/python-lint-sample.semgrep.sarif.json
	@echo "==> Reports regenerated; run make tool-oracles to validate"
