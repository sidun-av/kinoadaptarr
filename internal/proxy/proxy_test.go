package proxy

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	resolverPkg "github.com/sidun-av/kinoadaptarr/internal/resolver"
	"github.com/sidun-av/kinoadaptarr/internal/torznab"
)

type fakeResolver struct {
	rewrites     map[string]string
	gotMediaType resolverPkg.MediaType
}

func (f *fakeResolver) Resolve(title string, mediaType resolverPkg.MediaType) string {
	f.gotMediaType = mediaType
	if rewritten, ok := f.rewrites[title]; ok {
		return rewritten
	}
	return title
}

const sampleUpstreamXML = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="1.0" xmlns:_xmlns="xmlns" _xmlns:atom="http://www.w3.org/2005/Atom" _xmlns:torznab="http://torznab.com/schemas/2015/feed">
  <channel>
    <title>Prowlarr</title>
    <item>
      <title>Первая ракетка / S1E1-8 of 8</title>
      <guid>abc123</guid>
      <link>http://prowlarr:9696/1/download?apikey=xxx&amp;link=abc</link>
      <enclosure url="magnet:?xt=urn:btih:abc" length="1" type="application/x-bittorrent"></enclosure>
      <torznab:attr name="seeders" value="42"></torznab:attr>
    </item>
    <item>
      <title>Alice Doesn&#39;t Live Here Anymore [1974]</title>
      <guid>def456</guid>
      <enclosure url="magnet:?xt=urn:btih:def" length="2" type="application/x-bittorrent"></enclosure>
    </item>
  </channel>
</rss>`

func TestServeHTTPRewritesTitleInPlacePreservingRestOfDocument(t *testing.T) {
	var gotQuery string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, sampleUpstreamXML)
	}))
	defer upstream.Close()

	fr := &fakeResolver{rewrites: map[string]string{
		"Первая ракетка / S1E1-8 of 8": "Top Tennis Player S1E1-8 of 8",
	}}
	h := NewHandler(upstream.URL+"?apikey=upstream-key", fr, nil)

	req := httptest.NewRequest(http.MethodGet, "/rutracker/api?t=tvsearch&q=top+tennis", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(gotQuery, "apikey=upstream-key") {
		t.Errorf("expected upstream apikey in forwarded query, got %q", gotQuery)
	}
	if !strings.Contains(gotQuery, "t=tvsearch") {
		t.Errorf("expected caller's query params forwarded, got %q", gotQuery)
	}

	got := rec.Body.String()
	if !strings.Contains(got, "<title>Top Tennis Player S1E1-8 of 8</title>") {
		t.Errorf("expected rewritten title in response body, got:\n%s", got)
	}
	if strings.Contains(got, "Первая ракетка") {
		t.Errorf("expected original Cyrillic title to be replaced, got:\n%s", got)
	}
	// The critical regression check: the namespace declarations and every
	// other byte of the document must survive completely untouched, since
	// we no longer re-marshal the XML (which previously corrupted them
	// into things like xmlns:_xmlns="xmlns").
	if !strings.Contains(got, `xmlns:_xmlns="xmlns" _xmlns:atom="http://www.w3.org/2005/Atom" _xmlns:torznab="http://torznab.com/schemas/2015/feed"`) {
		t.Errorf("expected the (verbatim, even if odd-looking) source namespace declarations preserved untouched, got:\n%s", got)
	}
	if !strings.Contains(got, `<torznab:attr name="seeders" value="42"></torznab:attr>`) {
		t.Errorf("expected torznab:attr elements preserved untouched, got:\n%s", got)
	}
	if !strings.Contains(got, `<link>http://prowlarr:9696/1/download?apikey=xxx&amp;link=abc</link>`) {
		t.Errorf("expected unrelated elements (link, still &amp;-escaped) preserved untouched, got:\n%s", got)
	}
	// The second item wasn't in the rewrite map, so it (including its
	// original entity escaping) must be left completely alone.
	if !strings.Contains(got, `<title>Alice Doesn&#39;t Live Here Anymore [1974]</title>`) {
		t.Errorf("expected untouched item's title to keep its original &#39; escaping, got:\n%s", got)
	}
	if fr.gotMediaType != resolverPkg.MediaTV {
		t.Errorf("expected t=tvsearch to map to MediaTV, got %q", fr.gotMediaType)
	}
}

func TestServeHTTPDetectsMovieMediaType(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, `<?xml version="1.0"?>
<rss version="2.0">
  <channel>
    <item><title>Какой-то Фильм 2024 WEBDL</title></item>
  </channel>
</rss>`)
	}))
	defer upstream.Close()

	fr := &fakeResolver{}
	h := NewHandler(upstream.URL, fr, nil)
	req := httptest.NewRequest(http.MethodGet, "/api?t=movie&q=some+film", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if fr.gotMediaType != resolverPkg.MediaMovie {
		t.Errorf("expected t=movie to map to MediaMovie, got %q", fr.gotMediaType)
	}
}

func TestServeHTTPPassesThroughUpstreamErrorStatus(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "bad api key")
	}))
	defer upstream.Close()

	h := NewHandler(upstream.URL, &fakeResolver{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 passed through, got %d", rec.Code)
	}
}

func TestServeHTTPPassesThroughNonXMLBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "not xml at all")
	}))
	defer upstream.Close()

	h := NewHandler(upstream.URL, &fakeResolver{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api?t=caps", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if rec.Body.String() != "not xml at all" {
		t.Errorf("expected non-XML body passed through unchanged, got %q", rec.Body.String())
	}
}

func TestHealthzHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	HealthzHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("expected body 'ok', got %q", rec.Body.String())
	}
}

func TestRewriteTitlesHandlesDuplicateTitlesPositionally(t *testing.T) {
	body := []byte(`<rss><channel>` +
		`<item><title>Дубликат</title></item>` +
		`<item><title>Дубликат</title></item>` +
		`</channel></rss>`)
	rss, err := torznab.Parse(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := 0
	fr := resolverFunc(func(title string, _ resolverPkg.MediaType) string {
		calls++
		if calls == 1 {
			return "First"
		}
		return "Second"
	})

	out := rewriteTitles(body, rss.Channel.Items, fr, resolverPkg.MediaTV)
	got := string(out)
	firstIdx := strings.Index(got, "<title>First</title>")
	secondIdx := strings.Index(got, "<title>Second</title>")
	if firstIdx == -1 || secondIdx == -1 || firstIdx >= secondIdx {
		t.Errorf("expected 'First' before 'Second' in rewritten output, got:\n%s", got)
	}
}

type resolverFunc func(title string, mediaType resolverPkg.MediaType) string

func (f resolverFunc) Resolve(title string, mediaType resolverPkg.MediaType) string {
	return f(title, mediaType)
}
