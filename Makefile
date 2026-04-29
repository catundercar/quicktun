.PHONY: all build test test-race lint clean migrate sync-migrations

GO ?= go
BINDIR := bin
SERVER_BIN := $(BINDIR)/quicktun-server

all: build

build: $(SERVER_BIN)

$(SERVER_BIN): sync-migrations
	@mkdir -p $(BINDIR)
	$(GO) build -o $@ ./cmd/quicktun-server

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

lint:
	$(GO) vet ./...

sync-migrations:
	@cp migrations/*.sql internal/migration/files/

clean:
	rm -rf $(BINDIR) coverage.txt

migrate: build
	$(SERVER_BIN) migrate --config etc/server.yaml
