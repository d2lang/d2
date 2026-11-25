.POSIX:
.PHONY: all
all: fmt build test

.PHONY: check
check: fmt
	prefix "$@" bun run test:types

.PHONY: fmt
fmt: node_modules
	# Bun already installed the dependencies. Select the shared formatting jobs
	# by their own names, including when the root Makefile prefixes this job.
	unset JOBNAME; ../../ci/sub/bin/fmt.sh '^(mdtocsubst|go\.mod|gofmt|prettier|d2fmt)$$'

.PHONY: build
build: fmt
	prefix "$@" ./ci/build.sh

.PHONY: dev
dev: build
	prefix "$@" bun run dev

.PHONY: test
test: build
	prefix "$@" bun test:all

.PHONY: node_modules
node_modules:
	prefix "$@" bun install $${CI:+--frozen-lockfile}
