// Package kinopoisk queries the poiskkino.dev (formerly api.kinopoisk.dev)
// unofficial API to resolve a Russian-language title to its external TMDB
// ID, which can then be used to fetch the canonical English title.
package kinopoisk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// rateLimitCooldown is how long Search stops issuing real requests after a
// 403 response, before probing again. Kinopoisk's free tier enforces a
// daily quota with no reset time exposed to callers, so this can't wait
// exactly as long as the real reset — it's a fixed, conservative backoff
// that turns "every single lookup across every indexer poll re-hits an
// already-exhausted quota" into an occasional probe instead.
const rateLimitCooldown = 30 * time.Minute

// Client queries the Kinopoisk API for a title's external IDs.
type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client

	mu           sync.Mutex
	blockedUntil time.Time
	now          func() time.Time
}

// New returns a Client with a sane default timeout. baseURL should be the
// API root, e.g. "https://api.poiskkino.dev" (no trailing slash).
func New(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL: baseURL,
		APIKey:  apiKey,
		HTTPClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		now: time.Now,
	}
}

// Match is a single search result's identifying data.
type Match struct {
	Name       string
	ExternalID struct {
		TMDB int
		IMDB string
	}
}

type searchResponse struct {
	Docs []struct {
		Name       string `json:"name"`
		ExternalID struct {
			TMDB *int    `json:"tmdb"`
			IMDB *string `json:"imdb"`
		} `json:"externalId"`
	} `json:"docs"`
}

// Search queries the Kinopoisk API by title and returns the best (first)
// match. It returns (nil, nil) if the API returned zero results — that's
// treated as a lookup miss, not an error, by callers.
func (c *Client) Search(title string) (*Match, error) {
	if until, blocked := c.rateLimited(); blocked {
		return nil, fmt.Errorf("kinopoisk: backing off after a rate-limit response until %s", until.Format(time.RFC3339))
	}

	u := fmt.Sprintf("%s/v1.4/movie/search?query=%s&limit=1", c.BaseURL, url.QueryEscape(title))

	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-API-KEY", c.APIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kinopoisk request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		if resp.StatusCode == http.StatusForbidden {
			c.setBlockedUntil(c.now().Add(rateLimitCooldown))
		}
		return nil, fmt.Errorf("kinopoisk returned status %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}

	var parsed searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode kinopoisk response: %w", err)
	}

	if len(parsed.Docs) == 0 {
		return nil, nil
	}

	doc := parsed.Docs[0]
	m := &Match{Name: doc.Name}
	if doc.ExternalID.TMDB != nil {
		m.ExternalID.TMDB = *doc.ExternalID.TMDB
	}
	if doc.ExternalID.IMDB != nil {
		m.ExternalID.IMDB = *doc.ExternalID.IMDB
	}
	return m, nil
}

// rateLimited reports whether Search is still backing off from a prior
// 403 response, and until when.
func (c *Client) rateLimited() (until time.Time, blocked bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.now().Before(c.blockedUntil) {
		return c.blockedUntil, true
	}
	return time.Time{}, false
}

// setBlockedUntil records that Search should back off from real requests
// until until.
func (c *Client) setBlockedUntil(until time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.blockedUntil = until
}
