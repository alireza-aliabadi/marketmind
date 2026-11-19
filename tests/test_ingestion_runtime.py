"""Real M2.3/M2.4 workflows; Testcontainers owns only disposable synthetic stores."""

import io
import os
import subprocess
import tarfile
import tempfile
import uuid
from contextlib import ExitStack
from pathlib import Path

import pytest

from contracts.validation import load_json, validate_event

ROOT = Path(__file__).resolve().parents[1]
SERVICE = ROOT / "services/market-data-service"
pytestmark = pytest.mark.skipif(
    os.environ.get("RUN_MARKET_RUNTIME_TESTS") != "1",
    reason="opt-in PostgreSQL/Kafka Testcontainers workflow: make market-data-test",
)


@pytest.mark.parametrize(
    "scenario",
    [
        "TestKafkaDeliveryMilestone",
        "TestPersistenceMilestone",
        "TestConsumerRetryDoesNotSkipFailedMessage",
        "TestProductionDatabaseControls",
        "TestSupervisedWorkerRuntime",
    ],
)
def test_ingestion_milestone(scenario: str) -> None:
    # Import only for opt-in runs, keeping the ordinary suite Docker-free.
    from testcontainers.core.container import DockerContainer
    from testcontainers.core.wait_strategies import ExecWaitStrategy

    token = uuid.uuid4().hex[:16]
    password = "synthetic-test-only-password"
    postgres = (
        DockerContainer("timescale/timescaledb:2.31.0-pg18", name=f"marketmind-m2-pg-{token}")
        .with_envs(
            POSTGRES_USER="postgres",
            POSTGRES_PASSWORD=password,
            POSTGRES_DB="marketmind",
            PGDATA="/var/lib/postgresql/18/docker",
            TIMESCALEDB_TELEMETRY="off",
            TS_TUNE_MEMORY="1GB",
            TS_TUNE_NUM_CPUS="2",
        )
        .with_tmpfs_mount("/var/lib/postgresql", "size=512m")
        .with_command(["postgres", "-c", "shared_preload_libraries=timescaledb"])
        .with_kwargs(mem_limit="1g", shm_size="256m")
        .waiting_for(
            ExecWaitStrategy(
                [
                    "sh",
                    "-ec",
                    "PGPASSWORD=$POSTGRES_PASSWORD psql -h 127.0.0.1 -U postgres "
                    "-d marketmind -Atc 'SELECT 1' "
                    "| grep -qx 1",
                ]
            ).with_startup_timeout(180)
        )
    )
    kafka = (
        DockerContainer("apache/kafka:4.4.0", name=f"marketmind-m2-kafka-{token}")
        .with_envs(
            CLUSTER_ID="MkU3OEVBNTcwNTJENDM2Qk",
            KAFKA_NODE_ID="1",
            KAFKA_PROCESS_ROLES="broker,controller",
            KAFKA_CONTROLLER_QUORUM_VOTERS="1@localhost:9093",
            KAFKA_CONTROLLER_LISTENER_NAMES="CONTROLLER",
            KAFKA_LISTENERS="PLAINTEXT://:9092,CONTROLLER://:9093",
            KAFKA_ADVERTISED_LISTENERS="PLAINTEXT://kafka:9092",
            KAFKA_LISTENER_SECURITY_PROTOCOL_MAP="PLAINTEXT:PLAINTEXT,CONTROLLER:PLAINTEXT",
            KAFKA_INTER_BROKER_LISTENER_NAME="PLAINTEXT",
            KAFKA_LOG_DIRS="/tmp/marketmind-test-kafka",
            KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR="1",
            KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR="1",
            KAFKA_TRANSACTION_STATE_LOG_MIN_ISR="1",
            KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS="0",
            KAFKA_AUTO_CREATE_TOPICS_ENABLE="false",
            KAFKA_HEAP_OPTS="-Xms128m -Xmx512m",
        )
        .with_tmpfs_mount("/tmp/marketmind-test-kafka", "size=512m")
        .with_kwargs(mem_limit="1g")
        .waiting_for(
            ExecWaitStrategy(
                [
                    "/opt/kafka/bin/kafka-broker-api-versions.sh",
                    "--bootstrap-server",
                    "localhost:9092",
                ]
            ).with_startup_timeout(180)
        )
    )
    # Docker supports ephemeral loopback tuple bindings; no development host ports.
    postgres.ports["5432/tcp"] = ("127.0.0.1", None)
    kafka.ports["9092/tcp"] = ("127.0.0.1", None)
    with ExitStack() as stack:
        evidence = Path(
            stack.enter_context(
                tempfile.TemporaryDirectory(prefix="marketmind-m2-evidence-", dir="/tmp/opencode")
            )
        )
        binary = evidence / "market-data"
        build = subprocess.run(
            ["go", "build", "-o", str(binary), "./cmd/market-data"],
            cwd=SERVICE,
            capture_output=True,
            text=True,
            timeout=120,
            check=False,
        )
        assert build.returncode == 0, build.stderr
        stack.enter_context(postgres)
        stack.enter_context(kafka)
        env = {
            **os.environ,
            "RUN_MARKET_RUNTIME_TESTS": "1",
            "MARKET_TEST_ADMIN_DSN": f"postgresql://postgres:{password}@127.0.0.1:"
            f"{postgres.get_exposed_port(5432)}/marketmind?sslmode=disable",
            "MARKET_TEST_KAFKA": f"127.0.0.1:{kafka.get_exposed_port(9092)}",
            "MARKET_TEST_BINARY": str(binary),
            "MARKET_TEST_EVIDENCE_DIR": str(evidence),
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
                f"^{scenario}$",
            ],
            cwd=SERVICE,
            env=env,
            capture_output=True,
            text=True,
            timeout=240,
            check=False,
        )
        print(result.stdout)
        assert result.returncode == 0, result.stdout + result.stderr
        events = list(evidence.glob("*.event.json"))
        if scenario != "TestSupervisedWorkerRuntime":
            assert events, "ingestion runtime must produce actual envelope evidence"
        for path in events:
            validate_event(load_json(path))
        if scenario == "TestPersistenceMilestone":
            # Dump the entire DB: a schema-only dump omits Timescale chunks/catalog.
            dump = postgres.get_wrapped_container().exec_run(
                ["pg_dump", "-U", "postgres", "-d", "marketmind", "-Fc"], demux=True
            )
            assert dump.exit_code == 0, "private synthetic database backup failed"
            dump_bytes, _ = dump.output
            archive = io.BytesIO()
            with tarfile.open(fileobj=archive, mode="w") as bundle:
                member = tarfile.TarInfo("marketmind-test.dump")
                member.size = len(dump_bytes)
                member.mode = 0o600
                bundle.addfile(member, io.BytesIO(dump_bytes))
            assert postgres.get_wrapped_container().put_archive("/tmp", archive.getvalue())
            for command in [
                [
                    "psql",
                    "-U",
                    "postgres",
                    "-d",
                    "postgres",
                    "-v",
                    "ON_ERROR_STOP=1",
                    "-c",
                    "CREATE DATABASE marketmind_restore",
                ],
                [
                    "psql",
                    "-U",
                    "postgres",
                    "-d",
                    "marketmind_restore",
                    "-v",
                    "ON_ERROR_STOP=1",
                    "-c",
                    "CREATE EXTENSION IF NOT EXISTS timescaledb; SELECT timescaledb_pre_restore()",
                ],
                [
                    "pg_restore",
                    "-U",
                    "postgres",
                    "-d",
                    "marketmind_restore",
                    "--exit-on-error",
                    "/tmp/marketmind-test.dump",
                ],
                [
                    "psql",
                    "-U",
                    "postgres",
                    "-d",
                    "marketmind_restore",
                    "-v",
                    "ON_ERROR_STOP=1",
                    "-c",
                    "SELECT timescaledb_post_restore()",
                ],
            ]:
                restored = postgres.exec(command)
                assert restored.exit_code == 0, restored.output.decode(errors="replace")
            checked = postgres.exec(
                [
                    "psql",
                    "-U",
                    "postgres",
                    "-d",
                    "marketmind_restore",
                    "-At",
                    "-v",
                    "ON_ERROR_STOP=1",
                    "-c",
                    "SELECT (SELECT count(*) FROM market_data.market_prices), "
                    "(SELECT count(*) FROM market_data.economic_observations), "
                    "(SELECT value::text FROM market_data.economic_observations "
                    "WHERE period_date='2024-01-01'), "
                    "(SELECT tableowner FROM pg_tables WHERE schemaname='market_data' "
                    "AND tablename='companies')",
                ]
            )
            assert checked.exit_code == 0
            assert checked.output.decode().strip() == "4|3|100.123456789012345678|mm_market_data"
            print("Full PostgreSQL/Timescale restore preserved chunks, exact values and ownership")
