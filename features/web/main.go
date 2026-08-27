package main

import (
	"context"
	"errors"
	"flag"
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

func main() {
	addr := flag.String("addr", ":19322", "listen address (port taken => auto +1)")
	dir := flag.String("snapshot-dir", "/var/lib/catmonitor/snapshot", "daemon snapshot directory")
	controlSocket := flag.String("control-socket", "/run/catmonitor/control.sock", "daemon Stress control socket")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	client, err := stress.NewControlClient(*controlSocket)
	if err != nil {
		logger.Error("invalid stress control socket", "error", err)
		os.Exit(1)
	}
	listener, bound, err := listenWithFallback(*addr, logger)
	if err != nil {
		logger.Error("failed to listen", "error", err)
		os.Exit(1)
	}
	server := &http.Server{
		Handler:           NewServer(*dir, logger, client, bound).Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	errorsCh := make(chan error, 1)
	go func() {
		logger.Info("web server starting", "addr", bound, "snapshot_dir", *dir)
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
