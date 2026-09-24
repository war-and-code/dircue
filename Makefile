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
