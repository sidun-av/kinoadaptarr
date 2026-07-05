package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is kinoadaptarr's top-level configuration, loaded from a YAML file.
type Config struct {
	// Port the HTTP server listens on.
	Port int `yaml:"port"`

	// UpstreamURL is the full Prowlarr Torznab endpoint, including its own
	// apikey query parameter, e.g.
	// "http://prowlarr:9696/1/api?apikey=xxxx&t=search"
	UpstreamURL string `yaml:"upstream_url"`

	Kinopoisk KinopoiskConfig `yaml:"kinopoisk"`
	TMDB      TMDBConfig      `yaml:"tmdb"`

	// CachePath is the path to the SQLite cache database file.
	CachePath string `yaml:"cache_path"`
}

// KinopoiskConfig holds Kinopoisk API connection settings.
type KinopoiskConfig struct {
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
}

// TMDBConfig holds TMDB API connection settings.
type TMDBConfig struct {
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
}

// LoadConfig reads and validates the config file at path, applying defaults
// for optional fields and expanding ${VAR} environment references in
// URL/key fields (so secrets can be injected via environment variables
// rather than committed to the config file).
func LoadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg.UpstreamURL = os.Expand(cfg.UpstreamURL, os.Getenv)
	cfg.Kinopoisk.BaseURL = os.Expand(cfg.Kinopoisk.BaseURL, os.Getenv)
	cfg.Kinopoisk.APIKey = os.Expand(cfg.Kinopoisk.APIKey, os.Getenv)
	cfg.TMDB.BaseURL = os.Expand(cfg.TMDB.BaseURL, os.Getenv)
	cfg.TMDB.APIKey = os.Expand(cfg.TMDB.APIKey, os.Getenv)

	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	if cfg.Kinopoisk.BaseURL == "" {
		cfg.Kinopoisk.BaseURL = "https://api.kinopoisk.dev"
	}
	if cfg.TMDB.BaseURL == "" {
		cfg.TMDB.BaseURL = "https://api.themoviedb.org/3"
	}
	if cfg.CachePath == "" {
		cfg.CachePath = "/data/kinoadaptarr.db"
	}

	if cfg.UpstreamURL == "" {
		return nil, fmt.Errorf("upstream_url is required")
	}
	if cfg.Kinopoisk.APIKey == "" {
		return nil, fmt.Errorf("kinopoisk.api_key is required")
	}
	if cfg.TMDB.APIKey == "" {
		return nil, fmt.Errorf("tmdb.api_key is required")
	}

	return &cfg, nil
}
