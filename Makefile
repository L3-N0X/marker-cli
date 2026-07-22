BIN     := marker-cli
MODULE  := github.com/l3-n0x/marker-cli

# Overridable by packagers: make PREFIX=/usr DESTDIR=pkg install
PREFIX  ?= /usr/local
DESTDIR ?=
BINDIR   = $(DESTDIR)$(PREFIX)/bin
SHAREDIR = $(DESTDIR)$(PREFIX)/share
DOCDIR   = $(SHAREDIR)/doc/$(BIN)

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

# GOFLAGS comes from the environment when set (makepkg exports its own).
GOFLAGS    ?= -trimpath
GO_LDFLAGS := -s -w -X $(MODULE)/internal/cmd.version=$(VERSION)

INSTALL := install

GORELEASER := go run github.com/goreleaser/goreleaser/v2@latest

.PHONY: all build test vet fmt check completions install uninstall snapshot clean

all: build

build: $(BIN)

$(BIN):
	go build $(GOFLAGS) -ldflags '$(GO_LDFLAGS)' -o $(BIN) .

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

check: vet test

completions: $(BIN)
	@mkdir -p completions
	./$(BIN) completion bash > completions/$(BIN).bash
	./$(BIN) completion zsh  > completions/_$(BIN)
	./$(BIN) completion fish > completions/$(BIN).fish

install: build completions
	$(INSTALL) -Dm755 $(BIN) $(BINDIR)/$(BIN)
	$(INSTALL) -Dm644 completions/$(BIN).bash $(SHAREDIR)/bash-completion/completions/$(BIN)
	$(INSTALL) -Dm644 completions/_$(BIN)     $(SHAREDIR)/zsh/site-functions/_$(BIN)
	$(INSTALL) -Dm644 completions/$(BIN).fish $(SHAREDIR)/fish/vendor_completions.d/$(BIN).fish
	$(INSTALL) -Dm644 README.md $(DOCDIR)/README.md

uninstall:
	rm -f $(BINDIR)/$(BIN)
	rm -f $(SHAREDIR)/bash-completion/completions/$(BIN)
	rm -f $(SHAREDIR)/zsh/site-functions/_$(BIN)
	rm -f $(SHAREDIR)/fish/vendor_completions.d/$(BIN).fish
	rm -rf $(DOCDIR)

# Build all release archives locally into dist/ without publishing anything.
snapshot:
	$(GORELEASER) release --snapshot --clean --skip=publish

clean:
	rm -f $(BIN)
	rm -rf completions dist
