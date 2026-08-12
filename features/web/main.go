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
)

// catmonitor-web is a standalone read-only binary that serves the CATMonitor
// web dashboard + API. It reads daemon-produced snapshot files (snapshot.json +
// snapshot_<comp>.json) from -snapshot-dir, which must match the daemon's
// snapshot.dir (daemon must run with snapshot.enabled:true and features
// including web). No collection.
func main() {
	addr := flag.String("addr", ":19322", "listen address (port taken => auto +1)")
	dir := flag.String("snapshot-dir", "/var/lib/catmonitor/snapshot", "daemon snapshot dir (must match catmonitor.yaml snapshot.dir)")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	srv := NewServer(*dir, logger)
	httpServer := &http.Server{Handler: srv.Routes()}

	ln, bound, err := listenWithFallback(*addr, logger)
	if err != nil {
		logger.Error("failed to listen", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	go func() {
		logger.Info("web server starting (read-only consumer)", "addr", bound, "snapshot_dir", *dir)
		if err := httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			logger.Error("http server error", "error", err)
			cancel()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down", "signal", ctx.Err())
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	_ = httpServer.Shutdown(shutCtx)
}

// listenWithFallback tries to listen on initialAddr; if the port is already in
// use it increments the port until a free one is found, returning the listener
// and the actual address bound.
func listenWithFallback(initialAddr string, logger *slog.Logger) (net.Listener, string, error) {
	host, portStr, err := net.SplitHostPort(initialAddr)
	if err != nil {
		ln, lerr := net.Listen("tcp", initialAddr)
		return ln, initialAddr, lerr
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		ln, lerr := net.Listen("tcp", initialAddr)
		return ln, initialAddr, lerr
	}
	addr := initialAddr
	for {
		ln, lerr := net.Listen("tcp", addr)
		if lerr == nil {
			return ln, addr, nil
		}
		if !errors.Is(lerr, syscall.EADDRINUSE) {
			return nil, addr, lerr
		}
		logger.Warn("port in use, trying next", "addr", addr)
		port++
		addr = net.JoinHostPort(host, strconv.Itoa(port))
	}
}
