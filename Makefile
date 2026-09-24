VERSION ?= 1.0.0-dev
REFERENCE_IMAGE ?= dircue-linguist:9.7.0
RELEASE_DIR ?= dist
WHEEL_DIR ?= $(RELEASE_DIR)/wheels
ATLAS_CACHE ?= .cache/atlas-repos
ATLAS_OUTPUT ?= .cache/atlas

.PHONY: build test check bench reference conformance public-conformance classifier-window samples release release-archives hostile-fs forest-e2e atlas-fetch atlas atlas-smoke accuracy-cards

build:
	CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags '-s -w -X dircue/internal/cli.Version=$(VERSION)' -o bin/dircue .

test:
	go test -race ./...

check:
	go vet ./...
	go test -race ./...
	cd third_party/go-enry && go test ./...
	cd third_party/go-git && go test -race . ./plumbing/format/packfile ./storage/filesystem ./storage/filesystem/dotgit

bench:
	go test ./pkg/scanner -run '^$$' -bench . -benchmem

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
# Atlas parity and accuracy targets
# ---------------------------------------------------------------------------

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

# ---------------------------------------------------------------------------
# Mutation testing (issue #86)
# ---------------------------------------------------------------------------
# Scope: the five core packages most critical to map correctness and coverage
# honesty. Run on demand; never on a schedule. gremlins@v0.6.0 is pinned.
#
# MUTATION_PKGS can be overridden to target a single package:
#   make mutation MUTATION_PKGS=./pkg/sariflocate
# MUTATION_TIMEOUT_COEFF controls how many multiples of baseline test time
# gremlins waits before declaring a mutant "timed out" (default 20).
MUTATION_PKGS     ?= ./pkg/mapdoc ./pkg/treehash ./pkg/providerjoin ./pkg/sariflocate ./pkg/mapdiff
MUTATION_TIMEOUT_COEFF ?= 20
MUTATION_OUTPUT   ?= tests/mutation/results.json

.PHONY: mutation

mutation: ## Run mutation testing on core packages (on-demand; slow)
	@echo "Installing gremlins@v0.6.0..."
	@_gdir=$$(mktemp -d /tmp/dircue-gremlins-XXXXX); \
	GOBIN="$$_gdir" CGO_ENABLED=0 go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0; \
	mkdir -p tests/mutation; \
	echo "Running mutation testing on: $(MUTATION_PKGS)"; \
	for pkg in $(MUTATION_PKGS); do \
	  echo "=== $$pkg ==="; \
	  "$$_gdir/gremlins" unleash \
	    --timeout-coefficient $(MUTATION_TIMEOUT_COEFF) \
	    -o tests/mutation/$$(echo "$$pkg" | tr '/' '-' | tr '.' '-').json \
	    $$pkg; \
	done; \
	echo "Results written to tests/mutation/"

# ---------------------------------------------------------------------------
# On-demand fuzzing campaigns (issue #87)
# ---------------------------------------------------------------------------
# Runs every Go fuzz target in all packages for FUZZ_TIME seconds each.
# Seed corpora are in testdata/fuzz/ beside each package (committed).
# Findings (crashers/corpora) accumulate in FUZZ_CACHE when set.
#
# Usage:
#   make fuzz-campaign                     # run all targets, 30 s each
#   make fuzz-campaign FUZZ_TIME=300       # run all targets, 5 min each
#   make fuzz-campaign FUZZ_PKG=./pkg/treehash  # single package
#
# When a crasher is found, save the input under the package's
# testdata/fuzz/<FuzzTarget>/ directory as a regression seed and commit it.
FUZZ_TIME  ?= 30
FUZZ_PKG   ?= ./...
FUZZ_CACHE ?=

.PHONY: fuzz-campaign

fuzz-campaign: build ## Run all Go fuzz targets on demand (FUZZ_TIME seconds each)
	@_failed=0; \
	for _pkg_path in $$(go list -f '{{.Dir}}' $(FUZZ_PKG) 2>/dev/null); do \
	  _fuzz_funcs=$$(grep -rh '^func Fuzz' "$$_pkg_path"/*_test.go 2>/dev/null | sed 's/func \(Fuzz[A-Za-z0-9_]*\).*/\1/'); \
	  if [ -z "$$_fuzz_funcs" ]; then continue; fi; \
	  _import=$$(go list $$_pkg_path 2>/dev/null); \
	  for _fn in $$_fuzz_funcs; do \
	    echo ">>> fuzzing $$_import -run=^$${_fn}$$ ($(FUZZ_TIME)s)"; \
	    _args="-test.fuzz=^$${_fn}$$ -test.fuzztime=$(FUZZ_TIME)s -test.run=^$${_fn}$$"; \
	    if [ -n "$(FUZZ_CACHE)" ]; then \
	      _args="$$_args -test.fuzzcachedir=$(FUZZ_CACHE)/$$_fn"; \
	    fi; \
	    if ! go test -count=1 $$_pkg_path -fuzz="^$${_fn}$$" -fuzztime="$(FUZZ_TIME)s" -run="^$${_fn}$$" 2>&1; then \
	      echo "CRASHER in $$_import $$_fn — save failing input as a seed"; \
	      _failed=$$((_failed + 1)); \
	    fi; \
	  done; \
	done; \
	if [ $$_failed -gt 0 ]; then \
	  echo "$$_failed fuzz target(s) found a crasher — fix and commit seeds"; exit 1; \
	fi; \
	echo "Fuzz campaign complete: no new crashers."

# ---------------------------------------------------------------------------
# Issue #84: Syft oracle (independent package-coverage oracle)
#
# Verifies that `dircue map --attach syft-json=<fixture>` correctly integrates
# a pre-generated Syft JSON report and reflects it in the packages coverage
# question. The fixture is committed; no network access is required at run time.
#
# To regenerate the fixture (requires Docker and a network connection):
#   docker run --rm -v "$PWD/tests/compatibility_next/composition/fixture:/src" \
#     anchore/syft:1.20.0 scan /src -o json > tests/syft-oracle/fixtures/composition-syft.json
#
# Usage:
#   make syft-oracle              # run with the default binary (bin/dircue)
#   make syft-oracle BINARY=./bin/dircue-candidate
SYFT_BINARY ?= bin/dircue

.PHONY: syft-oracle

syft-oracle: build ## Run Syft oracle: verify package-coverage binding with a pre-generated Syft report
	python3 tests/syft-oracle/run.py --binary $(SYFT_BINARY)
