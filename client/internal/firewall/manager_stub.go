//go:build !windows

package firewall

import (
	"context"
	"fmt"

	"customvpn/client/internal/logging"
)

type Manager struct{}

func NewManager(_ *logging.Logger) *Manager {
	return &Manager{}
}

func (m *Manager) BlockDNSOnInterface(_ context.Context, _ string, _ []string, _ string) ([]string, error) {
	return nil, fmt.Errorf("firewall manager is only implemented on Windows")
}

func (m *Manager) RemoveRules(_ context.Context, _ []string) error {
	return nil
}

func (m *Manager) RemoveKillSwitchGroup(_ context.Context) error {
	return nil
}
