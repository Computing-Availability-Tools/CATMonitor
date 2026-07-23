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

	// Blank imports trigger collector self-registration via init(), exactly as
	// cmd/catmonitor does. This reuses the existing collectors without modifying
	// them; the scheduler only discovers collectors imported here. The static
	// hardware-identity specs (device model / GPU / NPU / disk / NIC) are NOT a
	// periodic collector — they are gathered once at startup by collectHWSpecs
	// (see hwinfo.go) so the periodic loop stays free of one-shot logic.
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/chassis"
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/cpu"
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/disk"
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/gpu"
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/memory"
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/network"
	_ "github.com/Computing-Availability-Tools/CATMonitor/internal/collectors/npu"

	"github.com/Computing-Availability-Tools/CATMonitor/internal/metrics"
)

func main() {
	configPath := flag.String("config", "features/web/config.yaml", "config file path")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	if err := metrics.Init("configs/metrics.yaml"); err != nil {
		logger.Error("metrics catalog init failed", "error", err)
		os.Exit(1)
	}
	// Web module reads its own metrics.yaml first (merged over the default).
	if err := metrics.LoadModuleOverride("features/web/metrics.yaml"); err != nil {
		logger.Error("web metrics override failed", "error", err)
		os.Exit(1)
	}
	// dfee module needs the 8 raw CPU time metrics (Low in default catalog)
	// for its utilization derivation; override them to Medium.
	if err := metrics.LoadModuleOverride("features/dfee/metrics.yaml"); err != nil {
		logger.Error("dfee metrics override failed", "error", err)
	}

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	dc := NewDataCollector(cfg, logger)

	// Collect hardware identity specs once at startup (non-blocking: smartctl /
	// dmidecode / nvidia-smi may take a moment; the snapshot's Specs field stays
	// empty until this returns, then is populated on the next collectOnce).
	go func() {
		specs := collectHWSpecs()
		dc.SetHWSpecs(specs)
		logger.Info("hardware specs collected", "count", len(specs))
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		logger.Info("data collector starting",
			"interval", cfg.Collector.RefreshInterval,
			"snapshot", cfg.Storage.SnapshotPath)
		dc.Run(ctx)
	}()

	srv := NewServer(cfg, dc, logger)
	httpServer := &http.Server{
		Handler: srv.Routes(),
	}

	ln, addr, err := listenWithFallback(cfg.Server.Addr, logger)
	if err != nil {
		logger.Error("failed to listen", "error", err)
		cancel()
		os.Exit(1)
	}
	cfg.Server.Addr = addr

	go func() {
		logger.Info("web server starting", "addr", addr)
		if err := httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			logger.Error("http server error", "error", err)
			cancel()
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	logger.Info("shutting down", "signal", sig)

	cancel()
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	_ = httpServer.Shutdown(shutCtx)
}

// listenWithFallback tries to listen on initialAddr; if the port is already in
// use it increments the port (default :9527 -> :9528 -> :9529 ...) until a free
// port is found, returning the acquired listener and the actual address bound.
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
