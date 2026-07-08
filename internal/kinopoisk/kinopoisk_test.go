package kinopoisk

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSearchReturnsBestMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-API-KEY"); got != "test-key" {
			t.Errorf("expected X-API-KEY header, got %q", got)
		}
		if got := r.URL.Query().Get("query"); got != "Первая ракетка" {
			t.Errorf("expected query=Первая ракетка, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"docs": [
				{"name": "Первая ракетка", "externalId": {"tmdb": 123456, "imdb": "tt9999999"}}
			]
		}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key")
	m, err := c.Search("Первая ракетка")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m == nil {
		t.Fatal("expected a match, got nil")
	}
	if m.ExternalID.TMDB != 123456 {
		t.Errorf("expected tmdb id 123456, got %d", m.ExternalID.TMDB)
	}
	if m.ExternalID.IMDB != "tt9999999" {
		t.Errorf("expected imdb id tt9999999, got %q", m.ExternalID.IMDB)
	}
}

func TestSearchReturnsNilOnNoResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"docs": []}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key")
	m, err := c.Search("Nonexistent Show")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m != nil {
		t.Errorf("expected nil match for zero results, got %+v", m)
	}
}

func TestSearchReturnsErrorOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := New(srv.URL, "bad-key")
	if _, err := c.Search("anything"); err == nil {
		t.Fatal("expected an error for a non-200 response, got nil")
	}
}

func TestSearchErrorIncludesResponseBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"Дневной лимит запросов исчерпан"}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key")
	_, err := c.Search("anything")
	if err == nil {
		t.Fatal("expected an error for a 403 response, got nil")
	}
	if !strings.Contains(err.Error(), "Дневной лимит запросов исчерпан") {
		t.Errorf("expected error to include the response body for diagnosis, got: %v", err)
	}
}

func TestSearchStopsCallingKinopoiskAfterRateLimitResponse(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"daily limit exceeded"}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key")
	fakeNow := time.Now()
	c.now = func() time.Time { return fakeNow }

	if _, err := c.Search("first"); err == nil {
		t.Fatal("expected an error on the first (rate-limited) call")
	}
	if hits != 1 {
		t.Fatalf("expected 1 request to reach the server, got %d", hits)
	}

	if _, err := c.Search("second"); err == nil {
		t.Fatal("expected an error on the second call while still in the cooldown window")
	}
	if hits != 1 {
		t.Errorf("expected the second call to be short-circuited without hitting the server, got %d requests", hits)
	}
}

func TestSearchResumesAfterCooldownExpires(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"message":"daily limit exceeded"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"docs": [{"name": "Первая ракетка", "externalId": {"tmdb": 123456}}]}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key")
	fakeNow := time.Now()
	c.now = func() time.Time { return fakeNow }

	if _, err := c.Search("first"); err == nil {
		t.Fatal("expected an error on the first (rate-limited) call")
	}

	fakeNow = fakeNow.Add(rateLimitCooldown + time.Second)

	m, err := c.Search("second")
	if err != nil {
		t.Fatalf("expected the call after cooldown to reach the server again, got error: %v", err)
	}
	if m == nil || m.ExternalID.TMDB != 123456 {
		t.Errorf("expected a successful match after cooldown, got %+v", m)
	}
	if hits != 2 {
		t.Errorf("expected exactly 2 requests to reach the server, got %d", hits)
	}
}
