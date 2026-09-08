//go:build linux

package main

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebRoutesRemainAvailableWithoutStressController(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default(), nil, ":19322")
	index := httptest.NewRecorder()
	server.Routes().ServeHTTP(index, httptest.NewRequest(http.MethodGet, "/", nil))
	if index.Code != http.StatusOK {
		t.Fatalf("index status=%d body=%s", index.Code, index.Body.String())
	}
	stressResponse := httptest.NewRecorder()
	server.Routes().ServeHTTP(stressResponse, httptest.NewRequest(http.MethodGet, "/api/stress/config", nil))
	if stressResponse.Code != http.StatusNotFound {
		t.Fatalf("stress route without client status=%d, want 404", stressResponse.Code)
	}
}
