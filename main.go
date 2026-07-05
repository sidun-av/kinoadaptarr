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

	// Prowlarr syncs each of its indexers to Sonarr as a separate Torznab
	// entry rather than one combined feed, so we register one route per
	// configured upstream — Sonarr's existing indexer URLs each get
	// repointed at http://kinoadaptarr:<port>/api/{name}.
	for name, upstreamURL := range cfg.Upstreams {
		handler := proxy.NewHandler(upstreamURL, res, http.DefaultClient)
		route := "/api/" + name
		mux.Handle(route, handler)
		log.Printf("kinoadaptarr: registered %s -> %s", route, upstreamURL)
	}

	log.Printf("kinoadaptarr listening on :%d", cfg.Port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", cfg.Port), mux))
}
