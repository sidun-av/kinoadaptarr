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

	// Upstreams maps a route name (used as /api/{name}) to the full Torznab
	// endpoint for one Prowlarr-synced indexer, including its own apikey
	// query parameter, e.g.
	// kinozal: "http://prowlarr:9696/1/api?apikey=xxxx"
	//
	// Don't bake a "t=..." param into this URL: Sonarr/Radarr always send
	// their own "t=" (caps, tvsearch, movie, search) on every request, and
	// ServeHTTP appends the caller's raw query string verbatim onto this
	// upstream URL — a hardcoded "t=search" here would produce a duplicate,
	// conflicting "t" parameter that Prowlarr rejects with a 404 for
	// anything other than plain search (e.g. the caps request Sonarr sends
	// when testing an indexer).
	//
	// Prowlarr syncs each of its indexers to Sonarr as a separate Torznab
	// entry (rather than one combined feed), so each one needs its own
	// route here — point each of Sonarr's existing indexer URLs at
	// http://kinoadaptarr:8080/api/{name} instead of directly at Prowlarr.
	Upstreams map[string]string `yaml:"upstreams"`

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

	for name, url := range cfg.Upstreams {
		cfg.Upstreams[name] = os.Expand(url, os.Getenv)
	}
	cfg.Kinopoisk.BaseURL = os.Expand(cfg.Kinopoisk.BaseURL, os.Getenv)
	cfg.Kinopoisk.APIKey = os.Expand(cfg.Kinopoisk.APIKey, os.Getenv)
	cfg.TMDB.BaseURL = os.Expand(cfg.TMDB.BaseURL, os.Getenv)
	cfg.TMDB.APIKey = os.Expand(cfg.TMDB.APIKey, os.Getenv)

	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	if cfg.Kinopoisk.BaseURL == "" {
		cfg.Kinopoisk.BaseURL = "https://api.poiskkino.dev"
	}
	if cfg.TMDB.BaseURL == "" {
		cfg.TMDB.BaseURL = "https://api.themoviedb.org/3"
	}
	if cfg.CachePath == "" {
		cfg.CachePath = "/data/kinoadaptarr.db"
	}

	if len(cfg.Upstreams) == 0 {
		return nil, fmt.Errorf("at least one entry under upstreams is required")
	}
	for name, url := range cfg.Upstreams {
		if url == "" {
			return nil, fmt.Errorf("upstreams.%s must not be empty", name)
		}
	}
	if cfg.Kinopoisk.APIKey == "" {
		return nil, fmt.Errorf("kinopoisk.api_key is required")
	}
	if cfg.TMDB.APIKey == "" {
		return nil, fmt.Errorf("tmdb.api_key is required")
	}

	return &cfg, nil
}
