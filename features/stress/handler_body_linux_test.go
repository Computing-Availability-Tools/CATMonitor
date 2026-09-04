//go:build linux

package stress

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOperatorRejectsMultipleJSONValues(t *testing.T) {
	manager := testManager(t, &fakeExecutor{})
	client, _ := startControlFixture(t, manager)
	mux := http.NewServeMux()
	Register(mux, client, ":19322", nil)

	request := httptest.NewRequest(http.MethodPost, "/api/stress/runs",
		bytes.NewBufferString(`{"benchmarks":["stream"]} {"benchmarks":["hpl"]}`))
	request.RemoteAddr = "127.0.0.1:41000"
	request.Host = "node.example:19322"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CATMonitor-Action", actionHeader)
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
