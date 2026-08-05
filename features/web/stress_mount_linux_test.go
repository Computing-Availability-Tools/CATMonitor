//go:build linux

package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
