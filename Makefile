# Go is not always on PATH — the toolchain may live in ~/.local/go. Resolve it
# here so `make` works regardless of how the shell is configured. Override with
# `make GO=/path/to/go`.
GO ?= $(shell command -v go 2>/dev/null || echo $(HOME)/.local/go/bin/go)

# gofmt ships beside go, so it is found the same way rather than trusted to be
# on PATH. It was not, and `gofmt -l .` printing nothing because the binary is
# missing looks exactly like a tree that is already formatted — so the format
# check passed for weeks without running.
GOFMT ?= $(shell command -v gofmt 2>/dev/null || echo $(dir $(GO))gofmt)


# Packages the fast loop covers. It is everything with tests: narrowing it
# further once cost the CLI its coverage, which is where the last real bug was.
# Narrow it deliberately when the tree grows enough to need it.
DEV_PKGS ?= ./...

# `make` with no arguments runs the fast loop, not the first target in the file.
.DEFAULT_GOAL := dev

# SESSION is which session `make restart` acts on.
SESSION ?= default

.PHONY: toolchain run dev watch test test-race check fmt vet bench build install dist manifest stats clean restart

## toolchain: fail with a usable message instead of "go: No such file or directory".
toolchain:
	@command -v $(GO) >/dev/null 2>&1 || test -x $(GO) || { \
		echo "go not found: looked on PATH and in ~/.local/go/bin"; \
		echo "install Go, or run: make GO=/path/to/go"; \
		exit 1; }

## dev: fastest feedback loop — cached, stops at the first failure, skips vet.
# `go test` runs vet by default; -vet=off is most of the speedup here. Use
# `make check` before committing, which puts vet back.
dev: toolchain
	@$(GO) test -vet=off -failfast -short $(DEV_PKGS)

## watch: re-run `make dev` whenever a .go file changes.
# Polls instead of depending on entr/fswatch/watchexec, none of which are
# assumed to be installed. The signature covers both the file list and the
# file contents, so adds, removes and edits all trigger while a bare touch
# does not. cksum keeps it portable across Linux and macOS.
watch:
	@echo "watching .go files — ctrl-c to stop"
	@last=""; \
	while true; do \
		sig=$$( { find . -name '*.go' -type f | sort; \
		          find . -name '*.go' -type f -exec cat {} + ; } | cksum ); \
		if [ "$$sig" != "$$last" ]; then \
			last="$$sig"; \
			printf '\033[2J\033[H'; \
			date +'--- %H:%M:%S ---'; \
			$(MAKE) --no-print-directory dev || true; \
		fi; \
		sleep 1; \
	done

## run: build and run the CLI. Pass arguments with ARGS="...".
#   make run
#   make run ARGS="agents -v"
#   make run ARGS="watch -- claude"
run: toolchain
	@$(GO) run ./cmd/tend $(ARGS)

## restart: stop a session's server and attach to a fresh one.
# The server outlives the client, so building a new binary and running it
# leaves yesterday's server answering. This is the loop during development.
# It closes every pane in the session, which is the point.
#   make restart
#   make restart SESSION=work
restart: build
	@./bin/tend kill -s $(SESSION) -server 2>/dev/null || true
	@./bin/tend attach -s $(SESSION)

## test: full test run.
test: toolchain
	$(GO) test ./...

## test-race: full run under the race detector.
test-race: toolchain
	$(GO) test -race ./...

## check: what must pass before committing.
# The race detector is part of the gate, not an extra: the server runs a
# goroutine per pane plus a detection loop, and a race found after committing
# is a race found the expensive way.
check: toolchain fmt vet test test-race

fmt:
	@command -v $(GOFMT) >/dev/null 2>&1 || test -x $(GOFMT) || { \
		echo "gofmt not found at $(GOFMT)"; \
		echo "override with: make GOFMT=/path/to/gofmt"; \
		exit 1; }
	@out="$$($(GOFMT) -l .)"; \
	if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

vet: toolchain
	$(GO) vet ./...

## bench: hot-path benchmarks. internal/vt must stay at 0 allocs/op.
bench: toolchain
	$(GO) test -bench=. -benchmem -run=XXX ./internal/vt/

# VERSION comes from a git tag when there is one, and from the commit
# otherwise, so a binary can always be traced back to what built it.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo unknown)
LDFLAGS := -X main.version=$(VERSION)

# Static: tend is pure Go (no cgo, by the owner's decision), and a binary
# linked against this machine's glibc is not one that runs everywhere. A
# remote attach copies it to the other machine, and there a newer glibc's
# binary crashed on an older one (2.43 here, 2.35 there). Only for the
# binaries: the race detector the tests run under needs cgo.
STATIC := CGO_ENABLED=0

# INSTALL_DIR is GOBIN when set, and the usual per-user bin otherwise.
INSTALL_DIR ?= $(shell $(GO) env GOBIN 2>/dev/null)
ifeq ($(INSTALL_DIR),)
INSTALL_DIR := $(HOME)/.local/bin
endif

build: toolchain
	$(STATIC) $(GO) build -ldflags "$(LDFLAGS)" -o bin/tend ./cmd/tend

## install: build and put tend on PATH, in GOBIN or ~/.local/bin.
install: toolchain
	@mkdir -p "$(INSTALL_DIR)"
	$(STATIC) $(GO) build -ldflags "$(LDFLAGS)" -o "$(INSTALL_DIR)/tend" ./cmd/tend
	@echo "installed $(INSTALL_DIR)/tend ($(VERSION))"
	@command -v tend >/dev/null 2>&1 || echo "note: $(INSTALL_DIR) is not on PATH"
	@# A GOBIN set by a version manager (asdf does) installs somewhere an
	@# older tend earlier on PATH shadows: the build is new and what runs is
	@# not, which looked exactly like a feature that did not work.
	@found=$$(command -v tend 2>/dev/null); \
	if [ -n "$$found" ] && [ "$$found" != "$(INSTALL_DIR)/tend" ]; then \
		echo "warning: 'tend' on PATH is $$found, not the one just installed"; \
		echo "         run: install -m 755 \"$(INSTALL_DIR)/tend\" \"$$found\"  (or set INSTALL_DIR)"; \
	fi

## dist: cross-compile release binaries into dist/.
dist: toolchain
	@rm -rf dist && mkdir -p dist
	@for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do \
		os=$${target%/*}; arch=$${target#*/}; \
		echo "  $$os/$$arch"; \
		$(STATIC) GOOS=$$os GOARCH=$$arch $(GO) build -ldflags "$(LDFLAGS)" \
			-o "dist/tend-$$os-$$arch" ./cmd/tend || exit 1; \
	done
	@cd dist && sha256sum tend-* > SHA256SUMS
	@ls -1 dist

## stats: how tend is being taken up, from what GitHub shows publicly (tools/stats).
# HISTORY=stats.jsonl keeps each run and shows the change in a week.
stats: toolchain
	$(GO) run ./tools/stats $(if $(HISTORY),-history $(HISTORY))

## manifest: write dist/latest.json, the update manifest, from the release's
## notes: make manifest NOTES=notes.md (after make dist, on the release's tag).
# The same notes are the GitHub release's text. Published with the release,
# the manifest is what every tend's background check reads.
manifest: toolchain
	@test -n "$(NOTES)" || { echo "usage: make manifest NOTES=notes.md"; exit 1; }
	$(GO) run ./tools/manifest -version $(VERSION) -notes $(NOTES) -dir dist

clean:
	rm -rf bin dist
	$(GO) clean -testcache
