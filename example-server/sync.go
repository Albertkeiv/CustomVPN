package main

import (
	"encoding/json"
	"log"
	"net/http"
)

// syncServersHandler handles GET /sync/servers
func syncServersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var serverDTOs []ServerDTO
	for _, server := range servers {
		dto := ServerDTO{
			ID:         server.ID,
			Name:       server.Name,
			Country:    server.Country,
			Host:       server.Host,
			Port:       server.Port,
			CoreConfig: server.CoreConfig,
		}
		serverDTOs = append(serverDTOs, dto)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(serverDTOs); err != nil {
		log.Printf("Failed to encode servers: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// syncRoutesHandler handles GET /sync/routes
func syncRoutesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var routeDTOs []RouteProfileDTO
	for _, route := range routes {
		dto := RouteProfileDTO{
			ID:           route.ID,
			Name:         route.Name,
			DirectRoutes: route.DirectRoutes,
			TunnelRoutes: route.TunnelRoutes,
		}
		routeDTOs = append(routeDTOs, dto)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(routeDTOs); err != nil {
		log.Printf("Failed to encode routes: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}