.PHONY: help infra-up infra-down api run test test-python test-go lint format check

help:
	@echo "Available commands:"
	@echo "  make infra-up    - Start local infrastructure containers (PostgreSQL, Kafka, Neo4j, Redis, MinIO)"
	@echo "  make infra-down  - Stop local infrastructure containers"
	@echo "  make api         - Run FastAPI service with hot-reload"
	@echo "  make run         - Alias for make api"
	@echo "  make test        - Run Python and Go test suites"
	@echo "  make lint        - Run ruff linter and mypy type checks"
	@echo "  make format      - Format Python source code with ruff"
	@echo "  make check       - Run format check, lint, and all test suites"

infra-up:
	docker compose up -d

infra-down:
	docker compose down

api:
	uv run uvicorn src.presentation.http.main:app --reload --host 0.0.0.0 --port 8000

run: api

test-python:
	uv run pytest

test-go:
	cd services/api-gateway && go test ./...

test: test-python test-go

lint:
	uv run ruff check .
	uv run mypy src

format:
	uv run ruff format .

check: lint test
