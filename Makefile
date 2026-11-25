.POSIX:

.PHONY: all ci native
all: native js test-wasm
ci: native js-check
native: gen lint build test

.PHONY: fmt
fmt:
	prefix "$@" ./ci/sub/bin/fmt.sh
.PHONY: gen
gen: fmt
	prefix "$@" ./ci/gen.sh
.PHONY: lint
lint: fmt
	prefix "$@" go vet --composites=false ./...
.PHONY: build
build: fmt
	prefix "$@" go build ./...
.PHONY: test
test: fmt
	prefix "$@" ./ci/test.sh
.PHONY: test-wasm
test-wasm: fmt
	prefix "$@" ./ci/test-wasm.sh
.PHONY: race
race: fmt
	prefix "$@" ./ci/test.sh --race ./...
.PHONY: js
js: gen
	cd d2js/js && NPM_VERSION="${NPM_VERSION}" prefix "$@" ./make.sh all
.PHONY: js-check
js-check: fmt
	cd d2js/js && prefix "$@" ./make.sh check
