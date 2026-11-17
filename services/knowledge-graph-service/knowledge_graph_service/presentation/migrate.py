"""Operator-only migration entrypoint. Configuration is explicit; .env is not loaded."""

import json
import os
import sys
from urllib.parse import urlsplit

from neo4j import GraphDatabase

from knowledge_graph_service.infra.schema import GraphSchemaMigrator


def main() -> int:
    uri = os.environ.get("GRAPH_SCHEMA_URI", "")
    username = os.environ.get("GRAPH_SCHEMA_USERNAME", "")
    password = os.environ.get("GRAPH_SCHEMA_PASSWORD", "")
    parsed = urlsplit(uri)
    if (
        parsed.scheme not in {"bolt", "neo4j", "bolt+s", "neo4j+s"}
        or not parsed.hostname
        or parsed.username
        or not username
        or not password
    ):
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
            results = GraphSchemaMigrator(driver).apply()
    except Exception:
        # Driver errors may contain addresses or data: do not echo credentials.
        print(
            "Graph schema operation failed; verify connectivity, privileges and checksums",
            file=sys.stderr,
        )
        return 1
    print(json.dumps([result.summary() for result in results]))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
