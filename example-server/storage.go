package main

import (
	"log"
)

// In-memory storage
var (
	users   = make(map[string]*User)
	tokens  = make(map[string]*AuthToken)
	servers = make(map[string]*Server)
	routes  = make(map[string]*RouteProfile)
)

// InitStorage initializes the storage with config data
func InitStorage(config *ServerConfig, serverDTOs []ServerDTO, routeDTOs []RouteProfileDTO) {
	// Add user
	user := &User{
		ID:       config.UserLogin,
		Login:    config.UserLogin,
		Password: config.UserPassword,
	}
	users[user.Login] = user

	// Add servers
	for _, dto := range serverDTOs {
		server := &Server{
			ID:         dto.ID,
			Name:       dto.Name,
			Country:    dto.Country,
			Host:       dto.Host,
			Port:       dto.Port,
			CoreConfig: dto.CoreConfig,
		}
		servers[server.ID] = server
	}

	// Add routes
	for _, dto := range routeDTOs {
		route := &RouteProfile{
			ID:           dto.ID,
			Name:         dto.Name,
			DirectRoutes: dto.DirectRoutes,
			TunnelRoutes: dto.TunnelRoutes,
		}
		routes[route.ID] = route
	}

	log.Printf("Loaded %d users, %d servers, %d routes", len(users), len(servers), len(routes))
}