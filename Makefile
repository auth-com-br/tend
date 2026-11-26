# Go is not always on PATH — the toolchain may live in ~/.local/go. Resolve it
# here so `make` works regardless of how the shell is configured. Override with
# `make GO=/path/to/go`.
GO ?= $(shell command -v go 2>/dev/null || echo $(HOME)/.local/go/bin/go)

# gofmt ships beside go, so it is found the same way rather than trusted to be
# on PATH. It was not, and `gofmt -l .` printing nothing because the binary is
# missing looks exactly like a tree that is already formatted — so the format
# check passed for weeks without running.
GOFMT ?= $(shell command -v gofmt 2>/dev/null || echo $(dir $(GO))gofmt)

# On macOS the tests get a temporary directory of their own, short and with no
# symlink in it. The default, /var/folders/../T/, broke over a hundred tests
# there: a unix socket's path is capped at 104 bytes and the servers' sockets
# under it did not fit ("bind: invalid argument"), and /var is a link to
# /private/var, so a directory a process reports was never the one the test
# made. Linux's /tmp has neither problem and is left alone.
ifeq ($(shell uname -s),Darwin)
export TMPDIR := $(shell mkdir -p /private/tmp/t && echo /private/tmp/t)
endif


# Packages the fast loop covers. It is everything with tests: narrowing it
# further once cost the CLI its coverage, which is where the last real bug was.
# Narrow it deliberately when the tree grows enough to need it.
DEV_PKGS ?= ./...

# `make` with no arguments runs the fast loop, not the first target in the file.
.DEFAULT_GOAL := dev

# SESSION is which session `make restart` acts on.
SESSION ?= default

.PHONY: toolchain run dev watch test test-race check fmt vet bench build install dist manifest packages stats clean restart desktop desktop-check desktop-install whatsapp whatsapp-check whatsapp-install deb appimage

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
check: toolchain fmt vet test test-race whatsapp-check

fmt:
	@command -v $(GOFMT) >/dev/null 2>&1 || test -x $(GOFMT) || { \
		echo "gofmt not found at $(GOFMT)"; \
		echo "override with: make GOFMT=/path/to/gofmt"; \
		exit 1; }
	@# Hidden directories are skipped, as go vet and go test skip them: a git
	@# worktree under .claude/ (an agent's work in progress) made the gate red
	@# for a file that is not this tree's. gofmt walks the directories that
	@# are not hidden (ls -d */ lists no hidden one) and the top's own Go
	@# files; what it cannot parse fails the gate too.
	@out="$$($(GOFMT) -l $$(ls -d */) $$(ls *.go 2>/dev/null) 2>&1)"; \
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

## packages: write dist/tend.rb (Homebrew) and dist/tend.nix (Nix) from the
## published release's checksums: make packages (after gh release create).
packages: toolchain
	$(GO) run ./tools/packages -version $(VERSION) -dir dist

## desktop: build tend-desktop, the Qt window on a session (desktop/, its own
## module: cgo and Qt 6 live there, so `make check` and the tend binary stay
## pure Go). Needs Qt 6 with Qt Quick and pkg-config; see docs/DESKTOP.md.
## The first build compiles the Qt bindings and takes a few minutes.
desktop: toolchain
	cd desktop && $(GO) generate ./internal/qtterm
	cd desktop && $(GO) build -ldflags "$(LDFLAGS)" -o bin/tend-desktop ./cmd/tend-desktop

## desktop-check: the desktop's gate — gofmt, vet, its tests (against a real
## tend server, and the weight budget, offscreen) and the race detector.
desktop-check: toolchain
	cd desktop && $(GO) generate ./internal/qtterm
	@out="$$($(GOFMT) -l desktop)"; \
	if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	cd desktop && $(GO) vet ./...
	cd desktop && $(GO) test ./...
	cd desktop && $(GO) test -race -short ./...

## desktop-install: install tend-desktop beside tend, so it starts sessions
## with the tend of the same version (core.FindTend looks beside itself first).
desktop-install: desktop
	@mkdir -p "$(INSTALL_DIR)"
	cp desktop/bin/tend-desktop "$(INSTALL_DIR)/tend-desktop"
	@echo "installed $(INSTALL_DIR)/tend-desktop ($(VERSION))"

## whatsapp: build tend-whatsapp, the owner's WhatsApp for the agents
## (whatsapp/, its own module). Its own because of its weight: whatsmeow and
## the pure-Go SQLite made tend 16 MB heavier (22.2 to 38.3 MB) when
## measured. Pure Go like tend, so it is built static the same way.
whatsapp: toolchain
	cd whatsapp && $(STATIC) $(GO) build -ldflags "$(LDFLAGS)" -o bin/tend-whatsapp ./cmd/tend-whatsapp

## whatsapp-check: its vet, tests and race detector. Part of `make check`:
## unlike the desktop it needs nothing but Go, so nothing excuses it.
whatsapp-check: toolchain
	cd whatsapp && $(GO) vet ./...
	cd whatsapp && $(GO) test ./...
	cd whatsapp && $(GO) test -race ./...

## whatsapp-install: install tend-whatsapp beside tend, where `tend whatsapp`
## looks for it first.
whatsapp-install: whatsapp
	@mkdir -p "$(INSTALL_DIR)"
	cp whatsapp/bin/tend-whatsapp "$(INSTALL_DIR)/tend-whatsapp"
	@echo "installed $(INSTALL_DIR)/tend-whatsapp ($(VERSION))"

## deb: the Debian packages of the three programs, for this machine's
## architecture, in dist/: tend_<v>_<arch>.deb, tend-desktop_… and
## tend-whatsapp_… (tools/deb). Needs dpkg-deb, dpkg-shlibdeps (dpkg-dev) and
## what `make desktop` needs; NO_DESKTOP=1 leaves the desktop's out, for a
## machine without Qt. Run it after `make dist`, which empties dist/.
# Built here and not cross-compiled: tend-desktop is cgo against this
# machine's Qt, so its package is for the distribution it was built on, and
# its Depends are read from the binary by dpkg-shlibdeps.
deb: build whatsapp $(if $(NO_DESKTOP),,desktop)
	$(GO) run ./tools/deb -version $(VERSION) -out dist -tend bin/tend \
		-whatsapp whatsapp/bin/tend-whatsapp $(if $(NO_DESKTOP),,-desktop desktop/bin/tend-desktop)

## appimage: dist/tend-desktop-<v>-<arch>.AppImage, tend-desktop with its Qt
## inside (tools/appimage). Needs appimagetool on PATH (or APPIMAGETOOL=...),
## which is a download; without it the AppDir is built and left in
## dist/tend-desktop.AppDir, which runs as it is.
# Linked again with an rpath, not run under LD_LIBRARY_PATH: every program
# the window starts — the tend server, and the shells and agents it starts —
# would inherit the variable and load the AppImage's libraries, from a
# mount that is gone once the window closes. (Seen: LD_DEBUG set on the
# window reached the shell in its pane.) --disable-new-dtags makes it a
# DT_RPATH, which unlike DT_RUNPATH also serves the bundled libraries' own
# dependencies and the plugins Qt opens.
APPIMAGETOOL ?= appimagetool
appimage: toolchain
	cd desktop && $(GO) generate ./internal/qtterm
	cd desktop && $(GO) build -ldflags "$(LDFLAGS) -extldflags '-Wl,-rpath,\$$ORIGIN/../lib -Wl,--disable-new-dtags'" \
		-o bin/tend-desktop-appimage ./cmd/tend-desktop
	$(GO) run ./tools/appimage -version $(VERSION) -binary desktop/bin/tend-desktop-appimage \
		-out dist -appimagetool "$(APPIMAGETOOL)"

clean:
	rm -rf bin dist desktop/bin whatsapp/bin
	$(GO) clean -testcache
