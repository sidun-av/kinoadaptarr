package tmdb

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTVTitleReturnsName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("expected Bearer auth header, got %q", got)
		}
		if r.URL.Path != "/tv/123456" {
			t.Errorf("expected path /tv/123456, got %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"name": "Top Tennis Player"}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-token")
	title, err := c.TVTitle(123456)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if title != "Top Tennis Player" {
		t.Errorf("expected 'Top Tennis Player', got %q", title)
	}
}

func TestTVTitleErrorsOnEmptyName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"name": ""}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-token")
	if _, err := c.TVTitle(1); err == nil {
		t.Fatal("expected an error for an empty name, got nil")
	}
}

func TestTVTitleErrorsOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-token")
	if _, err := c.TVTitle(999); err == nil {
		t.Fatal("expected an error for a 404 response, got nil")
	}
}
