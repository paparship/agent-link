GO      ?= go
BINDIR  ?= /usr/local/bin

# Version stamped into the binary via -ldflags. CI passes VERSION=<tag> on
# release; local builds fall back to git describe (e.g. v0.3.5-3-gabc123-dirty).
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

.PHONY: build build-all build-server install uninstall reinstall clean test lint

build:
	$(GO) build $(LDFLAGS) -o agentlink ./cmd/agentlink/

# CGO_ENABLED=0 produces static binaries with no glibc dependency, so the
# release artifacts run on any Linux regardless of the build machine's glibc
# version (otherwise they carry the CI runner's GLIBC_2.3x symbol requirements).
build-all:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build $(LDFLAGS) -o bin/agentlink-linux-amd64 ./cmd/agentlink/
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build $(LDFLAGS) -o bin/agentlink-linux-arm64 ./cmd/agentlink/
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GO) build $(LDFLAGS) -o bin/agentlink-darwin-amd64 ./cmd/agentlink/
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GO) build $(LDFLAGS) -o bin/agentlink-darwin-arm64 ./cmd/agentlink/

build-server:
	$(GO) build -o server ./cmd/server/

install: build
	mkdir -p $(BINDIR)
	cp agentlink $(BINDIR)/agentlink

uninstall:
	rm -f $(BINDIR)/agentlink

reinstall: uninstall install

clean:
	rm -f agentlink server
	rm -rf bin

test:
	$(GO) test ./... -count=1

lint:
	$(GO) vet ./...
