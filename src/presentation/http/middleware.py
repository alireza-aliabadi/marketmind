"""FastAPI middleware for request tracing and correlation."""

import uuid
from collections.abc import Awaitable, Callable

import structlog
from fastapi import Request, Response

REQUEST_ID_HEADER = "X-Request-ID"
CORRELATION_ID_HEADER = "X-Correlation-ID"


async def request_tracing_middleware(
    request: Request,
    call_next: Callable[[Request], Awaitable[Response]],
) -> Response:
    """Inject and propagate request and correlation IDs."""
    request_id = request.headers.get(REQUEST_ID_HEADER, str(uuid.uuid4()))
    correlation_id = request.headers.get(CORRELATION_ID_HEADER, request_id)

    # Bind to structlog contextvars for consistent logging across current async task
    structlog.contextvars.clear_contextvars()
    structlog.contextvars.bind_contextvars(
        request_id=request_id,
        correlation_id=correlation_id,
        path=request.url.path,
        method=request.method,
    )

    response = await call_next(request)
    response.headers[REQUEST_ID_HEADER] = request_id
    response.headers[CORRELATION_ID_HEADER] = correlation_id
    return response
