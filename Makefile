# Pyloggle developer entry points. Run `make help` for a summary.
SHELL := /bin/bash
GO ?= go
NPM ?= npm
PY ?= python3
VENV ?= python-sdk/.venv

.PHONY: help build web go-build test go-test py-test web-test golden licenses screenshots docs-screenshots demo-data footprint dev-db dev-api dev-web docs-dev compose-up compose-down clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

build: web go-build ## Build the web UI and the ingestd binary (UI embedded)

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "")
LDFLAGS := -X github.com/koneb71/pyloggle/internal/version.Version=$(VERSION) -X github.com/koneb71/pyloggle/internal/version.Commit=$(COMMIT)

go-build: ## Build the ingestd binary only (uses whatever is in web/dist)
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/ingestd ./cmd/ingestd

web: ## Build the React UI into web/dist (embedded by go-build)
	cd web && $(NPM) ci && $(NPM) run build

test: go-test py-test web-test ## Run every test suite

go-test: ## Go unit tests (race detector on)
	$(GO) vet ./...
	$(GO) test -race ./...

py-test: ## Python SDK tests (creates python-sdk/.venv on first run)
	@test -x $(VENV)/bin/python || ($(PY) -m venv $(VENV) && $(VENV)/bin/pip install -q -e 'python-sdk[test]')
	cd python-sdk && .venv/bin/python -m pytest -q

web-test: ## Web unit tests + typecheck
	cd web && $(NPM) run typecheck && $(NPM) test -- --run

golden: ## Regenerate the SDK v0.2 golden NDJSON consumed by the Go decoder tests
	cd python-sdk && .venv/bin/python tests/gen_golden.py

licenses: ## Regenerate THIRD_PARTY_LICENSES.md and web/public/licenses.txt (needs web/node_modules)
	$(PY) scripts/third_party_licenses.py

screenshots: ## Screenshot every UI page (light + dark) with Playwright; needs a running stack
	cd web && $(NPM) run screenshots

DOC_SHOTS ?= overview,issues,issue,issue-steps,logs,logs-details,logs-similar,logs-charts,requests,request,dashboards,alerts,alert-form,projects,project-settings,system,help
PYLOGGLE_URL ?= http://127.0.0.1:8080

demo-data: ## Fill a running server (PYLOGGLE_URL, sign-in off) with two days of demo data
	$(PY) scripts/demo_data.py --url $(PYLOGGLE_URL)

docs-screenshots: ## Capture the screenshots in docs/images/screenshots from a server with demo data
	cd web && PYLOGGLE_URL=$(PYLOGGLE_URL) OUT=../docs/images/screenshots ISSUE_Q=OperationalError TRACE_Q=status:error SHOTS=$(DOC_SHOTS) $(NPM) run screenshots

footprint: ## Measure RSS/disk of the running compose stack under a synthetic burst
	./scripts/footprint.sh

dev-db: ## Start only ClickHouse, published on 127.0.0.1:9000, for dev-api and the integration tests
	cd deploy && docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d clickhouse

dev-api: ## Run ingestd locally against the compose ClickHouse (sign-in off; start it with make dev-db)
	PYLOGGLE_UI_AUTH=disabled CLICKHOUSE_DSN="clickhouse://pyloggle:pyloggle@localhost:9000/pyloggle" $(GO) run ./cmd/ingestd

dev-web: ## Run the Vite dev server (proxies /ui/api and /v1 to :8080)
	cd web && $(NPM) run dev

docs-dev: ## Run the documentation site locally (http://localhost:5173/pyloggle/)
	cd docs && $(NPM) ci && $(NPM) run dev

compose-up: ## Start ClickHouse + ingestd via docker compose
	cd deploy && docker compose up --build -d

compose-down: ## Stop the compose stack
	cd deploy && docker compose down

clean: ## Remove build artifacts
	rm -rf bin dist web/dist/assets web/dist/index.html web/dist/licenses.txt
