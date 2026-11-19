"""Regression tests for isolated infrastructure persistence and cleanup."""

import json
import os
import re
import shutil
import subprocess
import sys
from collections.abc import Callable
from pathlib import Path
from typing import Any

import pytest

TEST_INFRA = Path(__file__).resolve().parents[1] / "infra" / "__main__.py"
SERVICES = ("postgres", "redis", "kafka", "neo4j", "aistor")
PersistenceRunner = Callable[..., tuple[subprocess.CompletedProcess[str], list[dict[str, Any]]]]

FAKE_DOCKER = r"""
import json
import os
import re
import sys

args = sys.argv[1:]
root = os.environ["FAKE_DOCKER_ROOT"]
scenario = json.loads(os.environ["FAKE_DOCKER_SCENARIO"])
call = {"argv": args, "stdin": sys.stdin.read(), "readback": False}

def finish(output="", status=0, error=None):
    call.update(stdout=output, exit_code=status, error=error)
    with open(os.environ["FAKE_DOCKER_LOG"], "a", encoding="utf-8") as log:
        log.write(json.dumps(call) + "\n")
    if error:
        print(error, file=sys.stderr)
        sys.exit(99)
    if output:
        print(output)
    sys.exit(status)

if args[:1] == ["compose"]:
    project = args[8] if len(args) > 8 and args[7] == "--project-name" else ""
    prefix = [
        "compose", "--project-directory", root, "--file", root + "/compose.yml",
        "--profile", "infra", "--project-name", project,
        "--env-file", root + "/.env.example", "--file", root + "/infra/compose.test.yml",
    ]
    if not re.fullmatch(r"marketmind-m13-test-\d+-\d+", project) or args[:len(prefix)] != prefix:
        finish(error="Compose requires explicit isolated test project and mock project paths")
    remaining = args[len(prefix):]
    if not remaining:
        finish(error="Missing Compose command")
    call.update(project=project, command=remaining[0], compose_args=remaining)
    if remaining[0] in ("config", "ps"):
        finish()
    if remaining[0] == "up":
        finish(status=23 if scenario.get("startup_failure") else 0)
    if remaining in (["down"], ["down", "--volumes"]):
        finish()
    if len(remaining) < 4 or remaining[:2] != ["exec", "-T"]:
        finish(error="Unexpected or destructive Compose command")
    service, probe = remaining[2], remaining[3:]
    call["service"] = service
    if service == "postgres":
        call["readback"] = "select marker from" in call["stdin"].lower()
        if not call["readback"] and "CREATE TABLE" not in call["stdin"]:
            finish(error="Unexpected PostgreSQL SQL input")
    elif service == "redis" and probe[:1] == ["redis-cli"]:
        call["readback"] = "GET" in probe
        if not call["readback"] and "SET" not in probe:
            finish(error="Unexpected Redis probe")
    elif service == "kafka":
        call["readback"] = probe[0].endswith("kafka-console-consumer.sh")
        if not call["readback"] and not probe[0].endswith((
            "kafka-topics.sh", "kafka-console-producer.sh"
        )):
            finish(error="Unexpected Kafka probe")
    elif service == "neo4j" and probe[:1] == ["sh"]:
        call["readback"] = "MATCH" in probe[-1]
        if not call["readback"] and "CREATE" not in probe[-1]:
            finish(error="Unexpected Neo4j probe")
    elif service == "aistor" and probe[:1] == ["sh"]:
        call["readback"] = "cat" in probe
        if not call["readback"] and not {"mb", "pipe"}.intersection(probe):
            finish(error="Unexpected S3 probe")
    else:
        finish(error="Unexpected functional command")
    output = ""
    if call["readback"]:
        output = 'marker\n"m13-persisted"' if service == "neo4j" else "m13-persisted"
    status = 23 if call["readback"] and scenario.get("failed_readback") == service else 0
    finish(output, status)

guards = {
    "container": ["ps", "--all", "--quiet", "--filter"],
    "volume": ["volume", "ls", "--quiet", "--filter"],
    "network": ["network", "ls", "--quiet", "--filter"],
}
for resource, prefix in guards.items():
    if len(args) == len(prefix) + 1 and args[:len(prefix)] == prefix:
        label = "label=com.docker.compose.project="
        project = args[-1].removeprefix(label)
        if (
            not args[-1].startswith(label)
            or not re.fullmatch(r"marketmind-m13-test-\d+-\d+", project)
        ):
            finish(error="Resource guard must target the isolated test project label")
        call.update(project=project, command="guard", resource=resource)
        finish("existing-resource" if scenario.get("existing_resource") == resource else "")
finish(error="Unexpected Docker command")
"""


@pytest.fixture
def persistence_runner(tmp_path: Path) -> PersistenceRunner:
    """Copy only the harness into a mock project with no real secrets or Docker."""
    project = tmp_path / "mock project"
    infra = project / "infra"
    infra.mkdir(parents=True)
    shutil.copyfile(TEST_INFRA, infra / "__main__.py")
    (project / "minio.license").write_text("dummy test license\n", encoding="utf-8")
    bin_dir = tmp_path / "bin"
    bin_dir.mkdir()
    docker = bin_dir / "docker"
    docker.write_text(f"#!{sys.executable}\n{FAKE_DOCKER}", encoding="utf-8")
    docker.chmod(0o755)
    log_path = tmp_path / "docker-calls.jsonl"

    def invoke(
        scenario: dict[str, Any] | None = None,
    ) -> tuple[subprocess.CompletedProcess[str], list[dict[str, Any]]]:
        result = subprocess.run(
            [sys.executable, str(infra / "__main__.py"), "test"],
            cwd=tmp_path,
            env={
                "PATH": f"{bin_dir}{os.pathsep}{os.defpath}",
                "HOME": str(tmp_path),
                "TMPDIR": str(tmp_path),
                "LC_ALL": "C",
                "FAKE_DOCKER_ROOT": str(project),
                "FAKE_DOCKER_LOG": str(log_path),
                "FAKE_DOCKER_SCENARIO": json.dumps(scenario or {}),
            },
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
        assert all(call["error"] is None for call in calls), calls
        projects = {call["project"] for call in calls}
        assert len(projects) == 1
        assert re.fullmatch(r"marketmind-m13-test-\d+-\d+", projects.pop())
        return result, calls

    return invoke


def lifecycle(calls: list[dict[str, Any]]) -> list[list[str]]:
    return [call["compose_args"] for call in calls if call["command"] in {"up", "down"}]


def test_persistence_recreates_and_cleans_only_test_project(
    persistence_runner: PersistenceRunner,
) -> None:
    result, calls = persistence_runner()

    assert result.returncode == 0, result.stdout + result.stderr
    changes = lifecycle(calls)
    assert [command[0] for command in changes] == ["up", "down", "up", "down"]
    assert changes[1] == ["down"]
    assert calls[-1]["compose_args"] == ["down", "--volumes"]
    readbacks = [call for call in calls if call["readback"]]
    assert len(readbacks) == len(SERVICES)
    assert {call["service"] for call in readbacks} == set(SERVICES)


@pytest.mark.parametrize("service", SERVICES)
def test_persistence_rejects_marker_followed_by_probe_failure(
    persistence_runner: PersistenceRunner, service: str
) -> None:
    result, calls = persistence_runner({"failed_readback": service})

    assert result.returncode != 0, result.stdout + result.stderr
    failed = [call for call in calls if call["readback"] and call["service"] == service]
    assert len(failed) == 1
    assert "m13-persisted" in failed[0]["stdout"]
    assert failed[0]["exit_code"] == 23
    changes = lifecycle(calls)
    assert [command[0] for command in changes] == ["up", "down", "up", "down"]
    assert changes[1] == ["down"]
    assert calls[-1]["compose_args"] == ["down", "--volumes"]


@pytest.mark.parametrize("resource", ["container", "volume", "network"])
def test_persistence_refuses_preexisting_labelled_resources(
    persistence_runner: PersistenceRunner, resource: str
) -> None:
    result, calls = persistence_runner({"existing_resource": resource})

    assert result.returncode != 0, result.stdout + result.stderr
    assert "Refusing to reuse" in result.stderr
    assert any(call.get("resource") == resource and call["stdout"] for call in calls)
    assert not lifecycle(calls)
    assert [call["command"] for call in calls if "compose_args" in call] == ["config"]


def test_persistence_cleans_test_project_after_startup_failure(
    persistence_runner: PersistenceRunner,
) -> None:
    result, calls = persistence_runner({"startup_failure": True})

    assert result.returncode != 0, result.stdout + result.stderr
    changes = lifecycle(calls)
    assert [command[0] for command in changes] == ["up", "down"]
    assert calls[-1]["compose_args"] == ["down", "--volumes"]
    assert not any(call["readback"] for call in calls)
