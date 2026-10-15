package main

import (
	"io"
	"log/slog"
	"net"
	"strconv"
	"testing"
)

func TestWebListenerUsesNextPortWhenInitialPortIsBusy(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	initialPort := busy.Addr().(*net.TCPAddr).Port
	listener, bound, err := listenWithFallback(busy.Addr().String(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, port, err := net.SplitHostPort(bound)
	if err != nil {
		t.Fatal(err)
	}
	actualPort, err := strconv.Atoi(port)
	if err != nil || actualPort <= initialPort {
		t.Fatalf("bound=%q must follow busy port %d", bound, initialPort)
	}
	if listener.Addr().String() != bound {
		t.Fatalf("reported=%q actual=%q", bound, listener.Addr())
	}
}

func TestWebListenerDoesNotHideInvalidAddress(t *testing.T) {
	listener, _, err := listenWithFallback("127.0.0.1:65536", slog.Default())
	if listener != nil {
		listener.Close()
	}
	if err == nil {
		t.Fatal("invalid port must not be treated as a port conflict")
	}
}
