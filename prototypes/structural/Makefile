CARGO ?= cargo
GO ?= go

.PHONY: build test demo clean

build:
	$(CARGO) build --locked --release --manifest-path worker/Cargo.toml
	mkdir -p bin
	cd driver && CGO_ENABLED=0 $(GO) build -trimpath -o ../bin/dircue-structural-prototype .

# Both test suites exercise the prototype, separately from the production CLI.
test:
	$(CARGO) test --locked --manifest-path worker/Cargo.toml
	cd driver && $(GO) test -race ./...

demo: build
	bin/dircue-structural-prototype --worker worker/target/release/dircue-structural-worker tests/fixtures

clean:
	$(CARGO) clean --manifest-path worker/Cargo.toml
	$(GO) clean -C driver
