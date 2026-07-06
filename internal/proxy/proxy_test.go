package proxy

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	resolverPkg "github.com/sidun-av/kinoadaptarr/internal/resolver"
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

func TestServeHTTPRewritesTitlesAndForwardsQuery(t *testing.T) {
	var gotQuery string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, `<?xml version="1.0"?>
<rss version="2.0">
  <channel>
    <item>
      <title>Первая ракетка / S1E1-8 of 8</title>
      <enclosure url="magnet:?xt=urn:btih:abc" length="1" type="application/x-bittorrent"/>
    </item>
  </channel>
</rss>`)
	}))
	defer upstream.Close()

	resolver := &fakeResolver{rewrites: map[string]string{
		"Первая ракетка / S1E1-8 of 8": "Top Tennis Player S1E1-8 of 8",
	}}
	h := NewHandler(upstream.URL+"?apikey=upstream-key", resolver, nil)

	req := httptest.NewRequest(http.MethodGet, "/api?t=tvsearch&q=top+tennis", nil)
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
	if !strings.Contains(rec.Body.String(), "Top Tennis Player S1E1-8 of 8") {
		t.Errorf("expected rewritten title in response body, got:\n%s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "Первая ракетка") {
		t.Errorf("expected original Cyrillic title to be replaced, got:\n%s", rec.Body.String())
	}
	if resolver.gotMediaType != resolverPkg.MediaTV {
		t.Errorf("expected t=tvsearch to map to MediaTV, got %q", resolver.gotMediaType)
	}
}

func TestServeHTTPDetectsMovieMediaType(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, `<?xml version="1.0"?>
<rss version="2.0">
  <channel>
    <item>
      <title>Какой-то Фильм 2024 WEBDL</title>
    </item>
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

func TestMediaTypeFromQuery(t *testing.T) {
	cases := []struct {
		query string
		want  resolverPkg.MediaType
	}{
		{"t=movie&q=x", resolverPkg.MediaMovie},
		{"t=tvsearch&q=x", resolverPkg.MediaTV},
		{"t=search&q=x", resolverPkg.MediaTV},
		{"t=caps", resolverPkg.MediaTV},
	}
	for _, c := range cases {
		if got := mediaTypeFromQuery(c.query); got != c.want {
			t.Errorf("mediaTypeFromQuery(%q) = %q, want %q", c.query, got, c.want)
		}
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
