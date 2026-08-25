//go:build linux

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress/runnerapi"
)

func main() {
	socket := flag.String("socket", "/run/catmonitor-stress/cpu-runner.sock", "absolute Unix socket path")
	adapter := flag.String("adapter", "/opt/catmonitor/stress/benchmark_check.sh", "absolute runner-local adapter path")
	modeText := flag.String("socket-mode", "0660", "Unix socket permissions")
	flag.Parse()
	if flag.NArg() != 0 {
		fatal("unexpected positional arguments")
	}
	mode, err := strconv.ParseUint(*modeText, 8, 9)
	if err != nil {
		fatal("invalid -socket-mode: %v", err)
	}
	server, err := runnerapi.NewServer(*adapter)
	if err != nil {
		fatal("initialize CPU runner: %v", err)
	}
	signals, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	done := make(chan struct{})
	go func() {
		select {
		case <-signals.Done():
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_ = server.Shutdown(ctx)
		case <-done:
		}
	}()
	fmt.Printf("CATMonitor CPU stress runner listening on %s\n", *socket)
	err = server.Serve(*socket, os.FileMode(mode))
	close(done)
	if err != nil {
		fatal("serve CPU runner: %v", err)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(1)
}
