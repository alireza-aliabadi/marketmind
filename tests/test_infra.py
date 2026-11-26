"""Regression tests for infrastructure readiness without a Docker daemon."""

import json
import os
import re
import shlex
import shutil
import subprocess
import sys
from collections.abc import Callable
from pathlib import Path
from typing import Any

import pytest

REPO_ROOT = Path(__file__).resolve().parents[1]
VERIFY_INFRA = REPO_ROOT / "infra" / "__main__.py"
SERVICES = ("postgres", "redis", "kafka", "neo4j", "aistor")
COMPOSE_PREFIX = (
    "compose",
    "--project-directory",
    str(REPO_ROOT),
    "--file",
    str(REPO_ROOT / "compose.yml"),
    "--profile",
    "infra",
)
InfraRunner = Callable[..., tuple[subprocess.CompletedProcess[str], list[dict[str, Any]]]]

FAKE_DOCKER = r"""
import json
import os
import sys

services = ("postgres", "redis", "kafka", "neo4j", "aistor")
args = sys.argv[1:]
prefix = json.loads(os.environ["FAKE_DOCKER_COMPOSE_PREFIX"])
scenario = json.loads(os.environ["FAKE_DOCKER_SCENARIO"])
command = None
service = None
output = ""
exit_code = 0
error = None

if args[:len(prefix)] == prefix:
    remaining = args[len(prefix):]
    if (
        len(remaining) == 4
        and remaining[:3] == ["ps", "--all", "--quiet"]
        and remaining[3] in services
    ):
        command = "ps"
        service = remaining[3]
        output = scenario.get("containers", {}).get(service, "fake-" + service)
    elif (
        len(remaining) >= 4
        and remaining[:2] == ["exec", "-T"]
        and remaining[2] in services
    ):
        command = "exec"
        service = remaining[2]
        defaults = {"postgres": "2.31.0", "redis": "PONG", "neo4j": "1"}
        output = scenario.get("probe_outputs", {}).get(service, defaults.get(service, ""))
        exit_code = scenario.get("probe_exit_codes", {}).get(service, 0)
    else:
        error = "Unexpected Compose command"
elif len(args) == 4 and args[:2] == ["inspect", "--format"]:
    service = args[3].removeprefix("fake-")
    if args[3] != "fake-" + service or service not in services:
        error = "Unexpected container ID"
    else:
        command = "inspect"
        output = scenario.get("states", {}).get(service, "running:healthy")
        exit_code = scenario.get("inspect_exit_codes", {}).get(service, 0)
else:
    error = "Unexpected Docker command or Compose options"

with open(os.environ["FAKE_DOCKER_LOG"], "a", encoding="utf-8") as log:
    log.write(json.dumps({
        "argv": args,
        "stdin": sys.stdin.read(),
        "command": command,
        "service": service,
        "error": error,
    }) + "\n")

if error:
    print(error, file=sys.stderr)
    sys.exit(99)
if output:
    print(output)
sys.exit(exit_code)
"""


@pytest.fixture
def infra_runner(tmp_path: Path) -> InfraRunner:
    """Run the native readiness entry point with every Docker call replaced and logged."""
    bin_dir = tmp_path / "bin"
    bin_dir.mkdir()
    docker = bin_dir / "docker"
    docker.write_text(f"#!{sys.executable}\n{FAKE_DOCKER}", encoding="utf-8")
    docker.chmod(0o755)
    log_path = tmp_path / "docker-calls.jsonl"

    def invoke(
        scenario: dict[str, Any] | None = None,
        *,
        options: tuple[str, ...] = (),
        cwd: Path = REPO_ROOT,
    ) -> tuple[subprocess.CompletedProcess[str], list[dict[str, Any]]]:
        environment = {
            "PATH": f"{bin_dir}{os.pathsep}{os.defpath}",
            "HOME": str(tmp_path),
            "TMPDIR": str(tmp_path),
            "LC_ALL": "C",
            "FAKE_DOCKER_LOG": str(log_path),
            "FAKE_DOCKER_COMPOSE_PREFIX": json.dumps([*COMPOSE_PREFIX, *options]),
            "FAKE_DOCKER_SCENARIO": json.dumps(scenario or {}),
        }
        result = subprocess.run(
            [sys.executable, str(VERIFY_INFRA), "check", *options],
            cwd=cwd,
            env=environment,
            input="",
            capture_output=True,
            text=True,
            timeout=15,
            check=False,
        )
        calls = (
            [json.loads(line) for line in log_path.read_text(encoding="utf-8").splitlines()]
            if log_path.exists()
            else []
        )
        assert calls, result.stdout + result.stderr
        # A malformed or mutating command must not make a negative test pass by accident.
        assert all(call["error"] is None for call in calls), calls
        return result, calls

    return invoke


def shell_words(command: str) -> list[str]:
    """Split shell probe commands while keeping control operators separate."""
    lexer = shlex.shlex(command, posix=True, punctuation_chars=True)
    lexer.whitespace_split = True
    return list(lexer)


def test_verify_infra_healthy_services(infra_runner: InfraRunner) -> None:
    """Verify all readiness checks precede the five authenticated functional probes."""
    result, calls = infra_runner()

    assert result.returncode == 0, result.stdout + result.stderr
    assert len(calls) == 3 * len(SERVICES)
    for command in ("ps", "inspect", "exec"):
        matching = [call for call in calls if call["command"] == command]
        assert len(matching) == len(SERVICES)
        assert {call["service"] for call in matching} == set(SERVICES)

    positions = {(call["command"], call["service"]): index for index, call in enumerate(calls)}
    for service in SERVICES:
        assert positions["ps", service] < positions["inspect", service]
    assert max(positions["inspect", service] for service in SERVICES) < min(
        positions["exec", service] for service in SERVICES
    )
    for call in calls:
        if call["command"] == "inspect":
            template = call["argv"][2]
            assert "{{" in template and "}}" in template
            assert ".State.Status" in template
            assert ".State.Health" in template

    probes = {call["service"]: call for call in calls if call["command"] == "exec"}
    postgres_args = probes["postgres"]["argv"][len(COMPOSE_PREFIX) + 3 :]
    assert postgres_args[:2] == ["sh", "-ec"]
    assert len(postgres_args) == 3
    postgres_shell = postgres_args[2]
    assert "export PGPASSWORD=" in postgres_shell
    assert "exec psql" in postgres_shell
    postgres_words = shell_words(postgres_shell)
    assert "-X" in postgres_words
    assert "-At" in postgres_words
    assert postgres_words[postgres_words.index("-v") + 1] == "ON_ERROR_STOP=1"
    sql = probes["postgres"]["stdin"].lower()
    assert "select" in sql
    assert "extversion" in sql
    assert "timescaledb" in sql

    assert probes["redis"]["argv"][len(COMPOSE_PREFIX) + 3 :] == ["redis-cli", "ping"]
    assert probes["kafka"]["argv"][len(COMPOSE_PREFIX) + 3 :] == [
        "/opt/kafka/bin/kafka-topics.sh",
        "--bootstrap-server",
        "kafka:29092",
        "--list",
    ]

    neo4j_args = probes["neo4j"]["argv"][len(COMPOSE_PREFIX) + 3 :]
    assert neo4j_args[0] == "sh"
    neo4j_words = shell_words(neo4j_args[2])
    assert "cypher-shell" in neo4j_words
    assert {"-u", "--username"}.intersection(neo4j_words)
    assert {"-p", "--password"}.intersection(neo4j_words)

    aistor_args = probes["aistor"]["argv"][len(COMPOSE_PREFIX) + 3 :]
    assert aistor_args[0] == "sh"
    aistor_shell = aistor_args[2]
    aistor_words = shell_words(aistor_shell)
    assert "mc" in aistor_words
    assert aistor_args[-3:] == ["sh", "ls", "verification"]
    assert '"$@"' in aistor_shell
    assert ("alias" in aistor_words and "set" in aistor_words) or "MC_HOST_" in aistor_shell
    assert re.search(r"\bmktemp\b[^;\n)]*\s(?:-d|--directory)\b", aistor_shell)
    assert "--config-dir" in aistor_words or "MC_CONFIG_DIR" in aistor_shell
    assert "trap" in aistor_words
    assert "rm " in aistor_shell


@pytest.mark.parametrize("foreign_cwd", [False, True], ids=["repo-cwd", "foreign-cwd"])
def test_verify_infra_forwards_compose_options(
    infra_runner: InfraRunner, tmp_path: Path, foreign_cwd: bool
) -> None:
    """Preserve global options and absolute Compose paths even outside the repository."""
    options = (
        "--project-name",
        "readiness-regression",
        "--env-file",
        str(tmp_path / "compose variables.fixture"),
        "--ansi",
        "never",
    )
    cwd = tmp_path / "unrelated working directory" if foreign_cwd else REPO_ROOT
    if foreign_cwd:
        cwd.mkdir()

    result, calls = infra_runner(options=options, cwd=cwd)

    assert result.returncode == 0, result.stdout + result.stderr
    compose_calls = [call for call in calls if call["argv"][0] == "compose"]
    assert len(compose_calls) == 2 * len(SERVICES)
    for call in compose_calls:
        assert call["argv"][: len(COMPOSE_PREFIX) + len(options)] == [
            *COMPOSE_PREFIX,
            *options,
        ]


@pytest.mark.parametrize("service", SERVICES)
def test_verify_infra_rejects_missing_container(infra_runner: InfraRunner, service: str) -> None:
    """A missing container must fail readiness before any functional command runs."""
    result, calls = infra_runner({"containers": {service: ""}})

    assert result.returncode != 0, result.stdout + result.stderr
    assert any(call["command"] == "ps" and call["service"] == service for call in calls)
    assert not any(call["command"] == "inspect" and call["service"] == service for call in calls)
    assert not any(call["command"] == "exec" for call in calls)


@pytest.mark.parametrize("service", SERVICES)
@pytest.mark.parametrize(
    "state",
    ["running:unhealthy", "exited:healthy", "running:missing", "running:", "running:starting", ""],
    ids=["unhealthy", "stopped", "missing-healthcheck", "empty-health", "starting", "empty-state"],
)
def test_verify_infra_rejects_unready_container(
    infra_runner: InfraRunner, service: str, state: str
) -> None:
    """Only running:healthy is ready, and every service must pass before probes."""
    result, calls = infra_runner({"states": {service: state}})

    assert result.returncode != 0, result.stdout + result.stderr
    assert any(call["command"] == "inspect" and call["service"] == service for call in calls)
    assert not any(call["command"] == "exec" for call in calls)


def test_verify_infra_rejects_missing_timescaledb(infra_runner: InfraRunner) -> None:
    """A successful SQL command with no TimescaleDB extension row is not ready."""
    result, calls = infra_runner({"probe_outputs": {"postgres": ""}})

    assert result.returncode != 0, result.stdout + result.stderr
    assert any(call["command"] == "exec" and call["service"] == "postgres" for call in calls)


@pytest.mark.parametrize("response", ["", "OK", "pong"], ids=["empty", "incorrect", "lowercase"])
def test_verify_infra_requires_redis_pong(infra_runner: InfraRunner, response: str) -> None:
    """A zero redis-cli exit status alone does not establish a successful ping."""
    result, calls = infra_runner({"probe_outputs": {"redis": response}})

    assert result.returncode != 0, result.stdout + result.stderr
    assert any(call["command"] == "exec" and call["service"] == "redis" for call in calls)


@pytest.mark.parametrize("service", SERVICES)
def test_verify_infra_propagates_functional_failure(
    infra_runner: InfraRunner, service: str
) -> None:
    """Postgres, Redis, Kafka, Neo4j and S3 failures cannot be masked by healthy state."""
    result, calls = infra_runner({"probe_exit_codes": {service: 23}})

    assert result.returncode != 0, result.stdout + result.stderr
    assert any(call["command"] == "exec" and call["service"] == service for call in calls)


@pytest.mark.parametrize("service", SERVICES)
def test_verify_infra_propagates_inspect_failure(infra_runner: InfraRunner, service: str) -> None:
    """An inspect failure must not pass even if it also prints a healthy-looking state."""
    result, calls = infra_runner({"inspect_exit_codes": {service: 24}})

    assert result.returncode != 0, result.stdout + result.stderr
    assert any(call["command"] == "inspect" and call["service"] == service for call in calls)
    assert not any(call["command"] == "exec" for call in calls)


@pytest.mark.parametrize(
    ("files", "published"),
    [
        (("compose.yml",), False),
        (("compose.yml", "infra/compose.test.yml"), False),
        (("compose.yml", "compose.override.yml"), True),
        ((), True),
    ],
    ids=["shared-base", "isolated-tests", "explicit-development", "automatic-development"],
)
def test_compose_port_isolation(tmp_path: Path, files: tuple[str, ...], published: bool) -> None:
    """Render real Compose merges without contacting Docker or exposing test ports."""
    docker = shutil.which("docker")
    if docker is None:
        pytest.skip("Compose CLI is unavailable; no Docker daemon is needed")
    version = subprocess.run(
        [docker, "compose", "version"], capture_output=True, text=True, timeout=15, check=False
    )
    if version.returncode != 0:
        pytest.skip("Compose CLI is unavailable; no Docker daemon is needed")

    command = [
        docker,
        "compose",
        "--project-directory",
        str(REPO_ROOT),
        "--project-name",
        "m13-config-regression",
        "--env-file",
        str(REPO_ROOT / ".env.example"),
        "--profile",
        "infra",
        "--profile",
        "tools",
    ]
    for file in files:
        command.extend(["--file", str(REPO_ROOT / file)])
    result = subprocess.run(
        [*command, "config", "--format", "json"],
        cwd=tmp_path,
        env={
            **os.environ,
            "POSTGRES_DB": "marketmind",
            "POSTGRES_USER": "marketmind",
            "POSTGRES_PASSWORD": "compose-config-test-only",
            "NEO4J_PASSWORD": "compose-config-test-only",
            "MINIO_ACCESS_KEY": "compose-config-test",
            "MINIO_SECRET_KEY": "compose-config-test-only",
        },
        capture_output=True,
        text=True,
        timeout=15,
        check=False,
    )
    assert result.returncode == 0, result.stderr
    services = json.loads(result.stdout)["services"]
    ports = {
        name: {
            (port["host_ip"], port["target"], port["published"])
            for port in service.get("ports", [])
        }
        for name, service in services.items()
    }
    expected = {
        "postgres": {("127.0.0.1", 5432, "5432")},
        "redis": {("127.0.0.1", 6379, "6379")},
        "kafka": {("127.0.0.1", 9092, "9092")},
        "kafka-ui": {("127.0.0.1", 8080, "8081")},
        "neo4j": {("127.0.0.1", 7474, "7474"), ("127.0.0.1", 7687, "7687")},
        "aistor": {("127.0.0.1", 9000, "9000"), ("127.0.0.1", 9001, "9001")},
    }
    assert ports == (expected if published else {name: set() for name in expected})
