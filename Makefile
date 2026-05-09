GO      ?= go
BINDIR  ?= /usr/local/bin

.PHONY: build build-all build-server install uninstall reinstall clean test lint

build:
	$(GO) build -o agentlink ./cmd/agentlink/

build-all:
	GOOS=linux GOARCH=amd64 $(GO) build -o bin/agentlink-linux-amd64 ./cmd/agentlink/
	GOOS=linux GOARCH=arm64 $(GO) build -o bin/agentlink-linux-arm64 ./cmd/agentlink/
	GOOS=darwin GOARCH=amd64 $(GO) build -o bin/agentlink-darwin-amd64 ./cmd/agentlink/
	GOOS=darwin GOARCH=arm64 $(GO) build -o bin/agentlink-darwin-arm64 ./cmd/agentlink/

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
