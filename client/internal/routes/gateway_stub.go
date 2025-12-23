//go:build !windows

package routes

import (
	"fmt"
	"net"

	"customvpn/client/internal/state"
)

// DetectDefaultGateway возвращает ошибку на не-Windows платформах.
func DetectDefaultGateway() (*state.GatewayInfo, error) {
	return nil, fmt.Errorf("DetectDefaultGateway is only implemented on Windows")
}

func DetectGatewayForIP(_ net.IP) (*state.GatewayInfo, error) {
	return nil, fmt.Errorf("DetectGatewayForIP is only implemented on Windows")
}
