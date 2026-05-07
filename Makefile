.PHONY: all build test test-race lint clean migrate sync-migrations check-migrations proto proto-lint proto-gen proto-clean web-build web-clean

GO ?= go
BINDIR        := bin
SERVER_BIN    := $(BINDIR)/quicktun-server
AGENT_BIN     := $(BINDIR)/quicktun-agent
AUTHPROXY_BIN := $(BINDIR)/quicktun-authproxy
CLI_BIN       := $(BINDIR)/quicktun

# SPA build artifacts. The Vite build emits to web/dist; we then sync into
# internal/server/webui/dist so //go:embed (which cannot reach across the
# module tree) can pick them up. Only the server binary depends on the SPA.
WEB_DIST       := web/dist/index.html
WEB_EMBED_DIR  := internal/server/webui/dist
WEB_EMBED      := $(WEB_EMBED_DIR)/index.html
WEB_SOURCES    := $(shell find web/src web/public web/index.html web/package.json web/vite.config.ts web/tsconfig.json -type f 2>/dev/null)

all: build

build: $(WEB_EMBED) $(SERVER_BIN) $(AGENT_BIN) $(AUTHPROXY_BIN) $(CLI_BIN)

$(WEB_DIST): $(WEB_SOURCES)
	@echo "==> Building web admin SPA"
	cd web && npm install --silent && npm run build

# Sync the Vite output into the Go embed directory. Wipe any previously
# embedded assets first so removed files don't linger in the binary, but
# preserve .gitkeep so an unbuilt tree still satisfies //go:embed.
$(WEB_EMBED): $(WEB_DIST)
	@echo "==> Syncing SPA into $(WEB_EMBED_DIR)"
	@mkdir -p $(WEB_EMBED_DIR)
	@find $(WEB_EMBED_DIR) -mindepth 1 ! -name .gitkeep -delete
	@cp -R web/dist/. $(WEB_EMBED_DIR)/

web-build: $(WEB_EMBED)

web-clean:
	rm -rf web/dist web/node_modules
	@find $(WEB_EMBED_DIR) -mindepth 1 ! -name .gitkeep -delete

$(SERVER_BIN): sync-migrations $(WEB_EMBED)
	@mkdir -p $(BINDIR)
	$(GO) build -o $@ ./cmd/quicktun-server

$(AGENT_BIN):
	@mkdir -p $(BINDIR)
	$(GO) build -o $@ ./cmd/quicktun-agent

$(AUTHPROXY_BIN):
	@mkdir -p $(BINDIR)
	$(GO) build -o $@ ./cmd/quicktun-authproxy

$(CLI_BIN):
	@mkdir -p $(BINDIR)
	$(GO) build -o $@ ./cmd/quicktun

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

proto: proto-lint proto-gen

proto-lint:
	cd api && buf lint

proto-gen:
	cd api && buf generate

proto-clean:
	rm -rf gen/

clean:
	rm -rf $(BINDIR) coverage.txt

migrate: build
	$(SERVER_BIN) migrate --config etc/server.yaml
