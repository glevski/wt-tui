# Go is not on PATH in some devcontainers, hence the explicit toolchain path.
GO     ?= $(shell command -v go 2>/dev/null || echo /usr/local/go/bin/go)
PREFIX ?= $(HOME)/.local

# Stamped into `wt-ui --version`: the latest tag (with -N-g<sha> when ahead
# of it, per git describe) plus the exact commit. `?=` so a caller building
# outside a checkout — e.g. from a `git archive` tarball, which carries no
# .git — can supply them instead: make install VERSION=0.1.0 COMMIT=7af004a
#
# The binary is wt-ui (the repo is wt-tui); the module path stays wt-tui.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null)
STAMP   := -X wt-tui/internal/cli.version=$(VERSION) -X wt-tui/internal/cli.commit=$(COMMIT)

ifeq ($(strip $(VERSION)$(COMMIT)),)
$(warning no git metadata in $(CURDIR) — building unstamped, `wt-ui --version` will report "dev"; pass VERSION=… COMMIT=… to stamp it)
endif

.PHONY: build install test vet fmt

build:
	$(GO) build -ldflags="$(STAMP)" -o wt-ui .

install:
	$(GO) build -ldflags="-s -w $(STAMP)" -o $(PREFIX)/bin/wt-ui .

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...
