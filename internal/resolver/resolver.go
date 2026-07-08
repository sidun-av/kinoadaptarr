// Package resolver ties together cyrillic detection, the Kinopoisk and TMDB
// clients, and the cache into a single "resolve this release title" call.
package resolver

import (
	"log"
	"strings"

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
	TVTitle(tmdbID int, language string) (string, error)
	MovieTitle(tmdbID int) (string, error)
	SearchTV(title string) (int, error)
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
		return rewrite.Title(releaseTitle, segment, m.ResolvedTitle)
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
		englishTitle, err = r.TMDB.TVTitle(kpMatch.ExternalID.TMDB, "en-US")
	}
	if err != nil {
		log.Printf("resolver: tmdb lookup failed for tmdb id %d (%q): %v", kpMatch.ExternalID.TMDB, segment, err)
		return releaseTitle
	}

	if err := r.Cache.Put(cacheKey, cache.Mapping{ResolvedTitle: englishTitle, TMDBID: kpMatch.ExternalID.TMDB}); err != nil {
		log.Printf("resolver: failed to cache mapping for %q: %v", cacheKey, err)
	}

	return rewrite.Title(releaseTitle, segment, englishTitle)
}

// ResolveQuery attempts to translate an English (Sonarr-supplied) TV
// series search query into its Russian title, for retrying a search that
// returned zero results against a Russian-language tracker. It tries TMDB
// first, then falls back to Kinopoisk — which, being Russian-content
// specialized, sometimes has very new or niche shows TMDB hasn't indexed
// yet. Returns ("", false) if no translation could be found — callers
// should treat that as "nothing to retry with", not an error.
func (r *Resolver) ResolveQuery(englishQuery string) (string, bool) {
	cacheKey := "revtv:" + strings.ToLower(strings.TrimSpace(englishQuery))

	if m, ok, err := r.Cache.Get(cacheKey); err != nil {
		log.Printf("resolver: reverse cache lookup failed for %q: %v", cacheKey, err)
	} else if ok {
		if m.ResolvedTitle == "" {
			// A cached negative result (deliberately stored below) — a
			// prior lookup determined there's nothing to retry with.
			return "", false
		}
		return m.ResolvedTitle, true
	}

	title, ok, tmdbErr := r.resolveQueryViaTMDB(englishQuery)
	if ok {
		r.cacheReverseTitle(cacheKey, title)
		return title, true
	}

	kpTitle, kpOk, kpErr := r.resolveQueryViaKinopoisk(englishQuery)
	if kpOk {
		r.cacheReverseTitle(cacheKey, kpTitle)
		return kpTitle, true
	}

	// Only cache a negative result once every source has deterministically
	// found nothing. A transient error (network/API outage) from either
	// source is never cached here, since a later attempt might still
	// succeed via that source.
	if tmdbErr == nil && kpErr == nil {
		r.cacheNegative(cacheKey, 0)
	}
	return "", false
}

// resolveQueryViaTMDB tries to translate englishQuery using TMDB's text
// search plus a ru-RU localized title lookup. err is non-nil only for a
// transient failure (network/API error) — a deterministic "nothing found"
// is reported as ok=false, err=nil, so ResolveQuery knows whether the miss
// is safe to cache.
func (r *Resolver) resolveQueryViaTMDB(englishQuery string) (title string, ok bool, err error) {
	tmdbID, searchErr := r.TMDB.SearchTV(englishQuery)
	if searchErr != nil {
		log.Printf("resolver: tmdb search failed for %q: %v", englishQuery, searchErr)
		return "", false, searchErr
	}
	if tmdbID == 0 {
		log.Printf("resolver: no tmdb match for query %q", englishQuery)
		return "", false, nil
	}

	russianTitle, ruErr := r.TMDB.TVTitle(tmdbID, "ru-RU")
	if ruErr != nil {
		log.Printf("resolver: tmdb ru-RU lookup failed for tmdb id %d (query %q): %v", tmdbID, englishQuery, ruErr)
		return "", false, ruErr
	}
	if !cyrillic.HasCyrillic(russianTitle) {
		// No Russian translation available — TMDB fell back to a
		// non-Russian (e.g. its own en-US/original) name instead. Checking
		// for Cyrillic content, rather than comparing against englishQuery
		// verbatim, correctly catches this even when TMDB's fallback text
		// differs from the exact query Sonarr sent.
		log.Printf("resolver: no russian translation for tmdb id %d (query %q)", tmdbID, englishQuery)
		return "", false, nil
	}
	return russianTitle, true, nil
}

// resolveQueryViaKinopoisk tries Kinopoisk as a second source when TMDB
// doesn't have the show. err is non-nil only for a transient failure, by
// the same convention as resolveQueryViaTMDB.
func (r *Resolver) resolveQueryViaKinopoisk(englishQuery string) (title string, ok bool, err error) {
	match, searchErr := r.Kinopoisk.Search(englishQuery)
	if searchErr != nil {
		log.Printf("resolver: kinopoisk reverse search failed for %q: %v", englishQuery, searchErr)
		return "", false, searchErr
	}
	if match == nil || match.Name == "" {
		log.Printf("resolver: no kinopoisk match for query %q", englishQuery)
		return "", false, nil
	}
	if !cyrillic.HasCyrillic(match.Name) {
		log.Printf("resolver: kinopoisk match for query %q has no russian name (%q)", englishQuery, match.Name)
		return "", false, nil
	}
	return match.Name, true, nil
}

// cacheReverseTitle stores a resolved reverse-query (English -> Russian)
// mapping.
func (r *Resolver) cacheReverseTitle(cacheKey, title string) {
	if err := r.Cache.Put(cacheKey, cache.Mapping{ResolvedTitle: title}); err != nil {
		log.Printf("resolver: failed to cache reverse mapping for %q: %v", cacheKey, err)
	}
}

// cacheNegative records that cacheKey deterministically has nothing to
// retry with (no TMDB match, or no Russian translation), so future calls
// skip straight to a cache hit instead of re-querying TMDB. Only called
// for deterministic misses — a transient error (network failure, TMDB
// outage) is never cached here, since it might succeed on a later
// attempt.
func (r *Resolver) cacheNegative(cacheKey string, tmdbID int) {
	if err := r.Cache.Put(cacheKey, cache.Mapping{ResolvedTitle: "", TMDBID: tmdbID}); err != nil {
		log.Printf("resolver: failed to cache negative reverse mapping for %q: %v", cacheKey, err)
	}
}
