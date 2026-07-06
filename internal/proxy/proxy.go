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
	"strings"

	"github.com/sidun-av/kinoadaptarr/internal/resolver"
	"github.com/sidun-av/kinoadaptarr/internal/torznab"
)

// TitleResolver is satisfied by *resolver.Resolver.
type TitleResolver interface {
	Resolve(releaseTitle string, mediaType resolver.MediaType) string
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

	resp, err := h.HTTPClient.Get(upstream)
	if err != nil {
		log.Printf("proxy: upstream request failed: %v", err)
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("proxy: failed to read upstream body: %v", err)
		http.Error(w, "failed to read upstream response", http.StatusBadGateway)
		return
	}

	if resp.StatusCode != http.StatusOK {
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return
	}

	rss, err := torznab.Parse(body)
	if err != nil {
		// Not parseable as Torznab XML (could be an error page, or a
		// caps/non-search request) — pass it through unchanged rather than
		// failing the whole request.
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		w.Write(body)
		return
	}

	out := rewriteTitles(body, rss.Channel.Items, h.Resolver, mediaType)

	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.Write(out)
}

// rewriteTitles splices resolved English titles into body in place of each
// item's original <title> text, without re-serializing the surrounding XML
// (which Go's encoding/xml cannot do losslessly for namespace declarations
// — see the torznab package doc comment). Items whose title doesn't change,
// or whose exact original <title> text can't be located in body (should not
// happen for well-formed input, but defensively skipped rather than
// corrupting the response), are left untouched.
func rewriteTitles(body []byte, items []torznab.Item, res TitleResolver, mediaType resolver.MediaType) []byte {
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
