"""Test fixtures and test client configuration."""

from collections.abc import Generator

import pytest
from fastapi.testclient import TestClient

from src.presentation.http.main import create_app
from src.shared.config import Settings


@pytest.fixture
def test_settings() -> Settings:
    """Return test settings configuration."""
    return Settings(
        app_name="marketmind-test",
        app_env="test",
        log_level="DEBUG",
        api_v1_prefix="/api/v1",
    )


@pytest.fixture
def client(test_settings: Settings) -> Generator[TestClient, None, None]:
    """Return FastAPI TestClient."""
    app = create_app(settings=test_settings)
    with TestClient(app) as test_client:
        yield test_client
