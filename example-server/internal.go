package main

// Profile represents an internal profile structure.
type Profile struct {
	ID           string
	Name         string
	Country      string
	Host         string
	Port         int
	CoreConfig   interface{}
	DirectRoutes []string
	TunnelRoutes []string
	KillSwitch  bool
}
