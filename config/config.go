package config

import (
	"encoding/json"
	"os"
)

const DefaultConfigFile = "liveshare.json"

type Config struct {
	Hostname  string `json:"hostname,omitempty"`
	Listen    string `json:"listen,omitempty"`
	Port      int    `json:"port,omitempty"`
	CfToken   string `json:"cf_token,omitempty"`
	TokenFile string `json:"token_file,omitempty"`
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
