"""Architecture checks have negative fixtures, not just a clean-tree smoke test."""

import importlib.util
import json
import sys
from pathlib import Path
from types import ModuleType
from typing import Any

import pytest

ROOT = Path(__file__).resolve().parents[1]


@pytest.fixture(scope="module")
def checker() -> ModuleType:
    spec = importlib.util.spec_from_file_location(
        "marketmind_architecture_checks", ROOT / "scripts/check-architecture.py"
    )
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


@pytest.fixture
def project(tmp_path: Path) -> tuple[Path, dict[str, Any]]:
    services = []
    for name in ("alpha", "beta"):
        (tmp_path / name / "domain").mkdir(parents=True)
        (tmp_path / name / "domain/models.py").write_text("from dataclasses import dataclass\n")
        services.append(
            {
                "name": name,
                "root": name,
                "status": "implemented",
                "language": "python",
                "import_root": name,
                "composition_roots": [],
                "postgresql": {
                    "schema": name,
                    "owner_role": f"mm_{name}",
                    "status": "RESERVED",
                    "role_login": False,
                    "runtime_credentials": "PLANNED",
                },
                "redis": [],
                "s3": [],
                "neo4j": [],
            }
        )
    manifest = {"version": 1, "services": services, "topics": []}
    (tmp_path / "contracts").mkdir()
    (tmp_path / "contracts/service-ownership.json").write_text(json.dumps(manifest))
    return tmp_path, manifest


def test_current_architecture_is_clean(checker: ModuleType) -> None:
    assert checker.check_architecture(ROOT) == []


@pytest.mark.parametrize(
    ("layer", "source", "message"),
    [
        ("domain", "import pydantic\n", "external/framework"),
        (
            "application",
            "from alpha.infrastructure import repository\n",
            "application -> infrastructure",
        ),
        ("application", "from beta.domain import models\n", "cross-service"),
        ("domain", "import os\n", "external/framework"),
        ("application", "from alpha.presentation import handler\n", "application -> presentation"),
        ("domain", "from ..application import service\n", "domain -> application"),
    ],
)
def test_forbidden_dependency_has_location(
    checker: ModuleType,
    project: tuple[Path, dict[str, Any]],
    layer: str,
    source: str,
    message: str,
) -> None:
    root, _ = project
    directory = root / "alpha" / layer
    directory.mkdir(exist_ok=True)
    path = directory / "violation.py"
    path.write_text(source)
    issues = checker.check_architecture(root)
    assert any(
        issue.path == path and issue.line == 1 and message in issue.message for issue in issues
    )


def test_duplicate_database_ownership_is_rejected(
    checker: ModuleType, project: tuple[Path, dict[str, Any]]
) -> None:
    root, manifest = project
    manifest["services"][1]["postgresql"]["schema"] = "alpha"
    (root / "contracts/service-ownership.json").write_text(json.dumps(manifest))
    assert any(
        "duplicate PostgreSQL schema" in issue.message for issue in checker.check_architecture(root)
    )


def test_planned_roots_need_not_exist_in_a_clean_checkout(
    checker: ModuleType, project: tuple[Path, dict[str, Any]]
) -> None:
    root, manifest = project
    manifest["services"][1]["status"] = "planned"
    manifest["services"][1]["root"] = "not-created-yet"
    (root / "contracts/service-ownership.json").write_text(json.dumps(manifest))
    assert checker.check_architecture(root) == []


def test_manifest_path_traversal_is_rejected(
    checker: ModuleType, project: tuple[Path, dict[str, Any]]
) -> None:
    root, manifest = project
    manifest["services"][0]["root"] = "../outside"
    (root / "contracts/service-ownership.json").write_text(json.dumps(manifest))
    assert any("traversal" in issue.message for issue in checker.check_architecture(root))


def test_go_import_scanner_handles_comments_aliases_and_raw_strings(
    checker: ModuleType, tmp_path: Path
) -> None:
    path = tmp_path / "example.go"
    path.write_text(
        'package example\n// import "net/http"\nimport (\n'
        '  ctx "context"\n  _ `database/sql`\n)\n'
        'var fake = "import \\"os\\""\n'
    )
    assert checker.go_imports(path) == [("context", 4), ("database/sql", 5)]
