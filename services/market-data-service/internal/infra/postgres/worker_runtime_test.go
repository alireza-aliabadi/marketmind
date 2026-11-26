package postgres

import (
	"bytes"
	"net"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestSupervisedWorkerRuntime(t *testing.T) {
	ctx, _, _, dsn, _ := stack(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	process := exec.CommandContext(ctx, os.Getenv("MARKET_TEST_BINARY"), "worker", "--role", "relay", "--timeout", "2s")
	process.Env = append(os.Environ(), "MARKET_ENV=local", "MARKET_DATABASE_URL="+dsn, "MARKET_OPERATIONAL_ADDRESS="+address, "KAFKA_BOOTSTRAP_SERVERS="+os.Getenv("MARKET_TEST_KAFKA"))
	var logs bytes.Buffer
	process.Stdout = &logs
	process.Stderr = &logs
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if process.ProcessState == nil {
			_ = process.Process.Kill()
			_ = process.Wait()
		}
	}()
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(15 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		response, err := client.Get("http://" + address + "/health/ready")
		if err == nil {
			ready = response.StatusCode == 200
			response.Body.Close()
			if ready {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatal("compiled worker never became ready against actual dependencies")
	}
	response, err := client.Get("http://" + address + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	if err := process.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- process.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal("graceful worker shutdown failed", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("worker did not stop within grace period")
	}
	t.Log("actual compiled supervised worker, dependency readiness, metrics and SIGTERM shutdown passed")
}
