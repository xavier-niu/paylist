# Paylist developer shortcuts. Run `make demo`, then open http://localhost:8080
# (or http://<this machine's IP>:8080 from a phone on the same network).

GO ?= $(shell command -v go 2>/dev/null || echo /usr/local/go/bin/go)

DEMO_ADDR     ?= 0.0.0.0:8080
DEMO_DATA     ?= /tmp/paylist-demo
DEMO_EMAIL    ?= demo@admin.com
DEMO_PASSWORD ?= admin

.PHONY: build ui ui-dev demo demo-reset test

NPM ?= $(shell command -v npm 2>/dev/null)
NODE_IMAGE ?= node:22-alpine
UI_DEPS = web/ui/node_modules/.package-lock.json
# Rootful Docker: run as you, so web/ stays yours.
DOCKER_USER = $(shell docker info --format '{{.SecurityOptions}}' 2>/dev/null | grep -q rootless || echo "--user $$(id -u):$$(id -g)")

# The server with its web app built in: ./paylist
build: ui
	$(GO) build -o paylist ./cmd/paylist

# The web app (web/ui: React and shadcn/ui), built into web/dist, which the
# server embeds. Without a local Node, it builds in Node's Docker image.
ifneq ($(NPM),)
ui: $(UI_DEPS)
	cd web/ui && npm run build

$(UI_DEPS): web/ui/package-lock.json
	cd web/ui && npm ci --no-audit --no-fund
else
ui:
	docker run --rm $(DOCKER_USER) -e HOME=/tmp -v "$(CURDIR):/src" -w /src/web/ui $(NODE_IMAGE) \
		sh -c 'npm ci --no-audit --no-fund && npm run build'
endif

# The web app with hot reload on http://localhost:5173, using the API of a
# server on :8080 (run `make demo` in another terminal).
ui-dev: $(UI_DEPS)
	cd web/ui && npm run dev

# A local demo: an admin (demo@admin.com / admin), six months of made-up
# spending, and the offline demo model on the agent page (no API key needed;
# pick a real provider in Settings for a real agent). The data lives in
# /tmp/paylist-demo and is created on the first run only: `make demo-reset`
# starts over. For your own machine only: the password is short.
demo: ui
	PAYLIST_DEV_FAKE=1 \
	PAYLIST_DEMO_SEED=1 \
	PAYLIST_ADMIN_EMAIL='$(DEMO_EMAIL)' \
	PAYLIST_ADMIN_PASSWORD='$(DEMO_PASSWORD)' \
	$(GO) run ./cmd/paylist -addr $(DEMO_ADDR) -data $(DEMO_DATA)

# Delete the demo data (entries, accounts, keys) so the next `make demo` starts fresh.
demo-reset:
	@test -n "$(DEMO_DATA)" && test "$(DEMO_DATA)" != "/" || { echo "DEMO_DATA is not set"; exit 1; }
	rm -rf -- "$(DEMO_DATA)"

# Go unit and integration tests (the same as CI's "test" job).
test:
	$(GO) vet ./...
	$(GO) test ./...
