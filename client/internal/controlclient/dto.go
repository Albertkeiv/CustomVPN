package controlclient

import (
	"encoding/json"
	"fmt"
	"strings"

	"customvpn/client/internal/state"
)

// ProfileDTO matches /profiles/{id} response.
type ProfileDTO struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Country      string          `json:"country"`
	Host         string          `json:"host"`
	Port         int             `json:"port"`
	CoreConfig   json.RawMessage `json:"core_config"`
	DirectRoutes []string        `json:"direct_routes"`
	TunnelRoutes []string        `json:"tunnel_routes"`
	KillSwitch  bool            `json:"kill_switch"`
}

// ProfileSummaryDTO matches /sync/profiles response.
type ProfileSummaryDTO struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Country string `json:"country"`
}

// AuthRequest encodes /auth request body.
type AuthRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

// AuthResponse carries authToken.
type AuthResponse struct {
	AuthToken string `json:"authToken"`
}

// Validate converts DTO to state.Profile with basic validation.
func (dto ProfileDTO) Validate() (state.Profile, error) {
	if dto.ID == "" {
		return state.Profile{}, fmt.Errorf("profile id is empty")
	}
	if dto.Name == "" {
		return state.Profile{}, fmt.Errorf("profile %s: name is empty", dto.ID)
	}
	if dto.Host == "" {
		return state.Profile{}, fmt.Errorf("profile %s: host is empty", dto.ID)
	}
	if dto.Port <= 0 || dto.Port > 65535 {
		return state.Profile{}, fmt.Errorf("profile %s: invalid port %d", dto.ID, dto.Port)
	}
	return state.Profile{
		ID:            dto.ID,
		Name:          dto.Name,
		Country:       dto.Country,
		Host:          dto.Host,
		Port:          dto.Port,
		CoreConfigRaw: dto.CoreConfig,
		DirectRoutes:  normalizeCIDRs(dto.DirectRoutes),
		TunnelRoutes:  normalizeCIDRs(dto.TunnelRoutes),
		KillSwitchEnabled: dto.KillSwitch,
	}, nil
}

// Validate converts list item DTO to state.Profile summary.
func (dto ProfileSummaryDTO) Validate() (state.Profile, error) {
	if dto.ID == "" {
		return state.Profile{}, fmt.Errorf("profile id is empty")
	}
	if dto.Name == "" {
		return state.Profile{}, fmt.Errorf("profile %s: name is empty", dto.ID)
	}
	return state.Profile{
		ID:      dto.ID,
		Name:    dto.Name,
		Country: dto.Country,
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
