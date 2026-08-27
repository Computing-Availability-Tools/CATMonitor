package stress

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type ControlClient struct {
	socket string
	http   *http.Client
}

func NewControlClient(socket string) (*ControlClient, error) {
	if socket == "" || !strings.HasPrefix(socket, "/") {
		return nil, errors.New("control socket must be an absolute path")
	}
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, "unix", socket)
	}}
	return &ControlClient{socket: socket, http: &http.Client{Transport: transport}}, nil
}

func (c *ControlClient) Config(ctx context.Context) (ControlConfigView, error) {
	var result ControlConfigView
	err := c.request(ctx, http.MethodGet, "/stress/config", nil, &result, http.StatusOK)
	return result, err
}
func (c *ControlClient) Latest(ctx context.Context) (Report, error) {
	var result Report
	err := c.request(ctx, http.MethodGet, "/stress/latest", nil, &result, http.StatusOK)
	return result, err
}
func (c *ControlClient) History(ctx context.Context, limit int) ([]Report, error) {
	var result []Report
	path := "/stress/history"
	if limit > 0 {
		path += "?limit=" + strconv.Itoa(limit)
	}
	err := c.request(ctx, http.MethodGet, path, nil, &result, http.StatusOK)
	return result, err
}
func (c *ControlClient) Start(ctx context.Context, request ControlStartRequest) (Report, error) {
	var result Report
	err := c.request(ctx, http.MethodPost, "/stress/jobs", request, &result, http.StatusAccepted)
	return result, err
}
func (c *ControlClient) Job(ctx context.Context, id string) (Report, error) {
	var result Report
	err := c.request(ctx, http.MethodGet, "/stress/jobs/"+url.PathEscape(id), nil, &result, http.StatusOK)
	return result, err
}
func (c *ControlClient) Cancel(ctx context.Context, id string) error {
	var result map[string]bool
	return c.request(ctx, http.MethodPost, "/stress/jobs/"+url.PathEscape(id)+"/cancel", map[string]any{}, &result, http.StatusAccepted)
}

func (c *ControlClient) request(ctx context.Context, method, path string, body, result any, expected int) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://unix"+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("connect to CATMonitor control socket %s: %w", c.socket, err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, 1<<20)
	data, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if response.StatusCode != expected {
		var apiError struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &apiError) != nil || apiError.Error == "" {
			apiError.Error = strings.TrimSpace(string(data))
		}
		return &ControlAPIError{StatusCode: response.StatusCode, Message: apiError.Error}
	}
	if result == nil {
		return nil
	}
	if err := decodeStrictJSON(data, result); err != nil {
		return fmt.Errorf("decode control response: %w", err)
	}
	return nil
}
