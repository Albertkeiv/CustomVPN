package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// LoadServers loads all server JSON files from the specified directory
func LoadServers(dir string) ([]ServerDTO, error) {
	var serverDTOs []ServerDTO

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("failed to open %s: %w", path, err)
		}
		defer file.Close()

		var dto ServerDTO
		if err := json.NewDecoder(file).Decode(&dto); err != nil {
			return fmt.Errorf("failed to decode %s: %w", path, err)
		}

		if err := validateServerDTO(dto); err != nil {
			return fmt.Errorf("invalid server DTO in %s: %w", path, err)
		}

		serverDTOs = append(serverDTOs, dto)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return serverDTOs, nil
}

// LoadRoutes loads all route JSON files from the specified directory
func LoadRoutes(dir string) ([]RouteProfileDTO, error) {
	var routeDTOs []RouteProfileDTO

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("failed to open %s: %w", path, err)
		}
		defer file.Close()

		var dto RouteProfileDTO
		if err := json.NewDecoder(file).Decode(&dto); err != nil {
			return fmt.Errorf("failed to decode %s: %w", path, err)
		}

		if err := validateRouteProfileDTO(dto); err != nil {
			return fmt.Errorf("invalid route DTO in %s: %w", path, err)
		}

		routeDTOs = append(routeDTOs, dto)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return routeDTOs, nil
}

// validateServerDTO performs basic validation on ServerDTO
func validateServerDTO(dto ServerDTO) error {
	if dto.ID == "" {
		return fmt.Errorf("id is required")
	}
	if dto.Name == "" {
		return fmt.Errorf("name is required")
	}
	if dto.Host == "" {
		return fmt.Errorf("host is required")
	}
	if dto.Port <= 0 || dto.Port > 65535 {
		return fmt.Errorf("invalid port: %d", dto.Port)
	}
	return nil
}

// validateRouteProfileDTO performs basic validation on RouteProfileDTO
func validateRouteProfileDTO(dto RouteProfileDTO) error {
	if dto.ID == "" {
		return fmt.Errorf("id is required")
	}
	if dto.Name == "" {
		return fmt.Errorf("name is required")
	}
	// Additional validation for CIDR can be added if needed
	return nil
}