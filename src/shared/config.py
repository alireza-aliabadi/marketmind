"""Application configuration loaded from environment variables."""

from functools import lru_cache

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    """Central configuration for MarketMind services."""

    model_config = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        extra="ignore",
    )

    # Application
    app_name: str = "marketmind-api"
    app_env: str = "local"
    app_version: str = "0.1.0"
    log_level: str = "INFO"
    api_v1_prefix: str = "/api/v1"
    host: str = "0.0.0.0"
    port: int = 8000

    # PostgreSQL / TimescaleDB
    postgres_db: str = "marketmind"
    postgres_user: str = "marketmind"
    postgres_password: str = "change-me-local"
    database_url: str = (
        "postgresql+psycopg://marketmind:change-me-local@localhost:5432/marketmind"
    )

    # Redis
    redis_url: str = "redis://localhost:6379/0"

    # Kafka
    kafka_bootstrap_servers: str = "localhost:9092"

    # Neo4j Knowledge Graph
    neo4j_uri: str = "bolt://localhost:7687"
    neo4j_username: str = "neo4j"
    neo4j_password: str = "change-me-local"

    # MinIO / S3 Object Storage
    minio_endpoint: str = "localhost:9000"
    minio_access_key: str = "marketmind"
    minio_secret_key: str = "change-me-local"
    minio_secure: bool = False

    # External Provider Defaults
    sec_edgar_user_agent: str = "MarketMindResearch admin@marketmind.local"
    fred_api_key: str = ""


@lru_cache
def get_settings() -> Settings:
    """Return cached application settings singleton."""
    return Settings()
