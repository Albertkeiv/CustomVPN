//go:build windows

package dns

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"customvpn/client/internal/logging"
)

type Manager struct {
	logger *logging.Logger
}

func NewManager(logger *logging.Logger) *Manager {
	return &Manager{logger: logger}
}

func (m *Manager) SetInterfaceDNS(ctx context.Context, iface string, servers []string) error {
	if strings.TrimSpace(iface) == "" {
		return fmt.Errorf("interface alias is empty")
	}
	if len(servers) == 0 {
		return fmt.Errorf("dns servers are empty")
	}
	serverList := make([]string, 0, len(servers))
	for _, server := range servers {
		server = strings.TrimSpace(server)
		if server == "" {
			continue
		}
		serverList = append(serverList, fmt.Sprintf("'%s'", escapeSingleQuotes(server)))
	}
	if len(serverList) == 0 {
		return fmt.Errorf("dns servers are empty")
	}
	script := fmt.Sprintf(
		"Set-DnsClientServerAddress -InterfaceAlias '%s' -ServerAddresses @(%s) -ErrorAction Stop | Out-Null",
		escapeSingleQuotes(iface),
		strings.Join(serverList, ","),
	)
	return runPowerShell(ctx, script)
}

func runPowerShell(ctx context.Context, script string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	applyCommandAttributes(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed != "" {
			return fmt.Errorf("powershell failed: %s", trimmed)
		}
		return fmt.Errorf("powershell failed: %w", err)
	}
	return nil
}

func escapeSingleQuotes(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}
