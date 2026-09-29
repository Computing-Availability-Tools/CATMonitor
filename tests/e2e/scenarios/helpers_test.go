//go:build e2e

package scenarios

import (
	"io"
	"net"
	"net/http"
	"time"
)

// httpGet is a raw HTTP GET that returns the response (any status).
func httpGet(url string) (*http.Response, error) {
	return http.Get(url)
}

// netDial dials a TCP address and returns the connection.
func netDial(addr string) (net.Conn, error) {
	return net.DialTimeout("tcp", addr, 200*time.Millisecond)
}

// readAllBody reads and closes the response body, returning its content.
func readAllBody(resp *http.Response) string {
	data, _ := io.ReadAll(resp.Body)
	return string(data)
}
