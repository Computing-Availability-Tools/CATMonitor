package main

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Computing-Availability-Tools/CATMonitor/features/snapshot"
	"github.com/Computing-Availability-Tools/CATMonitor/features/stress"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/collector"
	"github.com/Computing-Availability-Tools/CATMonitor/internal/version"
)

const historyPoints = 60

var webStartup = time.Now().Unix()

type Server struct {
	dir          string
	logger       *slog.Logger
	stressClient *stress.ControlClient
	listenAddr   string
}

func NewServer(dir string, logger *slog.Logger, client *stress.ControlClient, listenAddr string) *Server {
	return &Server{dir: dir, logger: logger, stressClient: client, listenAddr: listenAddr}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		s.logger.Error("static fs sub failed", "error", err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(sub))))
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /api/snapshot", s.handleSnapshot)
	mux.HandleFunc("GET /api/collectors", s.handleCollectors)
	mux.HandleFunc("GET /api/config", s.handleConfig)
	if s.stressClient != nil {
		stress.Register(mux, s.stressClient, s.listenAddr, s.logger)
	}
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	data, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "index not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}
func (s *Server) globalPath() string { return filepath.Join(s.dir, "snapshot.json") }
func (s *Server) readGlobal(w http.ResponseWriter) *snapshot.GlobalSnapshot {
	g, err := snapshot.ReadGlobal(s.globalPath())
	if err != nil {
		http.Error(w, `{"error":"snapshot not ready"}`, http.StatusServiceUnavailable)
		return nil
	}
	return g
}
func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	g := s.readGlobal(w)
	if g == nil {
		return
	}
	entries, _ := os.ReadDir(s.dir)
	var metrics []collector.Metric
	history := map[string][]float64{}
	var specs []collector.Metric
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "snapshot_") || !strings.HasSuffix(name, ".json") {
			continue
		}
		c, err := snapshot.ReadComp(filepath.Join(s.dir, name))
		if err != nil {
			continue
		}
		metrics = append(metrics, c.Metrics...)
		for k, v := range c.History {
			history[k] = v
		}
		specs = append(specs, c.Specs...)
	}
	specs = append(specs, g.SystemSpecs...)
	snap := &snapshot.Snapshot{SessionID: g.SessionID, Timestamp: time.Now(), RefreshInterval: g.RefreshInterval, HistoryPoints: historyPoints, Health: g.Health, Metrics: metrics, History: history, Specs: specs}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	_ = json.NewEncoder(w).Encode(snap)
}
func (s *Server) handleCollectors(w http.ResponseWriter, r *http.Request) {
	g := s.readGlobal(w)
	if g != nil {
		writeWebJSON(w, g.Collectors)
	}
}
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	g := s.readGlobal(w)
	if g == nil {
		return
	}
	writeWebJSON(w, map[string]any{"version": version.Version, "started_at": webStartup, "refresh_interval_ms": g.RefreshInterval, "history_points": historyPoints, "stress_operator": true})
}
func writeWebJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
