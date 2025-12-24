package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// LoadProfiles loads all profile JSON files from the specified directory.
func LoadProfiles(dir string) ([]ProfileDTO, error) {
	var profileDTOs []ProfileDTO

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

		var dto ProfileDTO
		if err := json.NewDecoder(file).Decode(&dto); err != nil {
			return fmt.Errorf("failed to decode %s: %w", path, err)
		}

		if err := validateProfileDTO(dto); err != nil {
			return fmt.Errorf("invalid profile DTO in %s: %w", path, err)
		}

		profileDTOs = append(profileDTOs, dto)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return profileDTOs, nil
}

// validateProfileDTO performs basic validation on ProfileDTO.
func validateProfileDTO(dto ProfileDTO) error {
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
	// Additional validation for CIDR can be added if needed
	return nil
}
