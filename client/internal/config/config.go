package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrConfigFailed обозначает любую проблему с чтением или разбором config.yaml.
var ErrConfigFailed = errors.New("config: failed to load")

// Config описывает пользовательские настройки приложения и вычисляемые пути.
type Config struct {
	ControlServerURL string `yaml:"control_server_url"`
	CorePath         string `yaml:"core_path"`
	LogLevel         string `yaml:"log_level"`
	LogFile          string `yaml:"log_file"`

	AppDir      string `yaml:"-"`
	CoreLogFile string `yaml:"-"`
}

// Error содержит дополнительный контекст при неудачной загрузке конфигурации.
type Error struct {
	Path string
	Err  error
}

func (e *Error) Error() string {
	if e == nil {
		return ErrConfigFailed.Error()
	}
	return fmt.Sprintf("%v: %s: %v", ErrConfigFailed, e.Path, e.Err)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// DetectAppDir возвращает каталог, в котором находится исполняемый файл.
func DetectAppDir() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("detect executable: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exePath)
	if err == nil {
		exePath = resolved
	}
	return filepath.Dir(exePath), nil
}

// DefaultPath возвращает путь к config.yaml относительно каталога приложения.
func DefaultPath(appDir string) string {
	return filepath.Join(appDir, "config.yaml")
}

// Load читает и валидирует YAML конфигурации, применяя appDir ко всем относительным путям.
func Load(path string, appDir string) (*Config, error) {
	if path == "" {
		return nil, &Error{Path: path, Err: errors.New("config path is empty")}
	}
	if appDir == "" {
		return nil, &Error{Path: path, Err: errors.New("app directory is empty")}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &Error{Path: path, Err: err}
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, &Error{Path: path, Err: err}
	}
	cfg.AppDir = appDir
	cfg.LogLevel = normalizeLogLevel(cfg.LogLevel)
	cfg.applyAppDir()
	if err := cfg.validate(); err != nil {
		return nil, &Error{Path: path, Err: err}
	}
	if err := cfg.ensureLogDirectories(); err != nil {
		return nil, &Error{Path: path, Err: err}
	}
	return &cfg, nil
}

func (c *Config) applyAppDir() {
	if c.AppDir == "" {
		return
	}
	c.AppDir = filepath.Clean(c.AppDir)
	c.CorePath = makeAbsolute(c.CorePath, c.AppDir)
	c.LogFile = makeAbsolute(c.LogFile, c.AppDir)
	logsDir := filepath.Join(c.AppDir, "logs")
	c.CoreLogFile = filepath.Join(logsDir, "core.log")
}

func (c *Config) validate() error {
	switch {
	case c.ControlServerURL == "":
		return errors.New("control_server_url is required")
	case c.CorePath == "":
		return errors.New("core_path is required")
	case c.LogFile == "":
		return errors.New("log_file is required")
	case c.AppDir == "":
		return errors.New("app directory is unknown")
	}
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}
	if _, ok := allowedLevels[c.LogLevel]; !ok {
		return fmt.Errorf("unsupported log_level %q", c.LogLevel)
	}
	return nil
}

func (c *Config) ensureLogDirectories() error {
	paths := []string{filepath.Dir(c.LogFile), filepath.Dir(c.CoreLogFile)}
	for _, dir := range paths {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create log directory %s: %w", dir, err)
		}
	}
	return nil
}

func makeAbsolute(path string, base string) string {
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	if base == "" {
		return filepath.Clean(path)
	}
	return filepath.Join(base, path)
}

func normalizeLogLevel(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "info"
	}
	return value
}

var allowedLevels = map[string]struct{}{
	"debug": {},
	"info":  {},
	"error": {},
}
