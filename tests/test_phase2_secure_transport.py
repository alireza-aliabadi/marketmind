"""Actual private SASL/TLS Kafka transport, never a live paid provider."""

import os
import subprocess
import tempfile
import uuid
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]
pytestmark = pytest.mark.skipif(
    os.environ.get("RUN_MARKET_SECURE_TESTS") != "1",
    reason="opt-in authenticated TLS Kafka: make market-production-test",
)


def test_authenticated_tls_kafka() -> None:
    from testcontainers.core.container import DockerContainer
    from testcontainers.core.wait_strategies import ExecWaitStrategy

    with tempfile.TemporaryDirectory(
        prefix="marketmind-secure-kafka-", dir="/tmp/opencode"
    ) as directory:
        path = Path(directory)
        path.chmod(0o755)
        result = subprocess.run(
            [
                "openssl",
                "req",
                "-x509",
                "-newkey",
                "rsa:2048",
                "-nodes",
                "-days",
                "1",
                "-keyout",
                str(path / "key.pem"),
                "-out",
                str(path / "ca.pem"),
                "-subj",
                "/CN=kafka",
                "-addext",
                "subjectAltName=DNS:kafka,DNS:localhost,IP:127.0.0.1",
            ],
            capture_output=True,
            timeout=30,
            check=False,
        )
        assert result.returncode == 0, "fixture certificate generation failed"
        (path / "broker.pem").write_bytes(
            (path / "key.pem").read_bytes() + (path / "ca.pem").read_bytes()
        )
        (path / "broker.pem").chmod(0o644)
        broker = (
            DockerContainer(
                "apache/kafka:4.4.0", name=f"marketmind-secure-kafka-{uuid.uuid4().hex[:12]}"
            )
            .with_envs(
                CLUSTER_ID="MkU3OEVBNTcwNTJENDM2Qk",
                KAFKA_NODE_ID="1",
                KAFKA_PROCESS_ROLES="broker,controller",
                KAFKA_CONTROLLER_QUORUM_VOTERS="1@localhost:9094",
                KAFKA_CONTROLLER_LISTENER_NAMES="CONTROLLER",
                KAFKA_LISTENERS="CLIENT://:9093,INTERNAL://:19092,CONTROLLER://:9094",
                KAFKA_ADVERTISED_LISTENERS="CLIENT://kafka:9093,INTERNAL://kafka:19092",
                KAFKA_LISTENER_SECURITY_PROTOCOL_MAP="CLIENT:SASL_SSL,INTERNAL:PLAINTEXT,CONTROLLER:PLAINTEXT",
                KAFKA_INTER_BROKER_LISTENER_NAME="INTERNAL",
                KAFKA_SASL_ENABLED_MECHANISMS="PLAIN",
                KAFKA_LISTENER_NAME_CLIENT_PLAIN_SASL_JAAS_CONFIG=(
                    "org.apache.kafka.common.security.plain.PlainLoginModule "
                    'required user_ingestor="synthetic-secret";'
                ),
                KAFKA_SSL_KEYSTORE_TYPE="PEM",
                KAFKA_SSL_KEYSTORE_LOCATION="/tls/broker.pem",
                KAFKA_SSL_TRUSTSTORE_TYPE="PEM",
                KAFKA_SSL_TRUSTSTORE_LOCATION="/tls/ca.pem",
                KAFKA_SSL_CLIENT_AUTH="none",
                KAFKA_AUTO_CREATE_TOPICS_ENABLE="false",
                KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR="1",
                KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS="0",
                KAFKA_LOG_DIRS="/tmp/private-kafka",
                KAFKA_HEAP_OPTS="-Xms128m -Xmx512m",
            )
            .with_volume_mapping(path, "/tls", "ro")
            .with_tmpfs_mount("/tmp/private-kafka", "size=512m")
            .with_kwargs(mem_limit="1g")
            .waiting_for(
                ExecWaitStrategy(
                    [
                        "/opt/kafka/bin/kafka-broker-api-versions.sh",
                        "--bootstrap-server",
                        "localhost:19092",
                    ]
                ).with_startup_timeout(180)
            )
        )
        broker.ports["9093/tcp"] = ("127.0.0.1", None)
        with broker:
            env = {
                **os.environ,
                "RUN_MARKET_SECURE_TESTS": "1",
                "MARKET_SECURE_BROKER": f"127.0.0.1:{broker.get_exposed_port(9093)}",
                "MARKET_SECURE_CA": str(path / "ca.pem"),
            }
            result = subprocess.run(
                [
                    "go",
                    "test",
                    "-race",
                    "-count=1",
                    "-v",
                    "./internal/infra/kafka",
                    "-run",
                    "^TestSecureKafkaRuntime$",
                ],
                cwd=ROOT / "services/market-data-service",
                env=env,
                capture_output=True,
                text=True,
                timeout=120,
                check=False,
            )
            print(result.stdout)
            assert result.returncode == 0, result.stdout + result.stderr


def test_verified_postgres_tls() -> None:
    from testcontainers.core.container import DockerContainer
    from testcontainers.core.wait_strategies import ExecWaitStrategy

    with tempfile.TemporaryDirectory(
        prefix="marketmind-secure-postgres-", dir="/tmp/opencode"
    ) as directory:
        path = Path(directory)
        path.chmod(0o755)
        certificate = subprocess.run(
            [
                "openssl",
                "req",
                "-x509",
                "-newkey",
                "rsa:2048",
                "-nodes",
                "-days",
                "1",
                "-keyout",
                str(path / "key.pem"),
                "-out",
                str(path / "ca.pem"),
                "-subj",
                "/CN=localhost",
                "-addext",
                "subjectAltName=DNS:localhost,IP:127.0.0.1",
            ],
            capture_output=True,
            timeout=30,
            check=False,
        )
        assert certificate.returncode == 0, "synthetic certificate generation failed"
        (path / "key.pem").chmod(0o644)
        postgres = (
            DockerContainer(
                "timescale/timescaledb:2.31.0-pg18",
                name=f"marketmind-secure-pg-{uuid.uuid4().hex[:12]}",
            )
            .with_envs(
                POSTGRES_USER="postgres",
                POSTGRES_PASSWORD="synthetic-secret",
                POSTGRES_DB="marketmind",
                PGDATA="/var/lib/postgresql/18/docker",
                TIMESCALEDB_TELEMETRY="off",
                TS_TUNE_MEMORY="1GB",
                TS_TUNE_NUM_CPUS="2",
            )
            .with_volume_mapping(path, "/fixture-tls", "ro")
            .with_volume_mapping(
                ROOT / "infra/postgres/init/010-timescaledb.sql",
                "/docker-entrypoint-initdb.d/010-timescaledb.sql",
                "ro",
            )
            .with_volume_mapping(
                ROOT / "infra/postgres/init/020-service-ownership.sql",
                "/docker-entrypoint-initdb.d/020-service-ownership.sql",
                "ro",
            )
            .with_tmpfs_mount("/var/lib/postgresql", "size=512m")
            .with_command(
                [
                    "sh",
                    "-ec",
                    "mkdir -p /tmp/private-tls; cp /fixture-tls/key.pem /tmp/private-tls/key.pem; "
                    "cp /fixture-tls/ca.pem /tmp/private-tls/cert.pem; "
                    "chown postgres:postgres /tmp/private-tls/key.pem; "
                    "chmod 600 /tmp/private-tls/key.pem; "
                    "exec docker-entrypoint.sh postgres -c shared_preload_libraries=timescaledb "
                    "-c ssl=on -c ssl_cert_file=/tmp/private-tls/cert.pem "
                    "-c ssl_key_file=/tmp/private-tls/key.pem",
                ]
            )
            .with_kwargs(mem_limit="1g", shm_size="256m")
            .waiting_for(
                ExecWaitStrategy(
                    [
                        "sh",
                        "-ec",
                        "PGPASSWORD=$POSTGRES_PASSWORD psql -h 127.0.0.1 -U postgres "
                        '-d marketmind -Atc "SELECT 1 FROM pg_roles '
                        "WHERE rolname='mm_market_data'\" "
                        "| grep -qx 1",
                    ]
                ).with_startup_timeout(180)
            )
        )
        postgres.ports["5432/tcp"] = ("127.0.0.1", None)
        with postgres:
            env = {
                **os.environ,
                "RUN_MARKET_SECURE_TESTS": "1",
                "MARKET_SECURE_POSTGRES": "postgresql://postgres:synthetic-secret@127.0.0.1:"
                f"{postgres.get_exposed_port(5432)}/marketmind?sslmode=verify-full"
                f"&sslrootcert={path / 'ca.pem'}",
            }
            result = subprocess.run(
                [
                    "go",
                    "test",
                    "-race",
                    "-count=1",
                    "-v",
                    "./internal/infra/postgres",
                    "-run",
                    "^TestSecurePostgresRuntime$",
                ],
                cwd=ROOT / "services/market-data-service",
                env=env,
                capture_output=True,
                text=True,
                timeout=120,
                check=False,
            )
            print(result.stdout)
            assert result.returncode == 0, result.stdout + result.stderr
