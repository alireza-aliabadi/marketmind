"""Opt-in PostgreSQL grant tests use a private project, no AIStor license or live data."""

import json
import os
import re
import shutil
import subprocess
import uuid
from collections.abc import Callable, Generator
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]
MANIFEST = json.loads((ROOT / "contracts/service-ownership.json").read_text())
OWNERS = [service["postgresql"] for service in MANIFEST["services"] if service["postgresql"]]
pytestmark = pytest.mark.skipif(
    os.environ.get("RUN_DB_OWNERSHIP_TESTS") != "1",
    reason="opt-in Docker permission test: make ownership-test",
)
SQLRunner = Callable[[str], subprocess.CompletedProcess[str]]


def identifier(value: str) -> str:
    assert re.fullmatch(r"[a-z][a-z0-9_]*", value), "Unsafe ownership identifier"
    return f'"{value}"'


@pytest.fixture(scope="module")
def sql() -> Generator[SQLRunner, None, None]:
    docker = shutil.which("docker")
    if docker is None:
        pytest.fail("Docker is required for ownership-test")
    name = f"marketmind-m14-db-test-{uuid.uuid4().hex[:16]}"
    env = {
        **os.environ,
        "POSTGRES_DB": "marketmind",
        "POSTGRES_USER": "marketmind",
        "POSTGRES_PASSWORD": "m14-permission-test-only",
        "NEO4J_PASSWORD": "m14-permission-test-only",
        "MINIO_ACCESS_KEY": "m14-permission-test",
        "MINIO_SECRET_KEY": "m14-permission-test-only",
    }
    compose = [
        docker,
        "compose",
        "--project-directory",
        str(ROOT),
        "--file",
        str(ROOT / "compose.yml"),
        "--profile",
        "infra",
        "--project-name",
        name,
        "--env-file",
        str(ROOT / ".env.example"),
    ]

    def run(
        arguments: list[str], *, input: str | None = None, timeout: int = 60
    ) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            arguments,
            cwd=ROOT,
            env=env,
            input=input,
            capture_output=True,
            text=True,
            timeout=timeout,
            check=False,
        )

    # The explicit unique project has no developer volumes or published host ports.
    for command in (
        ["ps", "--all", "--quiet"],
        ["volume", "ls", "--quiet"],
        ["network", "ls", "--quiet"],
    ):
        result = run([docker, *command, "--filter", f"label=com.docker.compose.project={name}"])
        assert result.returncode == 0 and not result.stdout.strip(), "Test project already exists"
    try:
        start = run(
            [
                *compose,
                "up",
                "--detach",
                "--quiet-pull",
                "--wait",
                "--wait-timeout",
                "240",
                "postgres",
            ],
            timeout=300,
        )
        assert start.returncode == 0, start.stderr

        def query(statement: str) -> subprocess.CompletedProcess[str]:
            return run(
                [
                    *compose,
                    "exec",
                    "-T",
                    "postgres",
                    "sh",
                    "-ec",
                    'export PGPASSWORD="$POSTGRES_PASSWORD"; exec psql -h 127.0.0.1 '
                    '-U "$POSTGRES_USER" -d "$POSTGRES_DB" -X -v ON_ERROR_STOP=1 '
                    "-v VERBOSITY=verbose -qAt",
                ],
                input=statement,
            )

        yield query
    finally:
        cleanup = run([*compose, "down", "--volumes"], timeout=90)
        assert cleanup.returncode == 0, cleanup.stderr


@pytest.mark.parametrize("owner", OWNERS, ids=lambda owner: owner["schema"])
def test_owner_can_create_and_query_its_schema(sql: SQLRunner, owner: dict[str, str]) -> None:
    schema, role = identifier(owner["schema"]), identifier(owner["owner_role"])
    result = sql(
        f"BEGIN; SET LOCAL ROLE {role}; "
        f"CREATE TABLE {schema}.m14_access_probe (marker TEXT); "
        f"INSERT INTO {schema}.m14_access_probe VALUES ('owned'); "
        f"SELECT marker FROM {schema}.m14_access_probe; ROLLBACK;"
    )
    assert result.returncode == 0, result.stderr
    assert result.stdout.strip() == "owned"


@pytest.mark.parametrize(
    ("owner", "foreign"),
    [(owner, foreign) for owner in OWNERS for foreign in OWNERS if owner != foreign],
    ids=[
        f"{owner['schema']}-blocks-{foreign['schema']}"
        for owner in OWNERS
        for foreign in OWNERS
        if owner != foreign
    ],
)
def test_cross_service_table_access_is_denied(
    sql: SQLRunner, owner: dict[str, str], foreign: dict[str, str]
) -> None:
    schema, role = identifier(owner["schema"]), identifier(owner["owner_role"])
    result = sql(
        f"BEGIN; SET LOCAL ROLE {role}; "
        f"CREATE TABLE {schema}.m14_access_probe (marker TEXT); RESET ROLE; "
        f"SET LOCAL ROLE {identifier(foreign['owner_role'])}; "
        f"SELECT * FROM {schema}.m14_access_probe; ROLLBACK;"
    )
    assert result.returncode != 0
    assert "42501" in result.stderr and "permission denied" in result.stderr, result.stderr


def test_owner_roles_have_no_login_or_elevated_privileges(sql: SQLRunner) -> None:
    for owner in OWNERS:
        role = owner["owner_role"]
        identifier(role)
        result = sql(
            "SELECT rolcanlogin OR rolsuper OR rolcreatedb OR rolcreaterole "
            "OR rolreplication OR rolbypassrls "
            f"FROM pg_roles WHERE rolname = '{role}';"
        )
        assert result.returncode == 0 and result.stdout.strip() == "f", result.stderr


def test_schema_usage_does_not_leak_to_other_owners(sql: SQLRunner) -> None:
    for owner in OWNERS:
        for target in OWNERS:
            role, schema = owner["owner_role"], target["schema"]
            identifier(role)
            identifier(schema)
            result = sql(f"SELECT has_schema_privilege('{role}', '{schema}', 'USAGE');")
            expected = "t" if owner == target else "f"
            assert result.returncode == 0 and result.stdout.strip() == expected, result.stderr
