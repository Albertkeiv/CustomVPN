package main

import (
	"log"
)

// In-memory storage
var (
	users   = make(map[string]*User)
	tokens  = make(map[string]*AuthToken)
	profiles = make(map[string]*Profile)
)

// InitStorage initializes the storage with config data
func InitStorage(config *ServerConfig, profileDTOs []ProfileDTO) {
	// Add user
	user := &User{
		ID:       config.UserLogin,
		Login:    config.UserLogin,
		Password: config.UserPassword,
	}
	users[user.Login] = user

	// Add profiles
	for _, dto := range profileDTOs {
		profile := &Profile{
			ID:           dto.ID,
			Name:         dto.Name,
			Country:      dto.Country,
			Host:         dto.Host,
			Port:         dto.Port,
			CoreConfig:   dto.CoreConfig,
			DirectRoutes: dto.DirectRoutes,
			TunnelRoutes: dto.TunnelRoutes,
			KillSwitch:  dto.KillSwitch,
		}
		profiles[profile.ID] = profile
	}

	log.Printf("Loaded %d users, %d profiles", len(users), len(profiles))
}
