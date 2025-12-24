package main

// ProfileDTO represents a combined profile DTO (server + routing).
type ProfileDTO struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	Country      string      `json:"country"`
	Host         string      `json:"host"`
	Port         int         `json:"port"`
	CoreConfig   interface{} `json:"core_config"`
	DirectRoutes []string    `json:"direct_routes"`
	TunnelRoutes []string    `json:"tunnel_routes"`
	KillSwitch  bool        `json:"kill_switch"`
}

// ProfileSummaryDTO represents a minimal profile list item.
type ProfileSummaryDTO struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Country string `json:"country"`
}
