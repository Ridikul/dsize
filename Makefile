# dsize — build & install
#
# Common targets:
#   make build      compile ./src into ./src/dsize
#   make install    build, then copy the binary onto your PATH (BINDIR)
#   make uninstall  remove the installed binary
#   make test       run go vet + the test suite
#   make clean      remove build output
#
# Override the install location with BINDIR, e.g.:
#   make install BINDIR=~/bin

BINARY := dsize
SRC    := src
PKG    := ./cmd/dsize

# Default install dir: Homebrew's bin on Apple Silicon if present, else
# /usr/local/bin. Override with `make install BINDIR=...`.
BINDIR ?= $(shell test -d /opt/homebrew/bin && echo /opt/homebrew/bin || echo /usr/local/bin)

# Use sudo automatically when BINDIR is not writable by the current user.
INSTALL_SUDO := $(shell test -w "$(BINDIR)" || echo sudo)

.PHONY: build install uninstall test vet clean

build:
	cd $(SRC) && go build -o $(BINARY) $(PKG)
	@echo "built $(SRC)/$(BINARY)"

install: build
	$(INSTALL_SUDO) install -m 0755 $(SRC)/$(BINARY) "$(BINDIR)/$(BINARY)"
	@echo "installed $(BINDIR)/$(BINARY)"

uninstall:
	$(INSTALL_SUDO) rm -f "$(BINDIR)/$(BINARY)"
	@echo "removed $(BINDIR)/$(BINARY)"

test: vet
	cd $(SRC) && go test ./...

vet:
	cd $(SRC) && go vet ./...

clean:
	rm -f $(SRC)/$(BINARY)
	@echo "cleaned"
