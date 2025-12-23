//go:build windows

package routes

import (
	"fmt"
	"net"
	"unsafe"

	"golang.org/x/sys/windows"

	"customvpn/client/internal/state"
)

const gaaFlagIncludeGateways = 0x0080

// DetectDefaultGateway ищет единственный маршрут по умолчанию (IPv4) на Windows.
func DetectDefaultGateway() (*state.GatewayInfo, error) {
	flags := uint32(gaaFlagIncludeGateways)
	var size uint32
	if err := windows.GetAdaptersAddresses(windows.AF_INET, flags, 0, nil, &size); err != windows.ERROR_BUFFER_OVERFLOW {
		return nil, fmt.Errorf("GetAdaptersAddresses sizing: %w", err)
	}
	buffer := make([]byte, size)
	addresses := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buffer[0]))
	if err := windows.GetAdaptersAddresses(windows.AF_INET, flags, 0, addresses, &size); err != nil {
		return nil, fmt.Errorf("GetAdaptersAddresses: %w", err)
	}
	var gateway *state.GatewayInfo
	for adapter := addresses; adapter != nil; adapter = adapter.Next {
		if adapter.OperStatus != windows.IfOperStatusUp {
			continue
		}
		for gw := adapter.FirstGatewayAddress; gw != nil; gw = gw.Next {
			raw := (*windows.RawSockaddrAny)(unsafe.Pointer(gw.Address.Sockaddr))
			if raw == nil {
				continue
			}
			if raw.Addr.Family != windows.AF_INET {
				continue
			}
			sa4 := (*windows.RawSockaddrInet4)(unsafe.Pointer(gw.Address.Sockaddr))
			ip := net.IP(sa4.Addr[:]).String()
			if ip == "0.0.0.0" {
				continue
			}
			info := &state.GatewayInfo{
				IP:             ip,
				InterfaceIndex: int(adapter.IfIndex),
				Metric:         int(adapter.Ipv4Metric),
			}
			if info.Metric <= 0 {
				info.Metric = 1
			}
			if gateway == nil {
				gateway = info
				continue
			}
			if gateway.IP != info.IP || gateway.InterfaceIndex != info.InterfaceIndex {
				return nil, fmt.Errorf("multiple default gateways detected")
			}
		}
	}
	if gateway == nil {
		return nil, fmt.Errorf("default gateway not found")
	}
	return gateway, nil
}

// DetectGatewayForIP находит интерфейс, через который доступен указанный IPv4 адрес.
func DetectGatewayForIP(ip net.IP) (*state.GatewayInfo, error) {
	if ip == nil || ip.To4() == nil {
		return nil, fmt.Errorf("target ip must be IPv4")
	}
	flags := uint32(gaaFlagIncludeGateways)
	var size uint32
	if err := windows.GetAdaptersAddresses(windows.AF_INET, flags, 0, nil, &size); err != windows.ERROR_BUFFER_OVERFLOW {
		return nil, fmt.Errorf("GetAdaptersAddresses sizing: %w", err)
	}
	buffer := make([]byte, size)
	addresses := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buffer[0]))
	if err := windows.GetAdaptersAddresses(windows.AF_INET, flags, 0, addresses, &size); err != nil {
		return nil, fmt.Errorf("GetAdaptersAddresses: %w", err)
	}
	var match *state.GatewayInfo
	for adapter := addresses; adapter != nil; adapter = adapter.Next {
		if adapter.OperStatus != windows.IfOperStatusUp {
			continue
		}
		for ua := adapter.FirstUnicastAddress; ua != nil; ua = ua.Next {
			raw := (*windows.RawSockaddrAny)(unsafe.Pointer(ua.Address.Sockaddr))
			if raw == nil || raw.Addr.Family != windows.AF_INET {
				continue
			}
			sa4 := (*windows.RawSockaddrInet4)(unsafe.Pointer(ua.Address.Sockaddr))
			addrIP := net.IP(sa4.Addr[:]).To4()
			if addrIP == nil {
				continue
			}
			prefix := int(ua.OnLinkPrefixLength)
			if prefix <= 0 || prefix > 32 {
				continue
			}
			mask := net.CIDRMask(prefix, 32)
			network := addrIP.Mask(mask)
			if !(&net.IPNet{IP: network, Mask: mask}).Contains(ip.To4()) {
				continue
			}
			info := &state.GatewayInfo{
				IP:             ip.String(),
				InterfaceIndex: int(adapter.IfIndex),
				Metric:         int(adapter.Ipv4Metric),
			}
			if info.Metric <= 0 {
				info.Metric = 1
			}
			if match == nil {
				match = info
				continue
			}
			if match.InterfaceIndex != info.InterfaceIndex {
				return nil, fmt.Errorf("multiple interfaces match target ip")
			}
		}
	}
	if match == nil {
		return nil, fmt.Errorf("no interface found for %s", ip.String())
	}
	return match, nil
}
