// Package proxy implements the Torznab-compatible HTTP endpoint that Sonarr
// or Radarr points at instead of Prowlarr directly: it forwards the request
// to the real Prowlarr instance, rewrites Cyrillic release titles in the
// response, and returns the modified feed.
package proxy

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/sidun-av/kinoadaptarr/internal/resolver"
	"github.com/sidun-av/kinoadaptarr/internal/torznab"
)

// itemTitleResolver is the narrow dependency rewriteTitles needs — just
// enough to resolve one item's title. TitleResolver (below) is a superset,
// satisfied by *resolver.Resolver.
type itemTitleResolver interface {
	Resolve(releaseTitle string, mediaType resolver.MediaType) string
}

// TitleResolver is satisfied by *resolver.Resolver.
type TitleResolver interface {
	itemTitleResolver
	// ResolveQuery translates an English TV search query to its Russian
	// title, for retrying a search that returned zero results. ok is false
	// if no translation could be found.
	ResolveQuery(englishQuery string) (russianTitle string, ok bool)
}

// Handler proxies Torznab requests to an upstream indexer aggregator
// (Prowlarr) and rewrites Cyrillic titles in the response.
type Handler struct {
	UpstreamURL string
	Resolver    TitleResolver
	HTTPClient  *http.Client
}

// NewHandler builds a Handler. upstreamURL is the full Prowlarr Torznab
// endpoint, including its own apikey query parameter — this proxy forwards
// the caller's query string verbatim alongside it.
func NewHandler(upstreamURL string, res TitleResolver, client *http.Client) *Handler {
	if client == nil {
		client = http.DefaultClient
	}
	return &Handler{UpstreamURL: upstreamURL, Resolver: res, HTTPClient: client}
}

// mediaTypeFromQuery maps a Torznab request's "t" parameter to a
// resolver.MediaType. Sonarr always searches with t=tvsearch, Radarr always
// with t=movie; anything else (e.g. a generic t=search or the t=caps
// capabilities probe) defaults to TV, which is harmless since those
// requests either return no Cyrillic titles or aren't real searches.
func mediaTypeFromQuery(q string) resolver.MediaType {
	if strings.Contains(q, "t=movie") {
		return resolver.MediaMovie
	}
	return resolver.MediaTV
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	upstream := h.UpstreamURL
	if r.URL.RawQuery != "" {
		sep := "?"
		if strings.Contains(upstream, "?") {
			sep = "&"
		}
		upstream += sep + r.URL.RawQuery
	}
	mediaType := mediaTypeFromQuery(r.URL.RawQuery)

	result, err := h.fetch(upstream)
	if err != nil {
		log.Printf("proxy: %v", err)
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		return
	}

	if result.statusCode != http.StatusOK {
		w.WriteHeader(result.statusCode)
		w.Write(result.body)
		return
	}

	rss, err := torznab.Parse(result.body)
	if err != nil {
		// Not parseable as Torznab XML (could be an error page, or a
		// caps/non-search request) — pass it through unchanged rather than
		// failing the whole request.
		w.Header().Set("Content-Type", result.header.Get("Content-Type"))
		w.Write(result.body)
		return
	}

	if len(rss.Channel.Items) == 0 {
		if retried, ok := h.retryWithReverseResolvedQuery(r, upstream); ok {
			result = retried.result
			rss = retried.rss
		}
	}

	out := rewriteTitles(result.body, rss.Channel.Items, h.Resolver, mediaType)

	w.Header().Set("Content-Type", result.header.Get("Content-Type"))
	w.Write(out)
}

// fetchResult holds the pieces of an upstream response ServeHTTP needs.
type fetchResult struct {
	body       []byte
	statusCode int
	header     http.Header
}

// fetch performs a GET against upstream and reads the full body. Errors
// are wrapped with distinct context (transport failure vs. a body-read
// failure after a successful connection) so callers' logs can tell them
// apart.
func (h *Handler) fetch(upstream string) (*fetchResult, error) {
	resp, err := h.HTTPClient.Get(upstream)
	if err != nil {
		return nil, fmt.Errorf("upstream request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read upstream body: %w", err)
	}
	return &fetchResult{body: body, statusCode: resp.StatusCode, header: resp.Header}, nil
}

// retriedFetch is the outcome of a successful reverse-query retry.
type retriedFetch struct {
	result *fetchResult
	rss    *torznab.RSS
}

// retryWithReverseResolvedQuery re-issues a t=tvsearch request with q
// translated to its Russian equivalent, for the case where the original
// (English) query found nothing. Returns ok=false if this isn't a
// tvsearch request, q is empty, no translation was found, the retry
// request itself failed, or the retry also came back with zero items —
// in every one of those cases the caller should keep using the original
// (empty) result.
func (h *Handler) retryWithReverseResolvedQuery(r *http.Request, upstream string) (*retriedFetch, bool) {
	if r.URL.Query().Get("t") != "tvsearch" {
		return nil, false
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		return nil, false
	}

	russianQuery, ok := h.Resolver.ResolveQuery(q)
	if !ok {
		return nil, false
	}

	retryURL, err := replaceQueryParam(upstream, "q", russianQuery)
	if err != nil {
		log.Printf("proxy: failed to build retry URL: %v", err)
		return nil, false
	}

	result, err := h.fetch(retryURL)
	if err != nil {
		log.Printf("proxy: retry: %v", err)
		return nil, false
	}
	if result.statusCode != http.StatusOK {
		return nil, false
	}

	rss, err := torznab.Parse(result.body)
	if err != nil || len(rss.Channel.Items) == 0 {
		return nil, false
	}

	return &retriedFetch{result: result, rss: rss}, true
}

// replaceQueryParam returns rawURL with the query parameter name set to
// value, preserving every other parameter and the base URL untouched.
func replaceQueryParam(rawURL, name, value string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set(name, value)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// rewriteTitles splices resolved English titles into body in place of each
// item's original <title> text, without re-serializing the surrounding XML
// (which Go's encoding/xml cannot do losslessly for namespace declarations
// — see the torznab package doc comment). Items whose title doesn't change,
// or whose exact original <title> text can't be located in body (should not
// happen for well-formed input, but defensively skipped rather than
// corrupting the response), are left untouched.
func rewriteTitles(body []byte, items []torznab.Item, res itemTitleResolver, mediaType resolver.MediaType) []byte {
	out := body
	cursor := 0
	for _, item := range items {
		original := item.Title()
		resolved := res.Resolve(original, mediaType)
		if resolved == original {
			continue
		}

		oldTag := []byte("<title>" + item.TitleRaw() + "</title>")
		idx := bytes.Index(out[cursor:], oldTag)
		if idx == -1 {
			continue
		}
		absIdx := cursor + idx

		var escaped bytes.Buffer
		xml.EscapeText(&escaped, []byte(resolved))
		newTag := []byte("<title>" + escaped.String() + "</title>")

		rebuilt := make([]byte, 0, len(out)-len(oldTag)+len(newTag))
		rebuilt = append(rebuilt, out[:absIdx]...)
		rebuilt = append(rebuilt, newTag...)
		rebuilt = append(rebuilt, out[absIdx+len(oldTag):]...)
		out = rebuilt

		cursor = absIdx + len(newTag)
	}
	return out
}

// HealthzHandler is a trivial liveness probe.
func HealthzHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "ok")
}
