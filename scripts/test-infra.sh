#!/usr/bin/env bash
set -Eeuo pipefail

project_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
project_name="marketmind-m13-test-$(date +%s)-$$"
compose_options=(--project-name "$project_name" --env-file "$project_root/.env.example" --file "$project_root/infra/compose.test.yml")
compose=(docker compose --project-directory "$project_root" --file "$project_root/compose.yml" --profile infra "${compose_options[@]}")

# Test credentials never replace the developer's .env or development volumes.
export POSTGRES_DB=marketmind POSTGRES_USER=marketmind POSTGRES_PASSWORD=m13-test-postgres-only
export NEO4J_PASSWORD=m13-test-neo4j-only
export MINIO_ACCESS_KEY=marketmind-m13-test MINIO_SECRET_KEY=m13-test-minio-only

[[ -f "$project_root/minio.license" && -s "$project_root/minio.license" ]] || {
  printf 'A valid local minio.license is required for AIStor.\n' >&2
  exit 1
}
"${compose[@]}" config --quiet

# Refuse to clean up a project that existed before this invocation.
for resource in container volume network; do
  case "$resource" in
    container) existing="$(docker ps --all --quiet --filter "label=com.docker.compose.project=$project_name")" ;;
    volume) existing="$(docker volume ls --quiet --filter "label=com.docker.compose.project=$project_name")" ;;
    network) existing="$(docker network ls --quiet --filter "label=com.docker.compose.project=$project_name")" ;;
  esac
  [[ -z "$existing" ]] || {
    printf 'Refusing to reuse existing test project %s.\n' "$project_name" >&2
    exit 1
  }
done

cleanup() {
  status=$?
  trap - EXIT
  if [[ "$status" -ne 0 ]]; then
    printf 'Infrastructure verification failed in %s.\n' "$project_name" >&2
    "${compose[@]}" ps --all || true
  fi
  # This project was uniquely allocated above; never remove development volumes.
  "${compose[@]}" down --volumes || status=1
  exit "$status"
}
trap cleanup EXIT

sql() {
  "${compose[@]}" exec -T postgres sh -ec '
    export PGPASSWORD="$POSTGRES_PASSWORD"
    exec psql -h 127.0.0.1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
      -X -v ON_ERROR_STOP=1 -qAt
  '
}

s3() {
  "${compose[@]}" exec -T aistor sh -ec '
    config_dir="$(mktemp -d)"
    trap "rm -rf \"$config_dir\"" EXIT
    mc --config-dir "$config_dir" alias set verification http://localhost:9000 \
      "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null
    mc --config-dir "$config_dir" "$@"
  ' sh "$@"
}

assert_marker() {
  [[ "$2" == "m13-persisted" ]] || {
    printf 'FAIL: %s did not preserve its fixture.\n' "$1" >&2
    exit 1
  }
  printf 'PASS: %s persistence after container recreation\n' "$1"
}

printf 'Verifying clean infrastructure in %s.\n' "$project_name"
"${compose[@]}" up --detach --quiet-pull --wait --wait-timeout 240
bash "$project_root/scripts/verify-infra.sh" "${compose_options[@]}"

# Synthetic fixtures exist only in this isolated project, not business schemas.
sql >/dev/null <<'SQL'
CREATE TABLE m13_storage_probe (observed_at TIMESTAMPTZ NOT NULL, marker TEXT NOT NULL);
SELECT create_hypertable('m13_storage_probe', by_range('observed_at'));
INSERT INTO m13_storage_probe VALUES ('2026-11-11T00:00:00Z', 'm13-persisted');
SQL
"${compose[@]}" exec -T redis redis-cli SET m13:storage:probe m13-persisted >/dev/null
"${compose[@]}" exec -T kafka /opt/kafka/bin/kafka-topics.sh \
  --bootstrap-server kafka:29092 --create --topic m13.storage.probe \
  --partitions 1 --replication-factor 1 >/dev/null
printf 'm13-persisted\n' | "${compose[@]}" exec -T kafka \
  /opt/kafka/bin/kafka-console-producer.sh --bootstrap-server kafka:29092 \
  --topic m13.storage.probe --command-property acks=all
"${compose[@]}" exec -T neo4j sh -ec '
  exec cypher-shell --address bolt://localhost:7687 --username neo4j \
    --password "${NEO4J_AUTH#*/}" "CREATE (:M13StorageProbe {marker: '\''m13-persisted'\''})"
' >/dev/null
s3 mb verification/m13-storage-probe >/dev/null
printf 'm13-persisted' | s3 pipe verification/m13-storage-probe/marker >/dev/null

# Recreate containers and networks while retaining their named volumes.
"${compose[@]}" down
"${compose[@]}" up --detach --quiet-pull --wait --wait-timeout 240
bash "$project_root/scripts/verify-infra.sh" "${compose_options[@]}"

marker="$(sql <<'SQL'
SELECT marker FROM m13_storage_probe;
SQL
)"
assert_marker PostgreSQL "$marker"
marker="$("${compose[@]}" exec -T redis redis-cli GET m13:storage:probe)"
assert_marker Redis "$marker"
marker="$("${compose[@]}" exec -T kafka \
  /opt/kafka/bin/kafka-console-consumer.sh --bootstrap-server kafka:29092 \
  --topic m13.storage.probe --from-beginning --max-messages 1 --timeout-ms 15000)"
assert_marker Kafka "$marker"
marker="$("${compose[@]}" exec -T neo4j sh -ec '
  exec cypher-shell --address bolt://localhost:7687 --username neo4j \
    --password "${NEO4J_AUTH#*/}" --format plain \
    "MATCH (probe:M13StorageProbe) RETURN probe.marker AS marker"
' | tr -d '\r' | grep -Fx '"m13-persisted"' | tr -d '"')"
assert_marker Neo4j "$marker"
marker="$(s3 cat verification/m13-storage-probe/marker)"
assert_marker AIStor "$marker"
printf 'M1.3 startup and persistence verification passed.\n'
