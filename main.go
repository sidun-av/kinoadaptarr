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

	handler := proxy.NewHandler(cfg.UpstreamURL, res, http.DefaultClient)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", proxy.HealthzHandler)
	mux.Handle("/api", handler)

	log.Printf("kinoadaptarr listening on :%d, proxying to %s", cfg.Port, cfg.UpstreamURL)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", cfg.Port), mux))
}
