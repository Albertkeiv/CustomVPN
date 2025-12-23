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

	// Load servers
	serverDTOs, err := LoadServers(config.ServersDir)
	if err != nil {
		log.Fatalf("Failed to load servers: %v", err)
	}

	// Load routes
	routeDTOs, err := LoadRoutes(config.RoutesDir)
	if err != nil {
		log.Fatalf("Failed to load routes: %v", err)
	}

	// Initialize storage
	InitStorage(config, serverDTOs, routeDTOs)

	// Start server
	StartServer(config)
}