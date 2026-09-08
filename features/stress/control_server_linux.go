//go:build linux

package stress

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type ControlServer struct {
	path     string
	listener net.Listener
	server   *http.Server
	logger   *slog.Logger
}

func ListenControl(path string, manager *Manager, logger *slog.Logger) (*ControlServer, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, errors.New("control socket must be an absolute path")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("refusing to replace non-socket control path: %s", path)
		}
		conn, dialErr := net.DialTimeout("unix", path, 250*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			return nil, fmt.Errorf("stress control socket is already active: %s", path)
		}
		if !errors.Is(dialErr, syscall.ECONNREFUSED) && !errors.Is(dialErr, syscall.ENOENT) {
			return nil, fmt.Errorf("probe existing stress control socket: %w", dialErr)
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o660); err != nil {
		listener.Close()
		os.Remove(path)
		return nil, err
	}
	return &ControlServer{
		path: path, listener: listener, logger: logger,
		server: &http.Server{Handler: NewControlHandler(manager, logger), ReadHeaderTimeout: 5 * time.Second},
	}, nil
}

func (s *ControlServer) Run(ctx context.Context) error {
	defer os.Remove(s.path)
	done := make(chan error, 1)
	go func() { done <- s.server.Serve(s.listener) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := s.server.Shutdown(shutdownCtx)
		return err
	}
}
