package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/sidun-av/kinoadaptarr/internal/cache"
	"github.com/sidun-av/kinoadaptarr/internal/kinopoisk"
	"github.com/sidun-av/kinoadaptarr/internal/proxy"
	"github.com/sidun-av/kinoadaptarr/internal/resolver"
	"github.com/sidun-av/kinoadaptarr/internal/tmdb"
)

func main() {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "/config.yml"
	}
	cfg, err := LoadConfig(configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	db, err := cache.Open(cfg.CachePath)
	if err != nil {
		log.Fatalf("open cache: %v", err)
	}
	defer db.Close()

	kp := kinopoisk.New(cfg.Kinopoisk.BaseURL, cfg.Kinopoisk.APIKey)
	tm := tmdb.New(cfg.TMDB.BaseURL, cfg.TMDB.APIKey)
	res := resolver.New(kp, tm, db)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", proxy.HealthzHandler)

	// Prowlarr syncs each of its indexers to Sonarr/Radarr as a separate
	// Torznab entry rather than one combined feed, so we register one route
	// per configured upstream. Sonarr/Radarr's Torznab client always
	// appends a literal "/api" segment to whatever base URL is configured
	// for an indexer (that's how the original direct-to-Prowlarr URLs
	// worked too: base "http://prowlarr:9696/1/" + "/api"), so routes here
	// are registered at /{name}/api to match — Sonarr's indexer URL should
	// be set to http://kinoadaptarr:<port>/{name} (no "/api" suffix; Sonarr
	// adds it itself).
	for name, upstreamURL := range cfg.Upstreams {
		handler := proxy.NewHandler(upstreamURL, res, http.DefaultClient)
		route := "/" + name + "/api"
		mux.Handle(route, handler)
		log.Printf("kinoadaptarr: registered %s -> %s", route, upstreamURL)
	}

	log.Printf("kinoadaptarr listening on :%d", cfg.Port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", cfg.Port), mux))
}
