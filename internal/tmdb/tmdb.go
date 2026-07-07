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

// TVTitle returns the canonical title for the TV series with the given
// TMDB ID, localized to language (e.g. "en-US", "ru-RU").
func (c *Client) TVTitle(tmdbID int, language string) (string, error) {
	return c.titleField(fmt.Sprintf("%s/tv/%d?language=%s", c.BaseURL, tmdbID, url.QueryEscape(language)), "name")
}

// MovieTitle returns the canonical English title (en-US locale) for the
// movie with the given TMDB ID.
func (c *Client) MovieTitle(tmdbID int) (string, error) {
	return c.titleField(fmt.Sprintf("%s/movie/%d?language=en-US", c.BaseURL, tmdbID), "title")
}

// SearchTV searches TMDB for a TV series by title text and returns the
// first (best) match's TMDB ID. Returns (0, nil) if the search found no
// results, or if the top result's own name doesn't match title (a
// same-name-different-show mismatch is far more likely than a genuine
// need for fuzzy matching here, since title is always an exact
// TVDB/TMDB-registered series name, not free-form user input) — both
// cases are treated as a lookup miss, not an error, by callers.
func (c *Client) SearchTV(title string) (int, error) {
	u := fmt.Sprintf("%s/search/tv?query=%s&api_key=%s", c.BaseURL, url.QueryEscape(title), url.QueryEscape(c.APIKey))

	resp, err := c.doRequest(u)
	if err != nil {
		return 0, fmt.Errorf("tmdb search: %w", err)
	}
	defer resp.Body.Close()

	var parsed struct {
		Results []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return 0, fmt.Errorf("decode tmdb search response: %w", err)
	}

	if len(parsed.Results) == 0 {
		return 0, nil
	}
	top := parsed.Results[0]
	if !strings.EqualFold(strings.TrimSpace(top.Name), strings.TrimSpace(title)) {
		return 0, nil
	}
	return top.ID, nil
}

// doRequest issues an authenticated GET to u and returns the response if
// it succeeded with a 200 status — the caller is responsible for closing
// resp.Body. Uses the v3 "API Key" (api_key query param), not the v4 Read
// Access Token (which would instead need an "Authorization: Bearer"
// header) — v3 keys are what TMDB's settings page surfaces by default.
func (c *Client) doRequest(u string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("returned status %d", resp.StatusCode)
	}
	return resp, nil
}

// titleField fetches u and extracts the named top-level JSON string field
// (TMDB names it "name" for TV series and "title" for movies).
func (c *Client) titleField(u, field string) (string, error) {
	sep := "?"
	if strings.Contains(u, "?") {
		sep = "&"
	}
	u += sep + "api_key=" + url.QueryEscape(c.APIKey)

	resp, err := c.doRequest(u)
	if err != nil {
		return "", fmt.Errorf("tmdb request: %w", err)
	}
	defer resp.Body.Close()

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
