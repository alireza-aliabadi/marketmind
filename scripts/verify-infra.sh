#!/usr/bin/env bash
set -Eeuo pipefail

project_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
compose=(docker compose --project-directory "$project_root" --file "$project_root/compose.yml" --profile infra "$@")
services=(postgres redis kafka neo4j aistor)

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

# A running container without a passing health check is not ready.
for service in "${services[@]}"; do
  container_id="$("${compose[@]}" ps --all --quiet "$service")"
  [[ -n "$container_id" ]] || fail "$service has no container"

  state="$(
    docker inspect \
      --format '{{.State.Status}}:{{if .State.Health}}{{.State.Health.Status}}{{else}}missing{{end}}' \
      "$container_id"
  )"
  [[ "$state" == "running:healthy" ]] || fail "$service is $state"
done

timescale_version="$(
  "${compose[@]}" exec -T postgres sh -ec '
    export PGPASSWORD="$POSTGRES_PASSWORD"
    exec psql -h 127.0.0.1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
      -X -v ON_ERROR_STOP=1 -At
  ' <<'SQL'
SELECT extversion FROM pg_extension WHERE extname = 'timescaledb';
SQL
)"
[[ -n "$timescale_version" ]] || fail "TimescaleDB extension is missing"
printf 'PASS: PostgreSQL; TimescaleDB %s\n' "$timescale_version"

redis_response="$("${compose[@]}" exec -T redis redis-cli ping)"
[[ "$redis_response" == "PONG" ]] || fail "Redis did not return PONG"
printf 'PASS: Redis\n'

"${compose[@]}" exec -T kafka /opt/kafka/bin/kafka-topics.sh \
  --bootstrap-server kafka:29092 --list >/dev/null
printf 'PASS: Kafka\n'

"${compose[@]}" exec -T neo4j sh -ec '
  exec cypher-shell --address bolt://localhost:7687 \
    --username neo4j --password "${NEO4J_AUTH#*/}" "RETURN 1"
' >/dev/null
printf 'PASS: Neo4j\n'

# Do not retain root credentials in the mc client's configuration directory.
"${compose[@]}" exec -T aistor sh -ec '
  config_dir="$(mktemp -d)"
  trap "rm -rf \"$config_dir\"" EXIT
  mc --config-dir "$config_dir" alias set verification http://localhost:9000 \
    "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null
  mc --config-dir "$config_dir" ls verification >/dev/null
'
printf 'PASS: S3-compatible object storage\n'
printf 'M1.3 infrastructure readiness checks passed.\n'
