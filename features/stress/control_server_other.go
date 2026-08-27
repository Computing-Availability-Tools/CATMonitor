//go:build !linux

package stress

import (
	"context"
	"errors"
	"log/slog"
)

type ControlServer struct{}

func ListenControl(string, *Manager, *slog.Logger) (*ControlServer, error) {
	return nil, errors.New("stress control socket is supported on Linux only")
}
func (s *ControlServer) Run(context.Context) error {
	return errors.New("stress control socket is supported on Linux only")
}
