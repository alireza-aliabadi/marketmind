"""Operator-only schema setup. Configuration is explicit; .env is not loaded."""

import argparse
import json
import os
import sys
from urllib.parse import urlsplit

from neo4j import GraphDatabase

from knowledge_graph_service.infra.schema import ensure_schema


def main() -> int:
    parser = argparse.ArgumentParser(description="Install missing Neo4j constraints and indexes")
    parser.add_argument("--database", default="neo4j", help="Explicit target database")
    args = parser.parse_args()
    uri = os.environ.get("GRAPH_SCHEMA_URI", "")
    username = os.environ.get("GRAPH_SCHEMA_USERNAME", "")
    password = os.environ.get("GRAPH_SCHEMA_PASSWORD", "")
    try:
        parsed = urlsplit(uri)
        valid = (
            parsed.scheme in {"bolt", "neo4j", "bolt+s", "neo4j+s"}
            and bool(parsed.hostname)
            and parsed.username is None
            and parsed.password is None
            and bool(username)
            and bool(password)
        )
    except ValueError:
        valid = False
    if not valid:
        print(
            "Explicit GRAPH_SCHEMA_URI/USERNAME/PASSWORD operator settings are required",
            file=sys.stderr,
        )
        return 1
    try:
        with GraphDatabase.driver(
            uri,
            auth=(username, password),
            connection_timeout=10,
            max_transaction_retry_time=10,
        ) as driver:
            result = ensure_schema(driver, database=args.database)
    except Exception:
        # Driver errors may contain addresses or data: do not echo credentials.
        print(
            "Graph schema setup failed; verify connectivity, privileges and existing definitions",
            file=sys.stderr,
        )
        return 1
    print(json.dumps(result))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
