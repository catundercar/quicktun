.PHONY: all build test test-race lint clean migrate sync-migrations check-migrations

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
	@rm -f internal/migration/files/*.sql
	@cp migrations/*.sql internal/migration/files/

check-migrations: sync-migrations
	@git diff --exit-code -- internal/migration/files/ \
		|| (echo "ERROR: internal/migration/files/ is out of sync. Run 'make sync-migrations' and commit." && exit 1)

clean:
	rm -rf $(BINDIR) coverage.txt

migrate: build
	$(SERVER_BIN) migrate --config etc/server.yaml
