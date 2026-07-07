package tmdb

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTVTitleReturnsName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("api_key"); got != "test-token" {
			t.Errorf("expected api_key query param, got %q", got)
		}
		if r.URL.Path != "/tv/123456" {
			t.Errorf("expected path /tv/123456, got %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"name": "Top Tennis Player"}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-token")
	title, err := c.TVTitle(123456, "en-US")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if title != "Top Tennis Player" {
		t.Errorf("expected 'Top Tennis Player', got %q", title)
	}
}

func TestTVTitlePassesLanguageParam(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("language"); got != "ru-RU" {
			t.Errorf("expected language=ru-RU, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"name": "Первая ракетка"}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-token")
	title, err := c.TVTitle(123456, "ru-RU")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if title != "Первая ракетка" {
		t.Errorf("expected 'Первая ракетка', got %q", title)
	}
}

func TestTVTitleErrorsOnEmptyName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"name": ""}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-token")
	if _, err := c.TVTitle(1, "en-US"); err == nil {
		t.Fatal("expected an error for an empty name, got nil")
	}
}

func TestTVTitleErrorsOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-token")
	if _, err := c.TVTitle(999, "en-US"); err == nil {
		t.Fatal("expected an error for a 404 response, got nil")
	}
}

func TestMovieTitleReturnsTitle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/movie/654321" {
			t.Errorf("expected path /movie/654321, got %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"title": "Some Movie", "name": "This field shouldn't be used for movies"}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-token")
	title, err := c.MovieTitle(654321)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if title != "Some Movie" {
		t.Errorf("expected 'Some Movie', got %q", title)
	}
}

func TestMovieTitleErrorsOnEmptyTitle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"title": ""}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-token")
	if _, err := c.MovieTitle(1); err == nil {
		t.Fatal("expected an error for an empty title, got nil")
	}
}

func TestMovieTitleErrorsOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-token")
	if _, err := c.MovieTitle(999); err == nil {
		t.Fatal("expected an error for a 404 response, got nil")
	}
}

func TestSearchTVReturnsFirstResultID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("query"); got != "Top Tennis Player" {
			t.Errorf("expected query=Top Tennis Player, got %q", got)
		}
		if got := r.URL.Query().Get("api_key"); got != "test-token" {
			t.Errorf("expected api_key query param, got %q", got)
		}
		if r.URL.Path != "/search/tv" {
			t.Errorf("expected path /search/tv, got %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results": [{"id": 999}, {"id": 111}]}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-token")
	id, err := c.SearchTV("Top Tennis Player")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 999 {
		t.Errorf("expected first result's id 999, got %d", id)
	}
}

func TestSearchTVReturnsZeroOnNoResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results": []}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-token")
	id, err := c.SearchTV("Some Unknown Show")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 0 {
		t.Errorf("expected id 0 on no results, got %d", id)
	}
}

func TestSearchTVErrorsOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-token")
	if _, err := c.SearchTV("anything"); err == nil {
		t.Fatal("expected an error for a 401 response, got nil")
	}
}
