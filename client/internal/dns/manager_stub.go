//go:build !windows

package dns

import (
	"context"
	"fmt"

	"customvpn/client/internal/logging"
)

type Manager struct{}

func NewManager(_ *logging.Logger) *Manager {
	return &Manager{}
}

func (m *Manager) SetInterfaceDNS(_ context.Context, _ string, _ []string) error {
	return fmt.Errorf("dns manager is only implemented on Windows")
}
