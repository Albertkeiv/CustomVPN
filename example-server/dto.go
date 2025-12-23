package main

// ServerDTO represents a server data transfer object
type ServerDTO struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Country    string      `json:"country"`
	Host       string      `json:"host"`
	Port       int         `json:"port"`
	CoreConfig interface{} `json:"core_config"`
}

// RouteProfileDTO represents a route profile data transfer object
type RouteProfileDTO struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	DirectRoutes  []string `json:"direct_routes"`
	TunnelRoutes  []string `json:"tunnel_routes"`
}