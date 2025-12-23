//go:build windows

package firewall

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"customvpn/client/internal/logging"
)

const (
	killSwitchGroup = "CustomVPN KillSwitch"
)

type Manager struct {
	logger *logging.Logger
}

func NewManager(logger *logging.Logger) *Manager {
	return &Manager{logger: logger}
}

func (m *Manager) BlockDNSOnInterface(ctx context.Context, iface string) ([]string, error) {
	if strings.TrimSpace(iface) == "" {
		return nil, fmt.Errorf("interface alias is empty")
	}
	rules := []struct {
		name     string
		protocol string
	}{
		{name: fmt.Sprintf("CustomVPN DNS Block (%s) UDP", iface), protocol: "UDP"},
		{name: fmt.Sprintf("CustomVPN DNS Block (%s) TCP", iface), protocol: "TCP"},
	}
	created := make([]string, 0, len(rules))
	for _, rule := range rules {
		safeName := escapeSingleQuotes(rule.name)
		script := fmt.Sprintf(
			"Get-NetFirewallRule -DisplayName '%s' -ErrorAction SilentlyContinue | Remove-NetFirewallRule -ErrorAction SilentlyContinue; "+
				"New-NetFirewallRule -DisplayName '%s' -Group '%s' -Direction Outbound -Action Block -Protocol %s -RemotePort 53 -InterfaceAlias '%s' -Profile Any | Out-Null",
			safeName,
			safeName,
			killSwitchGroup,
			rule.protocol,
			escapeSingleQuotes(iface),
		)
		if err := runPowerShell(ctx, script); err != nil {
			_ = m.RemoveRules(ctx, created)
			return created, err
		}
		created = append(created, rule.name)
	}
	return created, nil
}

func (m *Manager) RemoveRules(ctx context.Context, rules []string) error {
	for _, name := range rules {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		script := fmt.Sprintf(
			"Get-NetFirewallRule -DisplayName '%s' -ErrorAction SilentlyContinue | Remove-NetFirewallRule -ErrorAction SilentlyContinue",
			escapeSingleQuotes(name),
		)
		if err := runPowerShell(ctx, script); err != nil {
			return err
		}
	}
	return nil
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
