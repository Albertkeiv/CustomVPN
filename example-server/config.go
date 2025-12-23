package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ServerConfig represents the server configuration
type ServerConfig struct {
	ListenAddr   string `yaml:"listen_addr"`
	UserLogin    string `yaml:"user_login"`
	UserPassword string `yaml:"user_password"`
	ServersDir   string `yaml:"servers_dir"`
	RoutesDir    string `yaml:"routes_dir"`
}

// LoadServerConfig loads the server configuration from server-config.yaml
func LoadServerConfig(configPath string) (*ServerConfig, error) {
	file, err := os.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	var config ServerConfig
	decoder := yaml.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to decode config: %w", err)
	}

	return &config, nil
}