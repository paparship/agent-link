GO      ?= go
BINDIR  ?= /usr/local/bin

.PHONY: build build-server install uninstall reinstall clean test lint

build:
	$(GO) build -o agentlink ./cmd/agentlink/

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

test:
	$(GO) test ./... -count=1

lint:
	$(GO) vet ./...
