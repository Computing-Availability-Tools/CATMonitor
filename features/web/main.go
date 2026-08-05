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
	"github.com/Computing-Availability-Tools/CATMonitor/internal/config"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/platform"
)

// catmonitor-web is a standalone read-only binary that serves the CATMonitor
// web dashboard + API. It reads daemon-produced snapshot files (snapshot.json +
// snapshot_<comp>.json) from -snapshot-dir, which must match the daemon's
// snapshot.dir (daemon must run with snapshot.enabled:true and features
// including web). It does not collect metrics. The independently mounted
// stress feature may execute only when explicitly enabled in the shared
// CATMonitor config and the Web listener is loopback-only.
func main() {
	addr := flag.String("addr", ":9527", "listen address (port taken => auto +1)")
	dir := flag.String("snapshot-dir", "/var/lib/catmonitor/snapshot", "daemon snapshot dir (must match catmonitor.yaml snapshot.dir)")
	configPath := flag.String("config", platform.ConfigPath(), "CATMonitor config path (default: platform path or CATMONITOR_CONFIG)")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	catCfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("failed to load CATMonitor config", "path", *configPath, "error", err)
		os.Exit(1)
	}
	stressManager := stress.NewManagerWithLogger(catCfg.Stress, logger)

	srv := NewServer(*dir, logger, stressManager, *addr)
	httpServer := &http.Server{Handler: srv.Routes()}

	ln, bound, err := listenWithFallback(*addr, logger)
	if err != nil {
		logger.Error("failed to listen", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	go func() {
		logger.Info("web server starting (snapshot read-only consumer)", "addr", bound, "snapshot_dir", *dir, "config_path", *configPath)
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
	stressCtx, stressCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stressCancel()
	if err := stressManager.Shutdown(stressCtx); err != nil {
		logger.Error("stress manager shutdown failed", "error", err)
	}
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
