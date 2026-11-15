INFRA_COMPOSE_ARGS ?=
INFRA_COMPOSE = docker compose --file compose.yml --file compose.override.yml --profile infra $(INFRA_COMPOSE_ARGS)
GO_MODULES = services/api-gateway services/identity-service
GO_TEST_FLAGS ?=
GATEWAY_TEST_TMPDIR ?= /tmp/opencode

.PHONY: help infra-config infra-up infra-down infra-check infra-test infra-tools infra-logs contracts-check architecture-check ownership-test ownership-init api run gateway gateway-test pre-commit-check test test-python test-go build-go lint lint-python lint-go frontend-lint frontend-check format format-go check

help:
	@echo "Available commands:"
	@echo "  make infra-config - Validate Compose configuration and the local AIStor license file"
	@echo "  make infra-up    - Start infrastructure and verify readiness"
	@echo "  make infra-down  - Stop infrastructure without deleting volumes"
	@echo "  make infra-check - Verify infrastructure health and authenticated access"
	@echo "  make infra-test  - Test startup and persistence in an isolated Docker project"
	@echo "  make infra-tools - Start optional Kafka UI at http://localhost:8081"
	@echo "  make infra-logs  - Follow the latest infrastructure logs"
	@echo "  make contracts-check - Validate OpenAPI, event schemas, examples, versions, and ownership"
	@echo "  make architecture-check - Verify static service and dependency boundaries"
	@echo "  make ownership-test - Test PostgreSQL role/schema isolation in a private Docker project"
	@echo "  make ownership-init - Explicitly apply ownership bootstrap to an existing local database"
	@echo "  make api         - Run FastAPI service with hot-reload"
	@echo "  make run         - Alias for make api"
	@echo "  make gateway     - Run the OIDC-protected Go gateway (requires issuer/audience configuration)"
	@echo "  make gateway-test - Test the real Go gateway -> FastAPI flow with a disposable OIDC issuer"
	@echo "  make pre-commit-check - Run configured hooks on tracked files"
	@echo "  make test        - Run Python and Go test suites"
	@echo "  make lint        - Run Python, Go, and frontend lint/format/type checks"
	@echo "  make frontend-check - Lint, format-check, type-check and build the React scaffold"
	@echo "  make format      - Format Python source code with ruff"
	@echo "  make check       - Run lint, format/type/contract/architecture checks, tests and builds"

infra-config:
	$(INFRA_COMPOSE) config --quiet
	@test -f minio.license && test -s minio.license || { printf 'A valid local minio.license is required for AIStor.\n' >&2; exit 1; }

infra-up: infra-config
	$(INFRA_COMPOSE) up --detach --wait --wait-timeout 240
	$(MAKE) infra-check

infra-check: infra-config
	bash scripts/verify-infra.sh $(INFRA_COMPOSE_ARGS)

infra-test:
	bash scripts/test-infra.sh

infra-tools: infra-up
	$(INFRA_COMPOSE) --profile tools up --detach kafka-ui

infra-logs:
	$(INFRA_COMPOSE) --profile tools logs --tail 100 --follow

infra-down:
	$(INFRA_COMPOSE) --profile tools down

contracts-check:
	uv run python -m contracts.validation

architecture-check:
	uv run python scripts/check-architecture.py

ownership-test:
	RUN_DB_OWNERSHIP_TESTS=1 uv run pytest tests/test_db_ownership.py

ownership-init: infra-config
	$(INFRA_COMPOSE) exec -T postgres sh -ec 'export PGPASSWORD="$$POSTGRES_PASSWORD"; exec psql -h 127.0.0.1 -U "$$POSTGRES_USER" -d "$$POSTGRES_DB" -X -v ON_ERROR_STOP=1' < infra/postgres/init/020-service-ownership.sql

api:
	uv run uvicorn src.presentation.http.main:app --reload --host 0.0.0.0 --port 8000

run: api

gateway:
	cd services/api-gateway && go run ./cmd/server

gateway-test:
	@mkdir -p "$(GATEWAY_TEST_TMPDIR)"
	cd services/api-gateway && TMPDIR="$(GATEWAY_TEST_TMPDIR)" RUN_GATEWAY_INTEGRATION_TESTS=1 go test -count=1 -v ./cmd/server -run TestGatewayEndToEnd

pre-commit-check:
	uv tool run pre-commit run --config .pre-commit-config.yml --all-files

test-python:
	uv run pytest

test-go:
	@for service in $(GO_MODULES); do (cd "$$service" && go test $(GO_TEST_FLAGS) ./...) || exit 1; done

build-go:
	@for service in $(GO_MODULES); do (cd "$$service" && go build ./...) || exit 1; done

test: test-python test-go

lint-python:
	uv run ruff check .
	uv run ruff format --check .
	uv run mypy src

lint-go:
	@unformatted=$$(gofmt -l $(GO_MODULES)); if [ -n "$$unformatted" ]; then printf 'Run make format-go on:\n%s\n' "$$unformatted"; exit 1; fi
	@for service in $(GO_MODULES); do (cd "$$service" && go vet ./...) || exit 1; done

frontend-lint:
	pnpm --dir frontend/web run lint
	pnpm --dir frontend/web run format:check

frontend-check: frontend-lint
	pnpm --dir frontend/web run build

lint: lint-python lint-go frontend-lint

format:
	uv run ruff format .

format-go:
	gofmt -w $(GO_MODULES)

check: lint architecture-check contracts-check test build-go frontend-check
