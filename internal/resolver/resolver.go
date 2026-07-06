// Package resolver ties together cyrillic detection, the Kinopoisk and TMDB
// clients, and the cache into a single "resolve this release title" call.
package resolver

import (
	"log"

	"github.com/sidun-av/kinoadaptarr/internal/cache"
	"github.com/sidun-av/kinoadaptarr/internal/cyrillic"
	"github.com/sidun-av/kinoadaptarr/internal/kinopoisk"
	"github.com/sidun-av/kinoadaptarr/internal/rewrite"
)

// MediaType distinguishes a TV series lookup from a movie lookup, since
// TMDB exposes them via different endpoints/fields ("name" vs "title").
type MediaType string

const (
	MediaTV    MediaType = "tv"
	MediaMovie MediaType = "movie"
)

// KinopoiskSearcher is satisfied by *kinopoisk.Client.
type KinopoiskSearcher interface {
	Search(title string) (*kinopoisk.Match, error)
}

// TMDBTitleFetcher is satisfied by *tmdb.Client.
type TMDBTitleFetcher interface {
	TVTitle(tmdbID int) (string, error)
	MovieTitle(tmdbID int) (string, error)
}

// Cache is satisfied by *cache.Cache.
type Cache interface {
	Get(key string) (*cache.Mapping, bool, error)
	Put(key string, m cache.Mapping) error
}

// Resolver resolves a raw release title's Cyrillic segment to its English
// equivalent, normalizing season/episode notation along the way.
type Resolver struct {
	Kinopoisk KinopoiskSearcher
	TMDB      TMDBTitleFetcher
	Cache     Cache
}

// New builds a Resolver from concrete clients.
func New(kp KinopoiskSearcher, tm TMDBTitleFetcher, c Cache) *Resolver {
	return &Resolver{Kinopoisk: kp, TMDB: tm, Cache: c}
}

// Resolve rewrites releaseTitle if it contains Cyrillic text and a mapping
// can be found (via cache, or a fresh Kinopoisk+TMDB lookup). mediaType
// selects which TMDB endpoint/field to resolve the title against. If no
// Cyrillic text is present, or no mapping can be resolved, releaseTitle is
// returned unchanged — this function never errors out the caller; lookup
// failures are logged and treated as a pass-through.
func (r *Resolver) Resolve(releaseTitle string, mediaType MediaType) string {
	if !cyrillic.HasCyrillic(releaseTitle) {
		return releaseTitle
	}

	segment := cyrillic.ExtractTitle(releaseTitle)
	if segment == "" {
		return releaseTitle
	}

	// Namespaced by media type: the same Cyrillic string could plausibly
	// refer to a differently-titled movie and TV series.
	cacheKey := string(mediaType) + ":" + segment

	if m, ok, err := r.Cache.Get(cacheKey); err != nil {
		log.Printf("resolver: cache lookup failed for %q: %v", cacheKey, err)
	} else if ok {
		return rewrite.Title(releaseTitle, segment, m.EnglishTitle)
	}

	kpMatch, err := r.Kinopoisk.Search(segment)
	if err != nil {
		log.Printf("resolver: kinopoisk search failed for %q: %v", segment, err)
		return releaseTitle
	}
	if kpMatch == nil || kpMatch.ExternalID.TMDB == 0 {
		log.Printf("resolver: no kinopoisk/tmdb match for %q", segment)
		return releaseTitle
	}

	var englishTitle string
	if mediaType == MediaMovie {
		englishTitle, err = r.TMDB.MovieTitle(kpMatch.ExternalID.TMDB)
	} else {
		englishTitle, err = r.TMDB.TVTitle(kpMatch.ExternalID.TMDB)
	}
	if err != nil {
		log.Printf("resolver: tmdb lookup failed for tmdb id %d (%q): %v", kpMatch.ExternalID.TMDB, segment, err)
		return releaseTitle
	}

	if err := r.Cache.Put(cacheKey, cache.Mapping{EnglishTitle: englishTitle, TMDBID: kpMatch.ExternalID.TMDB}); err != nil {
		log.Printf("resolver: failed to cache mapping for %q: %v", cacheKey, err)
	}

	return rewrite.Title(releaseTitle, segment, englishTitle)
}
