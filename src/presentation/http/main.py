"""Main FastAPI presentation application entrypoint."""

from collections.abc import AsyncGenerator
from contextlib import asynccontextmanager

from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from src.presentation.http.health import router as health_router
from src.presentation.http.middleware import request_tracing_middleware
from src.shared.config import Settings, get_settings
from src.shared.logging import get_logger, setup_logging

logger = get_logger("marketmind.api")


@asynccontextmanager
async def lifespan(app: FastAPI) -> AsyncGenerator[None, None]:
    """Application lifespan manager for startup and shutdown hooks."""
    settings: Settings = app.state.settings
    setup_logging(log_level=settings.log_level, app_env=settings.app_env)
    logger.info(
        "Application starting up",
        app_name=settings.app_name,
        env=settings.app_env,
        version=settings.app_version,
    )
    try:
        yield
    finally:
        logger.info("Application shutting down", app_name=settings.app_name)


def create_app(settings: Settings | None = None) -> FastAPI:
    """Build and configure the FastAPI application."""
    current_settings = settings or get_settings()

    app = FastAPI(
        title="MarketMind AI API",
        description="Clean Architecture API platform for MarketMind research engine.",
        version=current_settings.app_version,
        lifespan=lifespan,
    )

    app.state.settings = current_settings

    # Tracing & correlation middleware
    app.middleware("http")(request_tracing_middleware)

    # CORS configuration
    app.add_middleware(
        CORSMiddleware,
        allow_origins=["*"],
        allow_credentials=True,
        allow_methods=["*"],
        allow_headers=["*"],
    )

    # Register routers
    app.include_router(health_router)

    return app


app = create_app()
