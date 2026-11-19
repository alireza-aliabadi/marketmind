"""Installed native Python entry points work without shell launchers or PYTHONPATH."""

import os
import socket
import subprocess
import sys
import time
from pathlib import Path

import httpx
import pytest


def environment() -> dict[str, str]:
    values = {**os.environ, "APP_ENV": "test"}
    values.pop("PYTHONPATH", None)
    return values


@pytest.mark.parametrize(
    "arguments",
    [
        ["-m", "src.presentation.http", "--help"],
        ["-m", "knowledge_graph_service", "--help"],
        [str(Path(sys.executable).parent / "marketmind-api"), "--help"],
        [str(Path(sys.executable).parent / "graph-setup"), "--help"],
    ],
)
def test_installed_entrypoints_outside_checkout(tmp_path: Path, arguments: list[str]) -> None:
    result = subprocess.run(
        [sys.executable, *arguments],
        cwd=tmp_path,
        env=environment(),
        capture_output=True,
        text=True,
        timeout=15,
        check=False,
    )
    assert result.returncode == 0, result.stdout + result.stderr
    assert "usage:" in result.stdout


def test_graph_setup_does_not_load_dotenv_or_default_to_a_database(tmp_path: Path) -> None:
    (tmp_path / ".env").write_text(
        "GRAPH_SCHEMA_URI=bolt://localhost:7687\nGRAPH_SCHEMA_USERNAME=operator\n"
        "GRAPH_SCHEMA_PASSWORD=synthetic-sensitive-password\n"
    )
    env = {
        **environment(),
        "GRAPH_SCHEMA_URI": "",
        "GRAPH_SCHEMA_USERNAME": "",
        "GRAPH_SCHEMA_PASSWORD": "",
    }
    result = subprocess.run(
        [sys.executable, "-m", "knowledge_graph_service"],
        cwd=tmp_path,
        env=env,
        capture_output=True,
        text=True,
        timeout=15,
        check=False,
    )
    assert result.returncode == 1
    assert "operator settings are required" in result.stderr
    assert "synthetic-sensitive-password" not in result.stderr


def test_api_rejects_invalid_port_before_startup(tmp_path: Path) -> None:
    result = subprocess.run(
        [sys.executable, "-m", "src.presentation.http", "--port", "0"],
        cwd=tmp_path,
        env=environment(),
        capture_output=True,
        text=True,
        timeout=15,
        check=False,
    )
    assert result.returncode == 2
    assert "port must be between 1 and 65535" in result.stderr


def test_native_api_serves_liveness_and_shuts_down(tmp_path: Path) -> None:
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        port = listener.getsockname()[1]
    log = tmp_path / "api.log"
    with log.open("w") as output:
        process = subprocess.Popen(
            [sys.executable, "-m", "src.presentation.http", "--port", str(port)],
            cwd=tmp_path,
            env=environment(),
            stdout=output,
            stderr=subprocess.STDOUT,
        )
        try:
            deadline = time.monotonic() + 10
            with httpx.Client(trust_env=False, timeout=0.5) as client:
                while time.monotonic() < deadline:
                    assert process.poll() is None, log.read_text()
                    try:
                        response = client.get(f"http://127.0.0.1:{port}/health/live")
                    except (httpx.NetworkError, httpx.TimeoutException):
                        time.sleep(0.05)
                        continue
                    assert response.status_code == 200
                    assert response.json() == {"status": "healthy"}
                    break
                else:
                    pytest.fail("Native API did not become live: " + log.read_text())
        finally:
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)
    assert "Application shutting down" in log.read_text()
