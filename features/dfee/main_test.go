package main

import (
	"io"
	"log/slog"
	"net"
	"strconv"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestListenWithFallbackFreePort(t *testing.T) {
	// Grab a free port, release it, then listen on it: no fallback needed.
	ln0, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln0.Addr().(*net.TCPAddr).Port
	ln0.Close()

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	ln, bound, err := listenWithFallback(addr, testLogger())
	if err != nil {
		t.Fatalf("listenWithFallback: %v", err)
	}
	defer ln.Close()
	if bound != addr {
		t.Errorf("bound = %q, want %q (no fallback expected)", bound, addr)
	}
}

func TestListenWithFallbackPortInUse(t *testing.T) {
	// Find a base port P with P+1 free: occupy P and expect the fallback
	// to bind P+1.
	for try := 0; try < 20; try++ {
		ln0, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := ln0.Addr().(*net.TCPAddr).Port
		next := net.JoinHostPort("127.0.0.1", strconv.Itoa(port+1))

		probe, perr := net.Listen("tcp", next)
		if perr != nil {
			ln0.Close()
			continue // port+1 busy; pick another base
		}
		probe.Close()

		addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		ln, bound, err := listenWithFallback(addr, testLogger())
		if err != nil {
			ln0.Close()
			t.Fatalf("listenWithFallback: %v", err)
		}
		if bound != next {
			ln.Close()
			ln0.Close()
			t.Errorf("bound = %q, want %q (fallback to next port)", bound, next)
			continue
		}
		ln.Close()
		ln0.Close()
		return
	}
	t.Skip("no adjacent free port pair found")
}

func TestListenWithFallbackInvalidAddr(t *testing.T) {
	if _, _, err := listenWithFallback("missing-port", testLogger()); err == nil {
		t.Error("address without port must return an error")
	}
}

func TestListenWithFallbackNonNumericPort(t *testing.T) {
	if _, _, err := listenWithFallback("127.0.0.1:abc", testLogger()); err == nil {
		t.Error("non-numeric port must return an error")
	}
}
