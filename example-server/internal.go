package main

// Server represents an internal server structure
type Server struct {
	ID         string
	Name       string
	Country    string
	Host       string
	Port       int
	CoreConfig interface{}
}

// RouteProfile represents an internal route profile structure
type RouteProfile struct {
	ID           string
	Name         string
	DirectRoutes []string
	TunnelRoutes []string
}