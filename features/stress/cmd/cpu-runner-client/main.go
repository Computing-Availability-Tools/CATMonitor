package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

func main() {
	socket := flag.String("socket", "/run/catmonitor-stress/cpu-runner.sock", "absolute Unix socket path")
	flag.Parse()
	if !filepath.IsAbs(*socket) {
		fatal("socket path must be absolute")
	}
	if flag.NArg() != 2 {
		fatal("usage: catmonitor-stress-cpu-client [-socket PATH] describe|run stream|hpl|hpcg")
	}
	action, benchmark := flag.Arg(0), flag.Arg(1)
	if !supported(benchmark) {
		fatal("unsupported CPU benchmark: %s", benchmark)
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", *socket)
		},
		DisableKeepAlives: true,
	}
	client := &http.Client{Transport: transport}
	defer transport.CloseIdleConnections()
	var request *http.Request
	var err error
	switch action {
	case "describe":
		request, err = http.NewRequest(http.MethodGet, "http://cpu-runner/v1/benchmarks/"+url.PathEscape(benchmark), nil)
	case "run":
		body, marshalErr := json.Marshal(map[string]string{"benchmark": benchmark})
		if marshalErr != nil {
			fatal("encode request: %v", marshalErr)
		}
		request, err = http.NewRequest(http.MethodPost, "http://cpu-runner/v1/runs", bytes.NewReader(body))
		if err == nil {
			request.Header.Set("Content-Type", "application/json")
		}
	default:
		fatal("unsupported action: %s", action)
	}
	if err != nil {
		fatal("create request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		fatal("CPU runner is unavailable at %s: %v", *socket, err)
	}
	defer response.Body.Close()
	output, err := io.ReadAll(io.LimitReader(response.Body, (16<<20)+1))
	if err != nil {
		fatal("read CPU runner response: %v", err)
	}
	if len(output) > 16<<20 {
		fatal("CPU runner response exceeded 16 MiB limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = os.Stderr.Write(output)
		if len(output) == 0 || output[len(output)-1] != '\n' {
			_, _ = fmt.Fprintln(os.Stderr)
		}
		fatal("CPU runner returned HTTP %s", response.Status)
	}
	if _, err := os.Stdout.Write(output); err != nil {
		fatal("write response: %v", err)
	}
}

func supported(name string) bool {
	switch name {
	case "stream", "hpl", "hpcg":
		return true
	default:
		return false
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(1)
}
