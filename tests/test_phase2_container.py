"""Package and execute an immutable non-root ingestion image with no live access."""

import os
import subprocess
import tempfile
import uuid
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]
SERVICE = ROOT / "services/market-data-service"
pytestmark = pytest.mark.skipif(
    os.environ.get("RUN_MARKET_CONTAINER_TESTS") != "1",
    reason="opt-in container artifact verification: make market-container-test",
)


def test_nonroot_readonly_runtime_image() -> None:
    tag = f"marketmind-m2-runtime-test:{uuid.uuid4().hex[:16]}"
    with tempfile.TemporaryDirectory(
        prefix="marketmind-runtime-image-", dir="/tmp/opencode"
    ) as directory:
        binary = Path(directory) / "market-data"
        build = subprocess.run(
            [
                "go",
                "build",
                "-mod=readonly",
                "-trimpath",
                "-ldflags=-s -w",
                "-o",
                str(binary),
                "./cmd/market-data",
            ],
            cwd=SERVICE,
            env={**os.environ, "CGO_ENABLED": "0", "GOOS": "linux", "GOARCH": "amd64"},
            capture_output=True,
            text=True,
            timeout=120,
            check=False,
        )
        assert build.returncode == 0, build.stderr
        try:
            image = subprocess.run(
                [
                    "docker",
                    "build",
                    "--tag",
                    tag,
                    "--file",
                    str(SERVICE / "Dockerfile.runtime"),
                    directory,
                ],
                capture_output=True,
                text=True,
                timeout=180,
                check=False,
            )
            assert image.returncode == 0, image.stderr
            command = [
                "docker",
                "run",
                "--rm",
                "--read-only",
                "--network",
                "none",
                "--user",
                "65532:65532",
            ]
            help_result = subprocess.run(
                [*command, tag, "sec", "--help"],
                capture_output=True,
                text=True,
                timeout=30,
                check=False,
            )
            assert help_result.returncode == 0, help_result.stderr
            disabled = subprocess.run(
                [
                    *command,
                    "--env",
                    "MARKET_ENV=production",
                    "--env",
                    "MARKET_DATABASE_URL=postgresql://ingestor:synthetic@db.example/db?sslmode=verify-full",
                    "--env",
                    "KAFKA_BOOTSTRAP_SERVERS=broker.example:9093",
                    "--env",
                    "KAFKA_SASL_MECHANISM=scram-sha-512",
                    "--env",
                    "KAFKA_SASL_USERNAME=ingestor",
                    "--env",
                    "KAFKA_SASL_PASSWORD=synthetic-secret",
                    tag,
                    "sec",
                    "--cik",
                    "1045810",
                    "--enqueue",
                ],
                capture_output=True,
                text=True,
                timeout=30,
                check=False,
            )
            assert disabled.returncode != 0 and "provider_permission_required" in disabled.stderr
            assert "synthetic-secret" not in disabled.stderr
            print(
                "Non-root read-only image rejects unapproved live providers without network access"
            )
        finally:
            removed = subprocess.run(
                ["docker", "image", "rm", tag], capture_output=True, timeout=30, check=False
            )
            assert removed.returncode == 0, "owned test image cleanup failed"
