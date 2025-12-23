package controlclient

import (
	"encoding/json"
	"fmt"
	"strings"

	"customvpn/client/internal/state"
)

// ServerDTO соответствует ответу /sync/servers.
type ServerDTO struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Country    string          `json:"country"`
	Host       string          `json:"host"`
	Port       int             `json:"port"`
	CoreConfig json.RawMessage `json:"core_config"`
}

// RouteProfileDTO соответствует ответу /sync/routes.
type RouteProfileDTO struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	DirectRoutes []string `json:"direct_routes"`
	TunnelRoutes []string `json:"tunnel_routes"`
}

// AuthRequest описывает тело запроса /auth.
type AuthRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

// AuthResponse содержит authToken.
type AuthResponse struct {
	AuthToken string `json:"authToken"`
}

// Validate преобразует DTO в бизнес-модель Server, выполняя проверки.
func (dto ServerDTO) Validate() (state.Server, error) {
	if dto.ID == "" {
		return state.Server{}, fmt.Errorf("server id is empty")
	}
	if dto.Name == "" {
		return state.Server{}, fmt.Errorf("server %s: name is empty", dto.ID)
	}
	if dto.Host == "" {
		return state.Server{}, fmt.Errorf("server %s: host is empty", dto.ID)
	}
	if dto.Port <= 0 || dto.Port > 65535 {
		return state.Server{}, fmt.Errorf("server %s: invalid port %d", dto.ID, dto.Port)
	}
	return state.Server{
		ID:            dto.ID,
		Name:          dto.Name,
		Country:       dto.Country,
		Host:          dto.Host,
		Port:          dto.Port,
		CoreConfigRaw: dto.CoreConfig,
	}, nil
}

// Validate преобразует DTO в RouteProfile.
func (dto RouteProfileDTO) Validate() (state.RouteProfile, error) {
	if dto.ID == "" {
		return state.RouteProfile{}, fmt.Errorf("route profile id is empty")
	}
	if dto.Name == "" {
		return state.RouteProfile{}, fmt.Errorf("route profile %s: name is empty", dto.ID)
	}
	return state.RouteProfile{
		ID:           dto.ID,
		Name:         dto.Name,
		DirectRoutes: normalizeCIDRs(dto.DirectRoutes),
		TunnelRoutes: normalizeCIDRs(dto.TunnelRoutes),
	}, nil
}

func normalizeCIDRs(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		result = append(result, value)
	}
	return result
}
