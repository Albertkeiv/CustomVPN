package main

import (
	"flag"
	"log"
)

func main() {
	configPath := flag.String("config", "server-config.yaml", "path to server config file")
	flag.Parse()

	log.Println("CustomVPN Example Server starting...")

	config, err := LoadServerConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Printf("Loaded config from %s", *configPath)

	// Load profiles
	profileDTOs, err := LoadProfiles(config.ProfilesDir)
	if err != nil {
		log.Fatalf("Failed to load profiles: %v", err)
	}

	// Initialize storage
	InitStorage(config, profileDTOs)

	// Start server
	StartServer(config)
}
