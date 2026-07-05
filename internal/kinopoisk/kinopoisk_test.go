package kinopoisk

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
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
