//go:build windows

package firewall

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

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

func (m *Manager) BlockDNSOnInterface(ctx context.Context, iface string, allowed []string, program string) ([]string, error) {
	if strings.TrimSpace(iface) == "" {
		return nil, fmt.Errorf("interface alias is empty")
	}
	blockRules := []struct {
		name     string
		protocol string
	}{
		{name: fmt.Sprintf("CustomVPN DNS Block (%s) UDP", iface), protocol: "UDP"},
		{name: fmt.Sprintf("CustomVPN DNS Block (%s) TCP", iface), protocol: "TCP"},
	}
	created := make([]string, 0, len(blockRules))
	for _, rule := range blockRules {
		safeName := escapeSingleQuotes(rule.name)
		removeScript := fmt.Sprintf(
			"Get-NetFirewallRule -DisplayName '%s' -ErrorAction SilentlyContinue | Remove-NetFirewallRule -ErrorAction SilentlyContinue",
			safeName,
		)
		if err := runPowerShell(ctx, removeScript); err != nil {
			_ = m.RemoveRules(ctx, created)
			return created, err
		}
		createScript := fmt.Sprintf(
			"New-NetFirewallRule -DisplayName '%s' -Group '%s' -Direction Outbound -Action Block -Protocol %s -RemotePort 53 -InterfaceAlias '%s' -Profile Any | Out-Null",
			safeName,
			killSwitchGroup,
			rule.protocol,
			escapeSingleQuotes(iface),
		)
		if err := runPowerShell(ctx, createScript); err != nil {
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

func (m *Manager) RemoveKillSwitchGroup(ctx context.Context) error {
	script := fmt.Sprintf(
		"Get-NetFirewallRule -Group '%s' -ErrorAction SilentlyContinue | Remove-NetFirewallRule -ErrorAction SilentlyContinue",
		escapeSingleQuotes(killSwitchGroup),
	)
	return runPowerShell(ctx, script)
}

func runPowerShell(ctx context.Context, script string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	wrapped := fmt.Sprintf(
		"$ErrorActionPreference = 'Stop'\n"+
			"$script = @'\n%s\n'@\n"+
			"try { Invoke-Expression $script; Write-Output 'ok' } "+
			"catch { Write-Output 'error'; Write-Output ($_ | Out-String); "+
			"if ($_.Exception) { Write-Output $_.Exception.Message }; "+
			"if ($_.ErrorDetails -and $_.ErrorDetails.Message) { Write-Output $_.ErrorDetails.Message }; exit 1 }\n",
		script,
	)
	cmd := exec.CommandContext(ctx, resolvePowerShellPath(), "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", wrapped)
	applyCommandAttributes(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(decodePowerShellOutput(output))
		code := exitCode(err)
		meta := fmt.Sprintf("exit=%d", code)
		if trimmed != "" {
			return fmt.Errorf("powershell failed: %s (%s)", trimmed, meta)
		}
		return fmt.Errorf("powershell failed: %v (no output) (%s) script=%s", err, meta, truncateScript(script, 480))
	}
	return nil
}

func escapeSingleQuotes(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

func decodePowerShellOutput(output []byte) string {
	if len(output) == 0 {
		return ""
	}
	if len(output) >= 2 && (output[0] == 0xFF && output[1] == 0xFE) {
		return decodeUTF16(output[2:], true)
	}
	if len(output) >= 2 && (output[0] == 0xFE && output[1] == 0xFF) {
		return decodeUTF16(output[2:], false)
	}
	if looksLikeUTF16(output) {
		return decodeUTF16(output, true)
	}
	if utf8.Valid(output) {
		return string(output)
	}
	return string(output)
}

func decodeUTF16(output []byte, littleEndian bool) string {
	if len(output) < 2 {
		return ""
	}
	u16 := make([]uint16, 0, len(output)/2)
	for i := 0; i+1 < len(output); i += 2 {
		if littleEndian {
			u16 = append(u16, uint16(output[i])|uint16(output[i+1])<<8)
		} else {
			u16 = append(u16, uint16(output[i])<<8|uint16(output[i+1]))
		}
	}
	return strings.TrimRight(string(utf16.Decode(u16)), "\x00")
}

func looksLikeUTF16(output []byte) bool {
	if len(output) < 2 {
		return false
	}
	nulls := 0
	sample := len(output)
	if sample > 256 {
		sample = 256
	}
	for i := 1; i < sample; i += 2 {
		if output[i] == 0x00 {
			nulls++
		}
	}
	return nulls > 0 && nulls*2 >= (sample/2)
}

func exitCode(err error) int {
	var ee *exec.ExitError
	if err == nil {
		return 0
	}
	if errors.As(err, &ee) {
		if status, ok := ee.Sys().(interface{ ExitStatus() int }); ok {
			return status.ExitStatus()
		}
	}
	return -1
}

func truncateScript(script string, max int) string {
	script = strings.TrimSpace(script)
	if max <= 0 || len(script) <= max {
		return script
	}
	return script[:max] + "..."
}

func resolvePowerShellPath() string {
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		return "powershell.exe"
	}
	candidates := []string{
		filepath.Join(systemRoot, "Sysnative", "WindowsPowerShell", "v1.0", "powershell.exe"),
		filepath.Join(systemRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"),
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return "powershell.exe"
}

func normalizeRemoteAddresses(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if ip := net.ParseIP(value); ip != nil {
			result = append(result, ip.String())
			continue
		}
		if _, _, err := net.ParseCIDR(value); err == nil {
			result = append(result, value)
		}
	}
	return result
}

type allowRule struct {
	name          string
	protocol      string
	program       string
	remoteAddress string
}

func buildAllowRules(iface string, allowed []string, program string) []allowRule {
	allowed = normalizeRemoteAddresses(allowed)
	if len(allowed) == 0 {
		return nil
	}
	rules := make([]allowRule, 0, len(allowed)*2)
	for _, address := range allowed {
		suffix := strings.NewReplacer("/", "_", ":", "_").Replace(address)
		rules = append(rules, allowRule{
			name:          fmt.Sprintf("CustomVPN DNS Allow (%s) UDP %s", iface, suffix),
			protocol:      "UDP",
			program:       program,
			remoteAddress: address,
		})
		rules = append(rules, allowRule{
			name:          fmt.Sprintf("CustomVPN DNS Allow (%s) TCP %s", iface, suffix),
			protocol:      "TCP",
			program:       program,
			remoteAddress: address,
		})
	}
	return rules
}
