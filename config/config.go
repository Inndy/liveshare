package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
)

const DefaultConfigFile = "liveshare.json"

type Config struct {
	Hostname     string `json:"hostname,omitempty"`
	Listen       string `json:"listen,omitempty"`
	Port         int    `json:"port,omitempty"`
	CfToken      string `json:"cf_token,omitempty"`
	TokenFile    string `json:"token_file,omitempty"`
	MaxCacheSize int    `json:"max_cache_size,omitempty"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) Save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

type ClientConfig struct {
	Server string `json:"server,omitempty"`
}

func ClientConfigPath() string {
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "liveshare", "client.json")
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "liveshare", "client.json")
	default:
		dir := os.Getenv("XDG_CONFIG_HOME")
		if dir == "" {
			home, _ := os.UserHomeDir()
			dir = filepath.Join(home, ".config")
		}
		return filepath.Join(dir, "liveshare", "client.json")
	}
}

func LoadClientConfig() (*ClientConfig, error) {
	data, err := os.ReadFile(ClientConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &ClientConfig{}, nil
		}
		return nil, err
	}
	var cfg ClientConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *ClientConfig) Save() error {
	path := ClientConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

func (c *Config) ApplyDefaults() {
	if c.Port == 0 {
		c.Port = 8080
	}
	if c.Listen == "" {
		c.Listen = "localhost"
	}
	if c.TokenFile == "" {
		c.TokenFile = "tokens.txt"
	}
}
