.PHONY: all build test lint clean migrate

GO ?= go
BINDIR := bin
SERVER_BIN := $(BINDIR)/quicktun-server

all: build

build: $(SERVER_BIN)

$(SERVER_BIN):
	@mkdir -p $(BINDIR)
	$(GO) build -o $@ ./cmd/quicktun-server

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

lint:
	$(GO) vet ./...

clean:
	rm -rf $(BINDIR) coverage.txt

migrate: build
	$(SERVER_BIN) migrate --config etc/server.yaml
