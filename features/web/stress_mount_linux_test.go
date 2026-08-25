//go:build linux

package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Computing-Availability-Tools/CATMonitor/features/stress"
)

func TestStressFeatureMountsWithoutRestoringWebCollection(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager := stress.NewManagerWithLogger(stress.Config{}, logger)
	srv := NewServer(t.TempDir(), logger, manager, "127.0.0.1:9527")
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	response, err := ts.Client().Get(ts.URL + "/stress/")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("stress page status=%d", response.StatusCode)
	}

	response, err = ts.Client().Get(ts.URL + "/api/stress/config")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["feature_enabled"] != false {
		t.Fatalf("disabled stress config=%v", body)
	}

	app, err := staticFiles.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(app), "aStress.href = '/stress/'") {
		t.Fatal("health dashboard is missing the independent stress navigation link")
	}
}

func TestWebStressConfigPreservesSnapshotOnlyStartup(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	cfg, wasMissing, err := loadWebStressConfig(missing, true)
	if err != nil || !wasMissing || cfg.Enabled {
		t.Fatalf("optional missing config should disable only stress: cfg=%+v missing=%v err=%v", cfg, wasMissing, err)
	}
	if _, _, err := loadWebStressConfig(missing, false); err == nil {
		t.Fatal("explicit missing config must fail")
	}

	path := filepath.Join(t.TempDir(), "catmonitor.yaml")
	if err := os.WriteFile(path, []byte("stress:\n  enabled: true\n  web_enabled: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, wasMissing, err = loadWebStressConfig(path, true)
	if err != nil || wasMissing || !cfg.Enabled || !cfg.WebEnabled {
		t.Fatalf("existing config was not loaded: cfg=%+v missing=%v err=%v", cfg, wasMissing, err)
	}

	if err := os.WriteFile(path, []byte("stress: [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadWebStressConfig(path, true); err == nil {
		t.Fatal("invalid existing config must fail")
	}
}
