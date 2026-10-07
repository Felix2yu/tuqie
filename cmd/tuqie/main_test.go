package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// freeAddr reserves a port, then releases it so run() can bind it. The gap is
// harmless: a failure to listen surfaces as an error from run either way.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

func get(url string) (*http.Response, error) {
	client := &http.Client{Timeout: 2 * time.Second}
	return client.Get(url)
}

// TestRunServesThenShutsDownOnSignal covers the whole wiring a container gets:
// upload dir, embedded frontend, and a clean exit on SIGTERM.
func TestRunServesThenShutsDownOnSignal(t *testing.T) {
	addr := freeAddr(t)
	done := make(chan error, 1)
	go func() { done <- run(addr, t.TempDir(), time.Hour) }()

	deadline := time.Now().Add(5 * time.Second)
	var resp *http.Response
	var err error
	for time.Now().Before(deadline) {
		if resp, err = get("http://" + addr + "/api/health"); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("health never came up: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health %d", resp.StatusCode)
	}

	// The frontend comes from the embedded build, not from disk.
	resp, err = get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("static: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("static %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("content-type %q", ct)
	}

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("clean shutdown should return nil, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return after SIGTERM")
	}
}

func TestRunFailsWhenUploadDirIsUnusable(t *testing.T) {
	// A regular file where the data directory should go: MkdirAll cannot win.
	blocker := filepath.Join(t.TempDir(), "occupied")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := run(freeAddr(t), filepath.Join(blocker, "tuqie"), time.Hour)
	if err == nil {
		t.Fatal("want the store error to abort startup")
	}
}

func TestRunFailsOnABadListenAddress(t *testing.T) {
	err := run(fmt.Sprintf("127.0.0.1:%d", 70000), t.TempDir(), time.Hour)
	if err == nil {
		t.Fatal("want the listen error to reach the caller")
	}
}
