// Package tmdb fetches a movie or TV series' canonical English title from
// TheMovieDB by ID (not by fuzzy text search, since TMDB's text search has
// known coverage gaps for niche non-English content — an ID lookup is
// reliable regardless of that).
package tmdb

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client fetches metadata from TMDB.
type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

// New returns a Client with a sane default timeout. baseURL should be the
// API root, e.g. "https://api.themoviedb.org/3" (no trailing slash).
func New(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL: baseURL,
		APIKey:  apiKey,
		HTTPClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// TVTitle returns the canonical English title (en-US locale) for the TV
// series with the given TMDB ID.
func (c *Client) TVTitle(tmdbID int) (string, error) {
	return c.titleField(fmt.Sprintf("%s/tv/%d?language=en-US", c.BaseURL, tmdbID), "name")
}

// MovieTitle returns the canonical English title (en-US locale) for the
// movie with the given TMDB ID.
func (c *Client) MovieTitle(tmdbID int) (string, error) {
	return c.titleField(fmt.Sprintf("%s/movie/%d?language=en-US", c.BaseURL, tmdbID), "title")
}

// titleField fetches u and extracts the named top-level JSON string field
// (TMDB names it "name" for TV series and "title" for movies).
func (c *Client) titleField(u, field string) (string, error) {
	// Uses the v3 "API Key" (api_key query param), not the v4 Read Access
	// Token (which would instead need an "Authorization: Bearer" header) —
	// v3 keys are what TMDB's settings page surfaces by default.
	sep := "?"
	if strings.Contains(u, "?") {
		sep = "&"
	}
	u += sep + "api_key=" + url.QueryEscape(c.APIKey)

	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("tmdb request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tmdb returned status %d", resp.StatusCode)
	}

	var parsed map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("decode tmdb response: %w", err)
	}

	raw, ok := parsed[field]
	if !ok {
		return "", fmt.Errorf("tmdb response had no %q field", field)
	}
	var title string
	if err := json.Unmarshal(raw, &title); err != nil {
		return "", fmt.Errorf("decode %q field: %w", field, err)
	}
	if title == "" {
		return "", fmt.Errorf("tmdb response had an empty %q field", field)
	}
	return title, nil
}
