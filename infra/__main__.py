"""Native infrastructure checks: python -m infra check or python -m infra test."""

import argparse
import os
import subprocess
import sys
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SERVICES = ("postgres", "redis", "kafka", "neo4j", "aistor")
POSTGRES_SHELL = (
    'export PGPASSWORD="$POSTGRES_PASSWORD"; exec psql -h 127.0.0.1 '
    '-U "$POSTGRES_USER" -d "$POSTGRES_DB" -X -v ON_ERROR_STOP=1 -At'
)
S3_SHELL = """
config_dir="$(mktemp -d)"
trap 'rm -rf "$config_dir"' EXIT
mc --config-dir "$config_dir" alias set verification http://localhost:9000 \
    "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null
mc --config-dir "$config_dir" "$@"
"""


def run(arguments: list[str], *, environment: dict[str, str] | None = None, input: str = "") -> str:
    try:
        result = subprocess.run(
            arguments,
            env=environment,
            input=input,
            capture_output=True,
            text=True,
            timeout=300,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise RuntimeError("Docker operation unavailable or timed out") from exc
    if result.returncode != 0:
        # Docker/driver output can contain credentials. Do not echo it or the command.
        raise RuntimeError("Docker operation failed")
    return result.stdout.strip()


class Compose:
    def __init__(
        self,
        root: Path,
        options: tuple[str, ...] = (),
        environment: dict[str, str] | None = None,
    ) -> None:
        self.command = [
            "docker",
            "compose",
            "--project-directory",
            str(root),
            "--file",
            str(root / "compose.yml"),
            "--profile",
            "infra",
            *options,
        ]
        self.environment = environment

    def run(self, *arguments: str, input: str = "") -> str:
        return run([*self.command, *arguments], environment=self.environment, input=input)

    def sql(self, statement: str) -> str:
        return self.run("exec", "-T", "postgres", "sh", "-ec", POSTGRES_SHELL, input=statement)

    def graph(self, query: str) -> str:
        return self.run(
            "exec",
            "-T",
            "neo4j",
            "sh",
            "-ec",
            "exec cypher-shell --address bolt://localhost:7687 --username neo4j "
            '--password "${NEO4J_AUTH#*/}" --format plain "$1"',
            "sh",
            query,
        )

    def s3(self, *arguments: str, input: str = "") -> str:
        return self.run(
            "exec", "-T", "aistor", "sh", "-ec", S3_SHELL, "sh", *arguments, input=input
        )


def check(compose: Compose) -> None:
    for service in SERVICES:
        container = compose.run("ps", "--all", "--quiet", service)
        if not container:
            raise RuntimeError(f"{service} has no container")
        state = run(
            [
                "docker",
                "inspect",
                "--format",
                "{{.State.Status}}:{{if .State.Health}}{{.State.Health.Status}}"
                "{{else}}missing{{end}}",
                container,
            ],
            environment=compose.environment,
        )
        if state != "running:healthy":
            raise RuntimeError(f"{service} is not running and healthy")

    version = compose.sql("SELECT extversion FROM pg_extension WHERE extname = 'timescaledb';")
    if not version:
        raise RuntimeError("TimescaleDB extension is missing")
    print(f"PASS: PostgreSQL; TimescaleDB {version}")
    if compose.run("exec", "-T", "redis", "redis-cli", "ping") != "PONG":
        raise RuntimeError("Redis did not return PONG")
    print("PASS: Redis")
    compose.run(
        "exec",
        "-T",
        "kafka",
        "/opt/kafka/bin/kafka-topics.sh",
        "--bootstrap-server",
        "kafka:29092",
        "--list",
    )
    print("PASS: Kafka")
    compose.graph("RETURN 1")
    print("PASS: Neo4j")
    compose.s3("ls", "verification")
    print("PASS: S3-compatible object storage")
    print("Infrastructure readiness checks passed.")


def assert_marker(service: str, marker: str) -> None:
    if marker != "m13-persisted":
        raise RuntimeError(f"{service} did not preserve its fixture")
    print(f"PASS: {service} persistence after container recreation")


def verify_persistence(root: Path) -> None:
    license_file = root / "minio.license"
    if not license_file.is_file() or not license_file.stat().st_size:
        raise RuntimeError("A valid local minio.license is required for AIStor")
    name = f"marketmind-m13-test-{time.time_ns()}-{os.getpid()}"
    environment = {
        **os.environ,
        "POSTGRES_DB": "marketmind",
        "POSTGRES_USER": "marketmind",
        "POSTGRES_PASSWORD": "m13-test-postgres-only",
        "NEO4J_PASSWORD": "m13-test-neo4j-only",
        "MINIO_ACCESS_KEY": "marketmind-m13-test",
        "MINIO_SECRET_KEY": "m13-test-minio-only",
    }
    compose = Compose(
        root,
        (
            "--project-name",
            name,
            "--env-file",
            str(root / ".env.example"),
            "--file",
            str(root / "infra/compose.test.yml"),
        ),
        environment,
    )
    compose.run("config", "--quiet")
    # Check ownership before entering cleanup: never remove a preexisting project.
    for prefix in (
        ["ps", "--all", "--quiet"],
        ["volume", "ls", "--quiet"],
        ["network", "ls", "--quiet"],
    ):
        if run(
            ["docker", *prefix, "--filter", f"label=com.docker.compose.project={name}"],
            environment=environment,
        ):
            raise RuntimeError(f"Refusing to reuse existing test project {name}")
    try:
        print(f"Verifying clean infrastructure in {name}.")
        compose.run("up", "--detach", "--quiet-pull", "--wait", "--wait-timeout", "240")
        # Compose already verifies readiness; these fixtures additionally test authenticated I/O.
        compose.sql(
            "CREATE TABLE m13_storage_probe "
            "(observed_at TIMESTAMPTZ NOT NULL, marker TEXT NOT NULL);"
            "SELECT create_hypertable('m13_storage_probe', by_range('observed_at'));"
            "INSERT INTO m13_storage_probe VALUES ('2026-11-11T00:00:00Z', 'm13-persisted');"
        )
        compose.run("exec", "-T", "redis", "redis-cli", "SET", "m13:storage:probe", "m13-persisted")
        compose.run(
            "exec",
            "-T",
            "kafka",
            "/opt/kafka/bin/kafka-topics.sh",
            "--bootstrap-server",
            "kafka:29092",
            "--create",
            "--topic",
            "m13.storage.probe",
            "--partitions",
            "1",
            "--replication-factor",
            "1",
        )
        compose.run(
            "exec",
            "-T",
            "kafka",
            "/opt/kafka/bin/kafka-console-producer.sh",
            "--bootstrap-server",
            "kafka:29092",
            "--topic",
            "m13.storage.probe",
            "--command-property",
            "acks=all",
            input="m13-persisted\n",
        )
        compose.graph("CREATE (:M13StorageProbe {marker: 'm13-persisted'})")
        compose.s3("mb", "verification/m13-storage-probe")
        compose.s3("pipe", "verification/m13-storage-probe/marker", input="m13-persisted")

        compose.run("down")
        compose.run("up", "--detach", "--quiet-pull", "--wait", "--wait-timeout", "240")
        assert_marker("PostgreSQL", compose.sql("SELECT marker FROM m13_storage_probe;"))
        assert_marker(
            "Redis", compose.run("exec", "-T", "redis", "redis-cli", "GET", "m13:storage:probe")
        )
        assert_marker(
            "Kafka",
            compose.run(
                "exec",
                "-T",
                "kafka",
                "/opt/kafka/bin/kafka-console-consumer.sh",
                "--bootstrap-server",
                "kafka:29092",
                "--topic",
                "m13.storage.probe",
                "--from-beginning",
                "--max-messages",
                "1",
                "--timeout-ms",
                "15000",
            ),
        )
        rows = compose.graph("MATCH (probe:M13StorageProbe) RETURN probe.marker AS marker")
        assert_marker("Neo4j", "m13-persisted" if '"m13-persisted"' in rows.splitlines() else "")
        assert_marker("AIStor", compose.s3("cat", "verification/m13-storage-probe/marker"))
        print("Infrastructure startup and persistence verification passed.")
    finally:
        # Only this invocation's uniquely allocated resources are removed.
        compose.run("down", "--volumes")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("check", "test"))
    args, options = parser.parse_known_args()
    if args.command == "test" and options:
        parser.error("test allocates its own isolated Compose project; extra options are forbidden")
    try:
        if args.command == "check":
            check(Compose(ROOT, tuple(options)))
        else:
            verify_persistence(ROOT)
    except (OSError, RuntimeError) as exc:
        print(f"FAIL: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
