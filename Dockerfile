FROM ghcr.io/astral-sh/uv:0.12.19 AS uv
FROM python:3.14-slim AS runtime
COPY --from=uv /uv /usr/local/bin/uv
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1 \
    UV_LINK_MODE=copy UV_COMPILE_BYTECODE=1 PATH="/app/.venv/bin:$PATH"
WORKDIR /app
RUN addgroup --system app && adduser  --system --ingroup app app
COPY pyproject.toml uv.lock ./
RUN --mount=type=cache,target=/root/.cache/uv uv sync --frozen --no-dev --no-install-project
COPY src ./src
COPY services/knowledge-graph-service/knowledge_graph_service ./services/knowledge-graph-service/knowledge_graph_service
RUN --mount=type=cache,target=/root/.cache/uv uv sync --frozen --no-dev --no-editable
USER app
EXPOSE 8000
CMD ["marketmind-api", "--host", "0.0.0.0", "--port", "8000"]
