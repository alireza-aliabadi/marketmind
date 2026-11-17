#!/usr/bin/env python3
"""Check declared ownership and static Python/Go dependency direction, without services."""

from __future__ import annotations

import argparse
import ast
import json
import re
import sys
import tokenize
from dataclasses import dataclass
from pathlib import Path, PurePosixPath

LAYERS = {
    "domain": "domain",
    "application": "application",
    "presentation": "presentation",
    "infrastructure": "infrastructure",
    "infra": "infrastructure",
    "shared": "support",
    "support": "support",
}
INWARD = {
    "domain": {"domain"},
    "application": {"application", "domain"},
    "presentation": {"presentation", "application", "domain", "support"},
    "infrastructure": {"infrastructure", "application", "domain", "support"},
    "support": {"support", "application", "domain"},
}
# Positive allowlists keep an unknown framework or framework-bearing helper out too.
PYTHON_PURE = {
    "__future__",
    "abc",
    "collections",
    "copy",
    "dataclasses",
    "datetime",
    "decimal",
    "enum",
    "fractions",
    "functools",
    "hashlib",
    "itertools",
    "math",
    "operator",
    "re",
    "statistics",
    "string",
    "types",
    "typing",
    "unicodedata",
    "uuid",
    "zoneinfo",
}
PYTHON_APPLICATION = PYTHON_PURE | {"asyncio", "contextlib", "json", "logging", "time"}
GO_PURE = {
    "bytes",
    "cmp",
    "errors",
    "fmt",
    "math",
    "math/big",
    "math/bits",
    "slices",
    "sort",
    "strconv",
    "strings",
    "time",
    "unicode",
    "unicode/utf8",
    "unicode/utf16",
}
GO_APPLICATION = GO_PURE | {"context", "encoding/json", "log/slog", "sync", "sync/atomic"}
IGNORED = {".git", ".venv", "__pycache__", "node_modules", "vendor"}
SQL_NAME = re.compile(r"[a-z][a-z0-9_]*\Z")
GO_TOKEN = re.compile(
    r"(?P<space>\s+)|(?P<comment>//[^\n]*|/\*.*?\*/)"
    r'|(?P<string>"(?:\\.|[^"\\\n])*"|`[^`]*`)'
    r"|(?P<rune>'(?:\\.|[^'\\\n])*')"
    r"|(?P<identifier>[A-Za-z_]\w*)|(?P<punct>.)",
    re.DOTALL,
)


@dataclass(frozen=True)
class Issue:
    path: Path
    line: int
    message: str


@dataclass(frozen=True)
class Service:
    name: str
    root: Path
    language: str
    import_root: str
    composition_roots: tuple[Path, ...]


def layer(parts: tuple[str, ...]) -> str:
    if "tests" in parts:
        return "test"
    return next((LAYERS[part] for part in parts if part in LAYERS), "unclassified")


def load_manifest(root: Path) -> tuple[list[Service], list[Issue]]:
    path = root / "contracts/service-ownership.json"
    issues: list[Issue] = []
    try:
        text = path.read_text(encoding="utf-8")
        manifest = json.loads(text)
    except json.JSONDecodeError as exc:
        return [], [Issue(path, exc.lineno, f"invalid ownership JSON: {exc.msg}")]
    except (OSError, UnicodeError) as exc:
        return [], [Issue(path, 1, f"cannot read ownership manifest: {exc}")]
    if not isinstance(manifest, dict) or manifest.get("version") != 1:
        return [], [Issue(path, 1, "ownership manifest must be an object with version: 1")]
    records = manifest.get("services")
    if not isinstance(records, list) or not records:
        return [], [Issue(path, 1, "services must be a nonempty array")]

    def problem(line: int, message: str) -> None:
        issues.append(Issue(path, line, message))

    def location(name: str) -> int:
        match = re.search(r'"name"\s*:\s*' + re.escape(json.dumps(name)), text)
        return text.count("\n", 0, match.start()) + 1 if match else 1

    def declared_path(
        value: object, base: Path, line: int, label: str, *, required: bool = True
    ) -> Path | None:
        if (
            not isinstance(value, str)
            or not value
            or "\\" in value
            or PurePosixPath(value).is_absolute()
            or any(part in {".", "..", ""} for part in value.split("/"))
        ):
            problem(line, f"{label} must be a nonempty repository-relative path without traversal")
            return None
        resolved = (base / value).resolve()
        if not resolved.is_relative_to(base) or (required and not resolved.exists()):
            problem(line, f"{label} escapes its root or does not exist: {value}")
            return None
        return resolved

    services: list[Service] = []
    names: set[str] = set()
    schemas: dict[str, str] = {}
    roles: dict[str, str] = {}
    namespaces: dict[tuple[str, str], str] = {}
    for index, record in enumerate(records):
        if not isinstance(record, dict):
            problem(1, f"services[{index}] must be an object")
            continue
        name = record.get("name")
        if not isinstance(name, str) or not re.fullmatch(r"[a-z][a-z0-9-]*", name):
            problem(1, f"services[{index}].name must be a kebab-case service name")
            continue
        line = location(name)
        if name in names:
            problem(line, f"duplicate service name: {name}")
        names.add(name)
        if record.get("status") not in {"implemented", "template", "planned"}:
            problem(line, f"{name}: status must be implemented, template or planned")
        language = record.get("language")
        if language not in {"python", "go"}:
            problem(line, f"{name}: language must be python or go")
            continue
        import_root = record.get("import_root")
        pattern = r"[A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*" if language == "python" else r"[\w./-]+"
        if not isinstance(import_root, str) or not re.fullmatch(pattern, import_root):
            problem(line, f"{name}: invalid {language} import_root")
            continue
        service_root = declared_path(
            record.get("root"),
            root,
            line,
            f"{name}.root",
            required=record.get("status") != "planned",
        )
        if service_root is None:
            continue
        if service_root.exists() and not service_root.is_dir():
            problem(line, f"{name}.root must be a directory")
        for other in services:
            if service_root.is_relative_to(other.root) or other.root.is_relative_to(service_root):
                problem(line, f"overlapping service roots: {name} and {other.name}")
            separator = "." if language == "python" else "/"
            if language == other.language and (
                import_root == other.import_root
                or import_root.startswith(other.import_root + separator)
                or other.import_root.startswith(import_root + separator)
            ):
                problem(line, f"overlapping import roots: {name} and {other.name}")
        composition = record.get("composition_roots")
        roots: list[Path] = []
        if not isinstance(composition, list):
            problem(line, f"{name}.composition_roots must be an array")
        else:
            for entry in composition:
                wiring = declared_path(entry, service_root, line, f"{name}.composition_roots")
                if wiring is not None:
                    if layer(wiring.relative_to(service_root).parts) in {"domain", "application"}:
                        problem(
                            line, f"{name}: domain/application cannot be composition roots: {entry}"
                        )
                    roots.append(wiring)
        services.append(Service(name, service_root, language, import_root, tuple(roots)))
        module_file = service_root / "go.mod"
        if language == "go" and module_file.exists():
            try:
                module = re.search(
                    r"(?m)^\s*module\s+(\S+)", module_file.read_text(encoding="utf-8")
                )
                if module is None or module[1].strip('"') != import_root:
                    problem(
                        line, f"{name}: import_root does not match {module_file.relative_to(root)}"
                    )
            except (OSError, UnicodeError) as exc:
                problem(line, f"{name}: cannot read go.mod: {exc}")

        if "postgresql" not in record:
            problem(line, f"{name}.postgresql must explicitly be null or an ownership object")
        postgres = record.get("postgresql")
        if postgres is not None:
            if not isinstance(postgres, dict):
                problem(line, f"{name}.postgresql must be null or an ownership object")
                continue
            for field, owners in (("schema", schemas), ("owner_role", roles)):
                value = postgres.get(field)
                if not isinstance(value, str) or not SQL_NAME.fullmatch(value):
                    problem(line, f"{name}.postgresql.{field} must be a lowercase SQL identifier")
                elif value in owners:
                    problem(
                        line, f"duplicate PostgreSQL {field} '{value}': {owners[value]} and {name}"
                    )
                else:
                    owners[value] = name
            if (
                postgres.get("status") != "RESERVED"
                or postgres.get("role_login") is not False
                or postgres.get("runtime_credentials") != "PLANNED"
            ):
                problem(
                    line, f"{name}: declare RESERVED NOLOGIN ownership, runtime credentials PLANNED"
                )
        for resource in ("redis", "s3", "neo4j"):
            responsibilities = record.get(resource)
            if not isinstance(responsibilities, list):
                problem(line, f"{name}.{resource} must be an array of PLANNED responsibilities")
                continue
            for responsibility in responsibilities:
                if (
                    not isinstance(responsibility, dict)
                    or responsibility.get("status") != "PLANNED"
                    or not isinstance(responsibility.get("namespace"), str)
                    or not responsibility["namespace"]
                    or not isinstance(responsibility.get("responsibility"), str)
                    or not responsibility["responsibility"]
                ):
                    problem(
                        line,
                        f"{name}.{resource}: namespace/responsibility and status PLANNED required",
                    )
                    continue
                key = resource, responsibility["namespace"]
                if key in namespaces:
                    problem(
                        line,
                        f"duplicate {resource} namespace '{key[1]}': {namespaces[key]} and {name}",
                    )
                namespaces[key] = name

    topics = manifest.get("topics")
    topic_names: set[str] = set()
    if not isinstance(topics, list):
        problem(1, "topics must be an array of declared versioned responsibilities")
    else:
        for topic in topics:
            if not isinstance(topic, dict) or not isinstance(topic.get("name"), str):
                problem(1, "each topic must have a name")
                continue
            name = topic["name"]
            line = location(name)
            if not re.fullmatch(r"[a-z][a-z0-9_.-]*\.v[1-9][0-9]*", name) or name in topic_names:
                problem(line, f"invalid or duplicate versioned topic name: {name}")
            topic_names.add(name)
            owner, publisher = topic.get("owner"), topic.get("publisher")
            consumers = topic.get("consumers")
            if not isinstance(owner, str) or owner not in names or publisher != owner:
                problem(line, f"{name}: owner/publisher must be the same registered service")
            if (
                not isinstance(consumers, list)
                or any(
                    not isinstance(consumer, str) or consumer not in names for consumer in consumers
                )
                or len(set(consumers)) != len(consumers)
            ):
                problem(line, f"{name}: consumers must be unique registered services")
            if (topic.get("status"), topic.get("contract_status")) not in {
                ("PLANNED", "contract-only"),
                ("IMPLEMENTED", "implemented"),
            }:
                problem(
                    line, f"{name}: topic status must agree with planned or implemented contract"
                )
    return services, issues


def python_imports(path: Path, service: Service) -> list[tuple[str, int]]:
    with tokenize.open(path) as source:
        tree = ast.parse(source.read(), filename=str(path))
    package = service.import_root.split(".") + list(path.relative_to(service.root).parent.parts)
    imports: list[tuple[str, int]] = []
    dynamic_functions = {"__import__"}
    importlib_names = {"importlib"}
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            for alias in node.names:
                imports.append((alias.name, node.lineno))
                if alias.name == "importlib":
                    importlib_names.add(alias.asname or alias.name)
        elif isinstance(node, ast.ImportFrom):
            base = node.module.split(".") if node.module else []
            if node.level:
                keep = len(package) - node.level + 1
                if keep < len(service.import_root.split(".")):
                    raise SyntaxError(
                        "relative import escapes service root", (str(path), node.lineno, 1, "")
                    )
                base = package[:keep] + base
            for alias in node.names:
                target = base if alias.name == "*" else [*base, alias.name]
                imports.append((".".join(target), node.lineno))
                if node.module == "importlib" and alias.name == "import_module":
                    dynamic_functions.add(alias.asname or alias.name)
    for node in ast.walk(tree):
        if not isinstance(node, ast.Call):
            continue
        call = node.func
        dynamic = isinstance(call, ast.Name) and call.id in dynamic_functions
        dynamic |= (
            isinstance(call, ast.Attribute)
            and call.attr == "import_module"
            and isinstance(call.value, ast.Name)
            and call.value.id in importlib_names
        )
        if dynamic:
            if (
                not node.args
                or not isinstance(node.args[0], ast.Constant)
                or not isinstance(node.args[0].value, str)
                or node.args[0].value.startswith(".")
            ):
                raise SyntaxError(
                    "dynamic import requires an absolute literal target",
                    (str(path), node.lineno, 1, ""),
                )
            imports.append((node.args[0].value, node.lineno))
    return imports


def go_imports(path: Path) -> list[tuple[str, int]]:
    """Lex imports with aliases/raw strings, ignoring comments and other literals."""
    source = path.read_text(encoding="utf-8")
    tokens: list[tuple[str, str, int]] = []
    line = 1
    for match in GO_TOKEN.finditer(source):
        kind, value = match.lastgroup, match[0]
        if kind == "punct" and (value in {'"', "'", "`"} or source.startswith("/*", match.start())):
            raise SyntaxError("unterminated Go literal/comment", (str(path), line, 1, ""))
        if kind not in {"space", "comment"}:
            tokens.append((kind or "punct", value, line))
        line += value.count("\n")
    imports: list[tuple[str, int]] = []
    index = 0
    while index < len(tokens):
        kind, value, line = tokens[index]
        index += 1
        if kind != "identifier" or value != "import":
            continue
        grouped = index < len(tokens) and tokens[index][1] == "("
        if grouped:
            index += 1
        while True:
            while index < len(tokens) and tokens[index][1] == ";":
                index += 1
            if grouped and index < len(tokens) and tokens[index][1] == ")":
                index += 1
                break
            if index < len(tokens) and (
                tokens[index][0] == "identifier" or tokens[index][1] == "."
            ):
                index += 1
            if index >= len(tokens) or tokens[index][0] != "string":
                raise SyntaxError(
                    "expected Go import path or closing ')'", (str(path), line, 1, "")
                )
            _, literal, import_line = tokens[index]
            try:
                module = literal[1:-1] if literal.startswith("`") else ast.literal_eval(literal)
            except (SyntaxError, ValueError) as exc:
                raise SyntaxError(
                    "invalid Go import string", (str(path), import_line, 1, "")
                ) from exc
            imports.append((module, import_line))
            index += 1
            if not grouped:
                break
    return imports


def import_target(
    module: str, service: Service, services: list[Service], root: Path
) -> tuple[Service, str] | None:
    separator = "." if service.language == "python" else "/"
    normalized = module.replace("-", "_") if separator == "." else module
    candidates: list[tuple[int, Service, str]] = []
    for other in services:
        relative = other.root.relative_to(root).as_posix()
        if separator == ".":
            prefixes = {relative.replace("/", "."), other.name}
            if other.language == "python":
                prefixes.add(other.import_root)
            prefixes = {prefix.replace("-", "_") for prefix in prefixes}
        else:
            prefixes = {relative, f"marketmind/{relative}"}
            if other.language == "go":
                prefixes.add(other.import_root)
        for prefix in prefixes:
            if normalized == prefix or normalized.startswith(prefix + separator):
                suffix = normalized[len(prefix) :].lstrip(separator)
                candidates.append((len(prefix), other, layer(tuple(suffix.split(separator)))))
    if candidates:
        _, owner, target_layer = max(candidates, key=lambda candidate: candidate[0])
        return owner, target_layer
    # Service-local imports from a Python source root still obey the same layer matrix.
    if separator == "." and normalized.split(".")[0] in LAYERS:
        return service, layer(tuple(normalized.split(".")))
    return None


def check_architecture(root: Path) -> list[Issue]:
    root = root.resolve()
    services, issues = load_manifest(root)
    if issues:
        return issues
    sources: set[Path] = set()
    for base in {root / "services", root / "src", *(service.root for service in services)}:
        for suffix in ("*.py", "*.go"):
            sources.update(
                path
                for path in base.rglob(suffix)
                if not IGNORED.intersection(path.relative_to(root).parts)
            )
    for path in sorted(sources):
        service = next((service for service in services if path.is_relative_to(service.root)), None)
        if service is None:
            issues.append(
                Issue(
                    path, 1, "source is outside registered service roots; update ownership manifest"
                )
            )
            continue
        if not path.resolve().is_relative_to(service.root):
            issues.append(Issue(path, 1, f"source escapes {service.name} root via symlink"))
            continue
        if path.suffix != (".py" if service.language == "python" else ".go"):
            issues.append(Issue(path, 1, f"source language disagrees with {service.name} manifest"))
            continue
        source_layer = layer(path.relative_to(service.root).parts)
        if path.name.endswith("_test.go"):
            source_layer = "test"
        elif any(
            path == wiring or path.is_relative_to(wiring) for wiring in service.composition_roots
        ):
            source_layer = "composition"
        try:
            imports = (
                python_imports(path, service) if service.language == "python" else go_imports(path)
            )
        except (OSError, UnicodeError, SyntaxError) as exc:
            issues.append(
                Issue(path, getattr(exc, "lineno", None) or 1, f"cannot scan imports: {exc}")
            )
            continue
        if source_layer == "unclassified" and not (path.name == "__init__.py" and not imports):
            issues.append(
                Issue(
                    path,
                    1,
                    "source needs a domain/application/adapter/support layer "
                    "or explicit composition root",
                )
            )
        for module, line in imports:
            target = import_target(module, service, services, root)
            if target is not None:
                owner, target_layer = target
                if owner != service:
                    issues.append(
                        Issue(
                            path,
                            line,
                            f"cross-service import '{module}': "
                            f"{service.name} -> {owner.name}; use contracts",
                        )
                    )
                elif source_layer in INWARD and target_layer not in INWARD[source_layer]:
                    issues.append(
                        Issue(
                            path,
                            line,
                            f"{source_layer} -> {target_layer} forbidden: '{module}'; "
                            "dependencies must point inward",
                        )
                    )
            elif source_layer in {"domain", "application"}:
                if service.language == "python":
                    allowed = PYTHON_PURE if source_layer == "domain" else PYTHON_APPLICATION
                    permitted = module.split(".")[0] in allowed
                else:
                    allowed = GO_PURE if source_layer == "domain" else GO_APPLICATION
                    permitted = module in allowed
                if not permitted:
                    issues.append(
                        Issue(
                            path,
                            line,
                            f"{source_layer} cannot import external/framework or I/O package "
                            f"'{module}'; use an inward port",
                        )
                    )
    return issues


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--root",
        type=Path,
        default=Path(__file__).resolve().parents[1],
        help="repository root (default: this script's repository)",
    )
    args = parser.parse_args()
    root = args.root.resolve()
    issues = check_architecture(root)
    for issue in issues:
        display = issue.path.relative_to(root) if issue.path.is_relative_to(root) else issue.path
        print(f"{display}:{issue.line}: {issue.message}", file=sys.stderr)
    if issues:
        print(f"Architecture checks failed: {len(issues)} violation(s).", file=sys.stderr)
        return 1
    print("Architecture checks passed: ownership manifest and Python/Go static import boundaries.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
