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

// catmonitor-dfee is a standalone read-only binary that serves ONLY the dfee
// energy-efficiency SPA + API. It reads daemon-produced snapshot files
// (snapshot.json + snapshot_<comp>.json) from -snapshot-dir, which must match
// the daemon's snapshot.dir (daemon must run with snapshot.enabled:true and
// features including dfee). No web dashboard, no collection.
func main() {
	addr := flag.String("addr", ":9528", "listen address (port taken => auto +1)")
	dir := flag.String("snapshot-dir", "/var/lib/catmonitor/snapshot", "daemon snapshot dir (must match catmonitor.yaml snapshot.dir)")
	exporter := flag.String("exporter", "disabled", "enable Prometheus exporter: enabled|disabled")
	exporterPort := flag.String("exporter-port", "9333", "exporter listen port")
	device := flag.String("device", "", "NPU device filter (comma-separated, e.g. 0,1); empty = all")
	dockerContainer := flag.String("docker-container", "", "docker container name for software version collection")
	csvEnabled := flag.String("csv", "disabled", "enable CSV output: enabled|disabled")
	csvDir := flag.String("csv-dir", "/var/lib/catmonitor/csv", "CSV output directory")
	csvInterval := flag.String("csv-interval", "10s", "CSV write interval")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	mux := http.NewServeMux()
	Register(mux, *dir)

	httpServer := &http.Server{Handler: mux}
	ln, bound, err := listenWithFallback(*addr, logger)
	if err != nil {
		logger.Error("failed to listen", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	go func() {
		logger.Info("dfee server starting (read-only consumer)", "addr", bound, "snapshot_dir", *dir)
		if err := httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			logger.Error("http server error", "error", err)
			cancel()
		}
	}()

	// Start Prometheus exporter if enabled.
	var exporterServer *http.Server
	var exp *Exporter
	if *exporter == "enabled" {
		logger.Info("collecting static info for exporter...")
		exp = NewExporter(*dir, *device, *dockerContainer)
		expMux := http.NewServeMux()
		expMux.Handle("/metrics", exp)
		exporterServer = &http.Server{Addr: ":" + *exporterPort, Handler: expMux}
		go func() {
			logger.Info("exporter starting", "port", *exporterPort)
			if err := exporterServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Error("exporter server error", "error", err)
			}
		}()
	}

	// Start CSV writer if enabled.
	var csvW *CSVWriter
	if *csvEnabled == "enabled" {
		if exp == nil {
			exp = NewExporter(*dir, *device, *dockerContainer)
		}
		interval, err := time.ParseDuration(*csvInterval)
		if err != nil || interval <= 0 {
			interval = 10 * time.Second
		}
		csvW = NewCSVWriter(exp, *csvDir, interval)
		go func() {
			logger.Info("csv writer starting", "dir", *csvDir, "interval", interval)
			csvW.Run(ctx)
		}()
	}

	<-ctx.Done()
	logger.Info("shutting down", "signal", ctx.Err())
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	_ = httpServer.Shutdown(shutCtx)
	if exporterServer != nil {
		_ = exporterServer.Shutdown(shutCtx)
	}
	if csvW != nil {
		csvW.Close()
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
