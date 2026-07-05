package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempConfig(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

func TestLoadConfigAppliesDefaults(t *testing.T) {
	path := writeTempConfig(t, `
upstream_url: "http://prowlarr:9696/1/api?apikey=abc&t=search"
kinopoisk:
  api_key: kp-key
tmdb:
  api_key: tmdb-key
`)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Port)
	}
	if cfg.Kinopoisk.BaseURL != "https://api.kinopoisk.dev" {
		t.Errorf("unexpected kinopoisk base url: %q", cfg.Kinopoisk.BaseURL)
	}
	if cfg.TMDB.BaseURL != "https://api.themoviedb.org/3" {
		t.Errorf("unexpected tmdb base url: %q", cfg.TMDB.BaseURL)
	}
	if cfg.CachePath != "/data/kinoadaptarr.db" {
		t.Errorf("unexpected default cache path: %q", cfg.CachePath)
	}
}

func TestLoadConfigExpandsEnvVars(t *testing.T) {
	t.Setenv("TEST_KP_KEY", "secret-kp-key")
	t.Setenv("TEST_TMDB_KEY", "secret-tmdb-key")

	path := writeTempConfig(t, `
upstream_url: "http://prowlarr:9696/1/api?apikey=abc&t=search"
kinopoisk:
  api_key: ${TEST_KP_KEY}
tmdb:
  api_key: ${TEST_TMDB_KEY}
`)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Kinopoisk.APIKey != "secret-kp-key" {
		t.Errorf("expected expanded kinopoisk key, got %q", cfg.Kinopoisk.APIKey)
	}
	if cfg.TMDB.APIKey != "secret-tmdb-key" {
		t.Errorf("expected expanded tmdb key, got %q", cfg.TMDB.APIKey)
	}
}

func TestLoadConfigRequiresUpstreamURL(t *testing.T) {
	path := writeTempConfig(t, `
kinopoisk:
  api_key: kp-key
tmdb:
  api_key: tmdb-key
`)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected error for missing upstream_url, got nil")
	}
}

func TestLoadConfigRequiresKinopoiskKey(t *testing.T) {
	path := writeTempConfig(t, `
upstream_url: "http://prowlarr:9696/1/api?apikey=abc&t=search"
tmdb:
  api_key: tmdb-key
`)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected error for missing kinopoisk.api_key, got nil")
	}
}

func TestLoadConfigRequiresTMDBKey(t *testing.T) {
	path := writeTempConfig(t, `
upstream_url: "http://prowlarr:9696/1/api?apikey=abc&t=search"
kinopoisk:
  api_key: kp-key
`)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected error for missing tmdb.api_key, got nil")
	}
}
