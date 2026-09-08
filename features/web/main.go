package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress"
)

type webOptions struct {
	addr          string
	snapshotDir   string
	controlSocket string
	legacyConfig  string
}

func parseWebOptions(args []string) (webOptions, error) {
	var options webOptions
	flags := flag.NewFlagSet("catmonitor-web", flag.ContinueOnError)
	flags.StringVar(&options.addr, "addr", ":19322", "listen address (port taken => auto +1)")
	flags.StringVar(&options.snapshotDir, "snapshot-dir", "/var/lib/catmonitor/snapshot", "daemon snapshot directory")
	flags.StringVar(&options.controlSocket, "control-socket", "/run/catmonitor/control.sock", "daemon Stress control socket")
	flags.StringVar(&options.legacyConfig, "config", "", "deprecated compatibility flag; Web reads snapshots and the daemon control socket")
	if err := flags.Parse(args); err != nil {
		return webOptions{}, err
	}
	if flags.NArg() != 0 {
		return webOptions{}, fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	return options, nil
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	options, err := parseWebOptions(os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		logger.Error("invalid command line", "error", err)
		os.Exit(2)
	}
	if options.legacyConfig != "" {
		logger.Warn("-config is deprecated and ignored; Web reads daemon snapshots and the optional Stress control socket", "path", options.legacyConfig)
	}
	client, err := stress.NewControlClient(options.controlSocket)
	if err != nil {
		logger.Error("invalid stress control socket", "error", err)
		os.Exit(1)
	}
	listener, bound, err := listenWithFallback(options.addr, logger)
	if err != nil {
		logger.Error("failed to listen", "error", err)
		os.Exit(1)
	}
	server := &http.Server{
		Handler:           NewServer(options.snapshotDir, logger, client, bound).Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	errorsCh := make(chan error, 1)
	go func() {
		logger.Info("web server starting", "addr", bound, "snapshot_dir", options.snapshotDir)
		errorsCh <- server.Serve(listener)
	}()
	select {
	case <-ctx.Done():
	case serveErr := <-errorsCh:
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			logger.Error("web server failed", "error", serveErr)
		}
		cancel()
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
}

func listenWithFallback(initialAddr string, logger *slog.Logger) (net.Listener, string, error) {
	host, portString, err := net.SplitHostPort(initialAddr)
	if err != nil {
		listener, listenErr := net.Listen("tcp", initialAddr)
		return listener, initialAddr, listenErr
	}
	port, err := strconv.Atoi(portString)
	if err != nil {
		listener, listenErr := net.Listen("tcp", initialAddr)
		return listener, initialAddr, listenErr
	}
	addr := initialAddr
	for {
		listener, listenErr := net.Listen("tcp", addr)
		if listenErr == nil {
			return listener, addr, nil
		}
		if !errors.Is(listenErr, syscall.EADDRINUSE) {
			return nil, addr, listenErr
		}
		logger.Warn("port in use, trying next", "addr", addr)
		port++
		addr = net.JoinHostPort(host, strconv.Itoa(port))
	}
}
