VERSION ?= 1.0.0-dev
REFERENCE_IMAGE ?= dircue-linguist:9.7.0
RELEASE_DIR ?= dist
WHEEL_DIR ?= $(RELEASE_DIR)/wheels

.PHONY: build test check bench reference conformance samples release release-archives

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

reference:
	docker build -t $(REFERENCE_IMAGE) -f tests/conformance/Dockerfile tests/conformance

conformance: reference
	python3 tests/conformance/run.py --image $(REFERENCE_IMAGE)

samples: reference
	python3 tests/conformance/samples.py --image $(REFERENCE_IMAGE) --require-match

release:
	@python3 -c 'import sys; sys.path.insert(0, "scripts"); from wheels import python_version; python_version(sys.argv[1])' "$(VERSION)"
	$(MAKE) release-archives
	python3 scripts/wheels.py --release-dir "$(RELEASE_DIR)" --output "$(WHEEL_DIR)"

release-archives:
	python3 scripts/release.py --version "$(VERSION)" --output "$(RELEASE_DIR)"
