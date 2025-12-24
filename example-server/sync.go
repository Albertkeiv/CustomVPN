package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

// syncProfilesListHandler handles GET /sync/profiles (list).
func syncProfilesListHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var profileDTOs []ProfileSummaryDTO
	for _, profile := range profiles {
		dto := ProfileSummaryDTO{
			ID:      profile.ID,
			Name:    profile.Name,
			Country: profile.Country,
		}
		profileDTOs = append(profileDTOs, dto)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(profileDTOs); err != nil {
		log.Printf("Failed to encode profiles: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// syncProfileHandler handles GET /profiles/{id}.
func syncProfileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/profiles/")
	id = strings.TrimSpace(id)
	if id == "" {
		http.Error(w, "profile id is required", http.StatusBadRequest)
		return
	}
	profile, ok := profiles[id]
	if !ok {
		http.Error(w, "profile not found", http.StatusNotFound)
		return
	}
	dto := ProfileDTO{
		ID:           profile.ID,
		Name:         profile.Name,
		Country:      profile.Country,
		Host:         profile.Host,
		Port:         profile.Port,
		CoreConfig:   profile.CoreConfig,
		DirectRoutes: profile.DirectRoutes,
		TunnelRoutes: profile.TunnelRoutes,
		KillSwitch:  profile.KillSwitch,
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(dto); err != nil {
		log.Printf("Failed to encode profile: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}
