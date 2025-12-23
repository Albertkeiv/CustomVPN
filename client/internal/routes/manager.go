package routes

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"customvpn/client/internal/logging"
	"customvpn/client/internal/state"

	"golang.org/x/text/encoding/charmap"
)

// Manager управляет добавлением и удалением маршрутов через системную утилиту route.exe.
type Manager struct {
	logger   *logging.Logger
	routeExe string
}

// NewManager создаёт новый экземпляр менеджера маршрутов.
func NewManager(logger *logging.Logger) *Manager {
	return &Manager{
		logger:   logger,
		routeExe: "route.exe",
	}
}

// AddHostRoute добавляет host-маршрут до конкретного IPv4-адреса.
func (m *Manager) AddHostRoute(ctx context.Context, dest net.IP, gateway *state.GatewayInfo, kind state.RouteKind) (state.RouteRecord, error) {
	if dest == nil || dest.To4() == nil {
		return state.RouteRecord{}, fmt.Errorf("destination must be IPv4")
	}
	if gateway == nil || gateway.IP == "" {
		return state.RouteRecord{}, fmt.Errorf("gateway is not defined")
	}
	mask := "255.255.255.255"
	metric := gateway.Metric
	if metric <= 0 {
		metric = 1
	}
	args := []string{"ADD", dest.String(), "MASK", mask, gateway.IP, "METRIC", strconv.Itoa(metric)}
	if gateway.InterfaceIndex > 0 {
		args = append(args, "IF", strconv.Itoa(gateway.InterfaceIndex))
	}
	if err := m.runRouteCommand(ctx, args...); err != nil {
		return state.RouteRecord{}, err
	}
	record := state.RouteRecord{
		ID:             fmt.Sprintf("%s-%s-%d", kind, dest.String(), time.Now().UnixNano()),
		Destination:    fmt.Sprintf("%s/32", dest.String()),
		Gateway:        gateway.IP,
		InterfaceIndex: gateway.InterfaceIndex,
		Metric:         metric,
		Kind:           kind,
		CreatedAt:      time.Now(),
		Active:         true,
	}
	return record, nil
}

// AddCIDRRoute добавляет маршрут до подсети в формате CIDR.
func (m *Manager) AddCIDRRoute(ctx context.Context, cidr string, gateway *state.GatewayInfo, kind state.RouteKind) (state.RouteRecord, error) {
	if cidr == "" {
		return state.RouteRecord{}, fmt.Errorf("cidr is empty")
	}
	if gateway == nil || gateway.IP == "" {
		return state.RouteRecord{}, fmt.Errorf("gateway is not defined")
	}
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return state.RouteRecord{}, fmt.Errorf("parse cidr %s: %w", cidr, err)
	}
	ip := network.IP.To4()
	if ip == nil {
		return state.RouteRecord{}, fmt.Errorf("cidr %s is not IPv4", cidr)
	}
	mask, err := maskToIPv4String(network.Mask)
	if err != nil {
		return state.RouteRecord{}, err
	}
	metric := gatewayMetric(gateway)
	args := []string{"ADD", ip.String(), "MASK", mask, gateway.IP, "METRIC", strconv.Itoa(metric)}
	if gateway != nil && gateway.InterfaceIndex > 0 {
		args = append(args, "IF", strconv.Itoa(gateway.InterfaceIndex))
	}
	if err := m.runRouteCommand(ctx, args...); err != nil {
		return state.RouteRecord{}, err
	}
	record := state.RouteRecord{
		ID:             fmt.Sprintf("%s-%s-%d", kind, cidr, time.Now().UnixNano()),
		Destination:    cidr,
		Gateway:        gatewayIP(gateway),
		InterfaceIndex: gatewayInterface(gateway),
		Metric:         metric,
		Kind:           kind,
		CreatedAt:      time.Now(),
		Active:         true,
	}
	return record, nil
}

// RemoveRoute удаляет ранее добавленный маршрут.
func (m *Manager) RemoveRoute(ctx context.Context, record state.RouteRecord) error {
	destination := record.Destination
	if destination == "" {
		return fmt.Errorf("route destination is empty")
	}
	if (destination == "0.0.0.0" || destination == "0.0.0.0/0") && record.Gateway == "" {
		return fmt.Errorf("refusing to delete default route without gateway")
	}
	if idx := strings.Index(destination, "/"); idx != -1 {
		destination = destination[:idx]
	}
	args := []string{"DELETE", destination}
	if record.Destination != "" {
		if _, network, err := net.ParseCIDR(record.Destination); err == nil {
			if mask, err := maskToIPv4String(network.Mask); err == nil {
				args = append(args, "MASK", mask)
			}
		}
	}
	if record.Gateway != "" {
		args = append(args, record.Gateway)
	}
	if record.InterfaceIndex > 0 {
		args = append(args, "IF", strconv.Itoa(record.InterfaceIndex))
	}
	return m.runRouteCommand(ctx, args...)
}

func (m *Manager) runRouteCommand(ctx context.Context, args ...string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, m.routeExe, args...)
	applyRouteCommandAttributes(cmd)
	output, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(output))
	decoded := decodeOEMText(trimmed)
	if err != nil {
		if decoded != "" {
			return fmt.Errorf("route %s failed: %s", strings.Join(args, " "), decoded)
		}
		return fmt.Errorf("route %s failed: %w", strings.Join(args, " "), err)
	}
	if m.logger != nil && decoded != "" {
		m.logger.Debugf("route %s -> %s", strings.Join(args, " "), decoded)
	}
	return nil
}

func decodeOEMText(text string) string {
	if text == "" {
		return ""
	}
	decoded, err := charmap.CodePage866.NewDecoder().String(text)
	if err != nil {
		return text
	}
	return decoded
}

func maskToIPv4String(mask net.IPMask) (string, error) {
	if len(mask) != net.IPv4len {
		return "", fmt.Errorf("only IPv4 masks are supported")
	}
	return net.IP(mask).String(), nil
}

func gatewayMetric(info *state.GatewayInfo) int {
	if info == nil || info.Metric <= 0 {
		return 1
	}
	return info.Metric
}

func gatewayIP(info *state.GatewayInfo) string {
	if info == nil {
		return ""
	}
	return info.IP
}

func gatewayInterface(info *state.GatewayInfo) int {
	if info == nil {
		return 0
	}
	return info.InterfaceIndex
}
