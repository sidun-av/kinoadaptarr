# Reverse Query Resolution (TV Search Fallback) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When a Sonarr `t=tvsearch` request comes back from the upstream indexer with zero results, retry it once with the query's English series title translated to Russian via TMDB, so Russian-original series (whose only TVDB-registered title is an English translation never present anywhere in the tracker's own release titles) can actually be found by search.

**Architecture:** A new `Resolver.ResolveQuery` method (TMDB-only: text search by the English title to get a TMDB id, then fetch that id's `ru-RU` localized title) lives alongside the existing forward `Resolve` method in `internal/resolver`. `proxy.Handler.ServeHTTP` calls it exactly once, only when the first upstream response parsed successfully but contained zero items and the request was a `t=tvsearch` with a non-empty `q`. Every failure mode (no TMDB match, retry itself still empty) falls through silently to today's behavior — an empty response to Sonarr — so this can only turn some zero-result searches into successful ones, never make anything worse.

**Tech Stack:** Go 1.25, stdlib `net/http`/`net/url`/`encoding/json`, existing `internal/tmdb`, `internal/resolver`, `internal/cache`, `internal/proxy` packages.

## Global Constraints

- TV search only (`t=tvsearch`). Movie search (`t=movie`) is explicitly out of scope for this plan.
- Only triggers as a fallback on zero upstream results — the original request is always sent unchanged first.
- No Kinopoisk calls in the reverse direction — TMDB only (see spec's rate-limit rationale).
- No new config fields; reuses the already-configured TMDB client/key and the existing SQLite cache file (new key namespace, no schema change).
- Every task must leave the repository fully buildable and green: `go build ./... && go test ./... && go vet ./... && gofmt -l .` (the last one must print nothing).

Full design background: `docs/superpowers/specs/2026-07-07-reverse-query-resolution-design.md`.

---

### Task 1: Rename `cache.Mapping.EnglishTitle` to `ResolvedTitle`

The cache stores resolved-title mappings in both directions once this feature lands; a reverse-direction row would hold a Russian title in a field literally named `EnglishTitle`, which is actively misleading. Rename first, as a pure refactor, so later tasks build on the correct name.

**Files:**
- Modify: `internal/cache/cache.go`
- Modify: `internal/cache/cache_test.go`
- Modify: `internal/resolver/resolver.go`

**Interfaces:**
- Produces: `cache.Mapping{ResolvedTitle string, TMDBID int}` — consumed by Task 3.

- [ ] **Step 1: Confirm the baseline is green**

Run: `go build ./... && go test ./...`
Expected: all packages build, all tests `ok`.

- [ ] **Step 2: Rename the field in `internal/cache/cache.go`**

Replace:
```go
// Mapping is a resolved title translation.
type Mapping struct {
	EnglishTitle string
	TMDBID       int
}
```
with:
```go
// Mapping is a resolved title translation. EnglishTitle holds the
// canonical English title for forward (Cyrillic -> English) lookups, or
// the Russian title for reverse (English query -> Russian) lookups — the
// field name reflects the more common forward direction.
type Mapping struct {
	ResolvedTitle string
	TMDBID        int
}
```

Then update the two SQL-backed methods further down the same file. Replace:
```go
// Get returns the cached mapping for key, if one exists.
func (c *Cache) Get(key string) (*Mapping, bool, error) {
	var m Mapping
	err := c.db.QueryRow(
		`SELECT english_title, tmdb_id FROM title_mappings WHERE cyrillic_key = ?`,
		key,
	).Scan(&m.EnglishTitle, &m.TMDBID)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("query mapping: %w", err)
	}
	return &m, true, nil
}

// Put stores (or overwrites) the mapping for key.
func (c *Cache) Put(key string, m Mapping) error {
	_, err := c.db.Exec(`
		INSERT INTO title_mappings (cyrillic_key, english_title, tmdb_id)
		VALUES (?, ?, ?)
		ON CONFLICT(cyrillic_key) DO UPDATE SET
			english_title = excluded.english_title,
			tmdb_id = excluded.tmdb_id,
			resolved_at = CURRENT_TIMESTAMP
	`, key, m.EnglishTitle, m.TMDBID)
	if err != nil {
		return fmt.Errorf("insert mapping: %w", err)
	}
	return nil
}
```
with:
```go
// Get returns the cached mapping for key, if one exists.
func (c *Cache) Get(key string) (*Mapping, bool, error) {
	var m Mapping
	err := c.db.QueryRow(
		`SELECT english_title, tmdb_id FROM title_mappings WHERE cyrillic_key = ?`,
		key,
	).Scan(&m.ResolvedTitle, &m.TMDBID)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("query mapping: %w", err)
	}
	return &m, true, nil
}

// Put stores (or overwrites) the mapping for key.
func (c *Cache) Put(key string, m Mapping) error {
	_, err := c.db.Exec(`
		INSERT INTO title_mappings (cyrillic_key, english_title, tmdb_id)
		VALUES (?, ?, ?)
		ON CONFLICT(cyrillic_key) DO UPDATE SET
			english_title = excluded.english_title,
			tmdb_id = excluded.tmdb_id,
			resolved_at = CURRENT_TIMESTAMP
	`, key, m.ResolvedTitle, m.TMDBID)
	if err != nil {
		return fmt.Errorf("insert mapping: %w", err)
	}
	return nil
}
```

Note: the SQL column name `english_title` is left as-is — it's a private on-disk schema detail, not worth a migration for a rename that's purely about the Go-level API.

- [ ] **Step 3: Update `internal/cache/cache_test.go`**

Replace every `EnglishTitle:` field literal and `.EnglishTitle` access with `ResolvedTitle:` / `.ResolvedTitle`. The three affected spots:
```go
	want := Mapping{EnglishTitle: "Top Tennis Player", TMDBID: 123456}
```
→
```go
	want := Mapping{ResolvedTitle: "Top Tennis Player", TMDBID: 123456}
```
```go
	if got.EnglishTitle != want.EnglishTitle || got.TMDBID != want.TMDBID {
```
→
```go
	if got.ResolvedTitle != want.ResolvedTitle || got.TMDBID != want.TMDBID {
```
```go
	if err := c.Put("key", Mapping{EnglishTitle: "Old Title", TMDBID: 1}); err != nil {
		t.Fatalf("first Put() failed: %v", err)
	}
	if err := c.Put("key", Mapping{EnglishTitle: "New Title", TMDBID: 2}); err != nil {
		t.Fatalf("second Put() failed: %v", err)
	}

	got, ok, err := c.Get("key")
	if err != nil || !ok {
		t.Fatalf("Get() failed: ok=%v err=%v", ok, err)
	}
	if got.EnglishTitle != "New Title" || got.TMDBID != 2 {
```
→
```go
	if err := c.Put("key", Mapping{ResolvedTitle: "Old Title", TMDBID: 1}); err != nil {
		t.Fatalf("first Put() failed: %v", err)
	}
	if err := c.Put("key", Mapping{ResolvedTitle: "New Title", TMDBID: 2}); err != nil {
		t.Fatalf("second Put() failed: %v", err)
	}

	got, ok, err := c.Get("key")
	if err != nil || !ok {
		t.Fatalf("Get() failed: ok=%v err=%v", ok, err)
	}
	if got.ResolvedTitle != "New Title" || got.TMDBID != 2 {
```

- [ ] **Step 4: Update the two call sites in `internal/resolver/resolver.go`**

Replace:
```go
		return rewrite.Title(releaseTitle, segment, m.EnglishTitle)
```
with:
```go
		return rewrite.Title(releaseTitle, segment, m.ResolvedTitle)
```

Replace:
```go
	if err := r.Cache.Put(cacheKey, cache.Mapping{EnglishTitle: englishTitle, TMDBID: kpMatch.ExternalID.TMDB}); err != nil {
```
with:
```go
	if err := r.Cache.Put(cacheKey, cache.Mapping{ResolvedTitle: englishTitle, TMDBID: kpMatch.ExternalID.TMDB}); err != nil {
```

- [ ] **Step 5: Run tests to confirm nothing broke**

Run: `go build ./... && go test ./...`
Expected: all packages build, all tests `ok` (identical outcome to Step 1 — this was a pure rename).

- [ ] **Step 6: Commit**

```bash
git add internal/cache/cache.go internal/cache/cache_test.go internal/resolver/resolver.go
git commit -m "Rename cache.Mapping.EnglishTitle to ResolvedTitle

The field will hold a Russian title in upcoming reverse-direction cache
rows, so the old name would be actively misleading."
```

---

### Task 2: Add `tmdb.Client.SearchTV` and parameterize `TVTitle`'s language

**Files:**
- Modify: `internal/tmdb/tmdb.go`
- Modify: `internal/tmdb/tmdb_test.go`
- Modify: `internal/resolver/resolver.go`
- Modify: `internal/resolver/resolver_test.go`

**Interfaces:**
- Produces:
  - `tmdb.Client.TVTitle(tmdbID int, language string) (string, error)` (signature change)
  - `tmdb.Client.SearchTV(title string) (int, error)` — consumed by Task 3.

- [ ] **Step 1: Write the failing tests in `internal/tmdb/tmdb_test.go`**

Replace the entire file with:
```go
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
```

- [ ] **Step 2: Run tests to verify the new/changed ones fail**

Run: `go test ./internal/tmdb/...`
Expected: FAIL — compile error, since `TVTitle` doesn't yet accept a second argument and `SearchTV` doesn't exist yet.

- [ ] **Step 3: Implement in `internal/tmdb/tmdb.go`**

Replace:
```go
// TVTitle returns the canonical English title (en-US locale) for the TV
// series with the given TMDB ID.
func (c *Client) TVTitle(tmdbID int) (string, error) {
	return c.titleField(fmt.Sprintf("%s/tv/%d?language=en-US", c.BaseURL, tmdbID), "name")
}
```
with:
```go
// TVTitle returns the canonical title for the TV series with the given
// TMDB ID, localized to language (e.g. "en-US", "ru-RU").
func (c *Client) TVTitle(tmdbID int, language string) (string, error) {
	return c.titleField(fmt.Sprintf("%s/tv/%d?language=%s", c.BaseURL, tmdbID, url.QueryEscape(language)), "name")
}
```

Then add, after `MovieTitle` and before `titleField`:
```go
// SearchTV searches TMDB for a TV series by title text and returns the
// first (best) match's TMDB ID. Returns (0, nil) if the search found no
// results — treated as a lookup miss, not an error, by callers.
func (c *Client) SearchTV(title string) (int, error) {
	u := fmt.Sprintf("%s/search/tv?query=%s&api_key=%s", c.BaseURL, url.QueryEscape(title), url.QueryEscape(c.APIKey))

	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("tmdb search request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("tmdb search returned status %d", resp.StatusCode)
	}

	var parsed struct {
		Results []struct {
			ID int `json:"id"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return 0, fmt.Errorf("decode tmdb search response: %w", err)
	}

	if len(parsed.Results) == 0 {
		return 0, nil
	}
	return parsed.Results[0].ID, nil
}
```

- [ ] **Step 4: Run tests to verify the tmdb package now passes**

Run: `go test ./internal/tmdb/...`
Expected: PASS — `tmdb` doesn't import `resolver`, so this scoped test run succeeds on its own.

Note: `go build ./...` for the *whole* repo would still fail at this exact point — `internal/resolver` still calls the old one-argument `TVTitle` and its `TMDBTitleFetcher` interface hasn't been updated yet. Step 5 fixes that immediately, in the same task, so the repo is buildable again by the task's end (Step 7).

- [ ] **Step 5: Update `internal/resolver/resolver.go`**

Replace:
```go
// TMDBTitleFetcher is satisfied by *tmdb.Client.
type TMDBTitleFetcher interface {
	TVTitle(tmdbID int) (string, error)
	MovieTitle(tmdbID int) (string, error)
}
```
with:
```go
// TMDBTitleFetcher is satisfied by *tmdb.Client.
type TMDBTitleFetcher interface {
	TVTitle(tmdbID int, language string) (string, error)
	MovieTitle(tmdbID int) (string, error)
}
```

Replace:
```go
	var englishTitle string
	if mediaType == MediaMovie {
		englishTitle, err = r.TMDB.MovieTitle(kpMatch.ExternalID.TMDB)
	} else {
		englishTitle, err = r.TMDB.TVTitle(kpMatch.ExternalID.TMDB)
	}
```
with:
```go
	var englishTitle string
	if mediaType == MediaMovie {
		englishTitle, err = r.TMDB.MovieTitle(kpMatch.ExternalID.TMDB)
	} else {
		englishTitle, err = r.TMDB.TVTitle(kpMatch.ExternalID.TMDB, "en-US")
	}
```

- [ ] **Step 6: Update `internal/resolver/resolver_test.go`**

Replace:
```go
type fakeTMDB struct {
	tvTitle        string
	movieTitle     string
	err            error
	tvCallCount    int
	movieCallCount int
}

func (f *fakeTMDB) TVTitle(tmdbID int) (string, error) {
	f.tvCallCount++
	return f.tvTitle, f.err
}

func (f *fakeTMDB) MovieTitle(tmdbID int) (string, error) {
	f.movieCallCount++
	return f.movieTitle, f.err
}
```
with:
```go
type fakeTMDB struct {
	tvTitle        string
	movieTitle     string
	err            error
	tvCallCount    int
	movieCallCount int
	gotLanguage    string
}

func (f *fakeTMDB) TVTitle(tmdbID int, language string) (string, error) {
	f.tvCallCount++
	f.gotLanguage = language
	return f.tvTitle, f.err
}

func (f *fakeTMDB) MovieTitle(tmdbID int) (string, error) {
	f.movieCallCount++
	return f.movieTitle, f.err
}
```

(`gotLanguage` isn't asserted on by any test yet — it's used starting in Task 3. No existing test needs changes beyond the signature match, since none of them previously inspected the language argument.)

- [ ] **Step 7: Run the full suite to verify everything passes**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all packages build, all tests `ok`, `go vet` silent, `gofmt -l .` prints nothing.

- [ ] **Step 8: Commit**

```bash
git add internal/tmdb/tmdb.go internal/tmdb/tmdb_test.go internal/resolver/resolver.go internal/resolver/resolver_test.go
git commit -m "Add tmdb.SearchTV and parameterize TVTitle's language

Needed for reverse (English query -> Russian title) resolution: search
TMDB by title text to get an id, then fetch that id's title in
whichever language is needed (en-US for the existing forward direction,
ru-RU for the upcoming reverse one)."
```

---

### Task 3: Implement `Resolver.ResolveQuery`

**Files:**
- Modify: `internal/resolver/resolver.go`
- Modify: `internal/resolver/resolver_test.go`

**Interfaces:**
- Consumes: `tmdb.Client.SearchTV`, `tmdb.Client.TVTitle(id, language)` (Task 2); `cache.Mapping.ResolvedTitle` (Task 1).
- Produces: `resolver.Resolver.ResolveQuery(englishQuery string) (russianTitle string, ok bool)` — consumed by Task 4.

- [ ] **Step 1: Write the failing tests in `internal/resolver/resolver_test.go`**

First, extend `fakeTMDB` to support `SearchTV`. Replace the struct and its methods (as left by Task 2) with:
```go
type fakeTMDB struct {
	tvTitle        string
	movieTitle     string
	err            error
	tvCallCount    int
	movieCallCount int
	gotLanguage    string

	searchID        int
	searchErr       error
	searchCallCount int
	gotSearchQuery  string
}

func (f *fakeTMDB) TVTitle(tmdbID int, language string) (string, error) {
	f.tvCallCount++
	f.gotLanguage = language
	return f.tvTitle, f.err
}

func (f *fakeTMDB) MovieTitle(tmdbID int) (string, error) {
	f.movieCallCount++
	return f.movieTitle, f.err
}

func (f *fakeTMDB) SearchTV(title string) (int, error) {
	f.searchCallCount++
	f.gotSearchQuery = title
	return f.searchID, f.searchErr
}
```

Then append these new tests to the end of the file:
```go
func TestResolveQueryReturnsRussianTitleOnFullLookupChain(t *testing.T) {
	tm := &fakeTMDB{searchID: 999, tvTitle: "Первая ракетка"}
	r := New(&fakeKinopoisk{}, tm, newFakeCache())

	got, ok := r.ResolveQuery("Top Tennis Player")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got != "Первая ракетка" {
		t.Errorf("ResolveQuery() = %q, want %q", got, "Первая ракетка")
	}
	if tm.gotSearchQuery != "Top Tennis Player" {
		t.Errorf("expected search query %q, got %q", "Top Tennis Player", tm.gotSearchQuery)
	}
	if tm.gotLanguage != "ru-RU" {
		t.Errorf("expected ru-RU language, got %q", tm.gotLanguage)
	}
}

func TestResolveQueryUsesCacheOnSecondCall(t *testing.T) {
	tm := &fakeTMDB{searchID: 999, tvTitle: "Первая ракетка"}
	r := New(&fakeKinopoisk{}, tm, newFakeCache())

	first, _ := r.ResolveQuery("Top Tennis Player")
	second, _ := r.ResolveQuery("Top Tennis Player")

	if first != second {
		t.Errorf("expected identical results, got %q then %q", first, second)
	}
	if tm.searchCallCount != 1 || tm.tvCallCount != 1 {
		t.Errorf("expected the second call to hit the cache, got search=%d tv=%d", tm.searchCallCount, tm.tvCallCount)
	}
}

func TestResolveQueryReturnsFalseOnSearchMiss(t *testing.T) {
	tm := &fakeTMDB{searchID: 0}
	r := New(&fakeKinopoisk{}, tm, newFakeCache())

	if _, ok := r.ResolveQuery("Some Unknown Show"); ok {
		t.Error("expected ok=false when tmdb search finds nothing")
	}
	if tm.tvCallCount != 0 {
		t.Errorf("expected no TVTitle call on a search miss, got %d", tm.tvCallCount)
	}
}

func TestResolveQueryReturnsFalseOnSearchError(t *testing.T) {
	tm := &fakeTMDB{searchErr: errors.New("tmdb search down")}
	r := New(&fakeKinopoisk{}, tm, newFakeCache())

	if _, ok := r.ResolveQuery("Top Tennis Player"); ok {
		t.Error("expected ok=false on a tmdb search error")
	}
}

func TestResolveQueryReturnsFalseOnLocalizationError(t *testing.T) {
	tm := &fakeTMDB{searchID: 999, err: errors.New("tmdb down")}
	r := New(&fakeKinopoisk{}, tm, newFakeCache())

	if _, ok := r.ResolveQuery("Top Tennis Player"); ok {
		t.Error("expected ok=false when the ru-RU title lookup fails")
	}
}

func TestResolveQueryReturnsFalseWhenNoRussianTranslationExists(t *testing.T) {
	tm := &fakeTMDB{searchID: 999, tvTitle: "Top Tennis Player"}
	r := New(&fakeKinopoisk{}, tm, newFakeCache())

	if _, ok := r.ResolveQuery("Top Tennis Player"); ok {
		t.Error("expected ok=false when TMDB's ru-RU title falls back to the same English text")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/resolver/...`
Expected: FAIL — compile error, `ResolveQuery` doesn't exist yet.

- [ ] **Step 3: Implement `ResolveQuery` in `internal/resolver/resolver.go`**

Add `"strings"` to the import block:
```go
import (
	"log"
	"strings"

	"github.com/sidun-av/kinoadaptarr/internal/cache"
	"github.com/sidun-av/kinoadaptarr/internal/cyrillic"
	"github.com/sidun-av/kinoadaptarr/internal/kinopoisk"
	"github.com/sidun-av/kinoadaptarr/internal/rewrite"
)
```

Add `SearchTV` to the `TMDBTitleFetcher` interface:
```go
// TMDBTitleFetcher is satisfied by *tmdb.Client. Despite the name, it also
// covers TMDB's text search — used for reverse (English query -> Russian
// title) resolution, not just forward title-fetching.
type TMDBTitleFetcher interface {
	TVTitle(tmdbID int, language string) (string, error)
	MovieTitle(tmdbID int) (string, error)
	SearchTV(title string) (int, error)
}
```

Then append this new method at the end of the file, after `Resolve`:
```go
// ResolveQuery attempts to translate an English (Sonarr-supplied) TV
// series search query into its Russian title, for retrying a search that
// returned zero results against a Russian-language tracker. Returns
// ("", false) if no translation could be found — callers should treat
// that as "nothing to retry with", not an error.
func (r *Resolver) ResolveQuery(englishQuery string) (string, bool) {
	cacheKey := "revtv:" + strings.ToLower(strings.TrimSpace(englishQuery))

	if m, ok, err := r.Cache.Get(cacheKey); err != nil {
		log.Printf("resolver: reverse cache lookup failed for %q: %v", cacheKey, err)
	} else if ok {
		return m.ResolvedTitle, true
	}

	tmdbID, err := r.TMDB.SearchTV(englishQuery)
	if err != nil {
		log.Printf("resolver: tmdb search failed for %q: %v", englishQuery, err)
		return "", false
	}
	if tmdbID == 0 {
		log.Printf("resolver: no tmdb match for query %q", englishQuery)
		return "", false
	}

	russianTitle, err := r.TMDB.TVTitle(tmdbID, "ru-RU")
	if err != nil {
		log.Printf("resolver: tmdb ru-RU lookup failed for tmdb id %d (query %q): %v", tmdbID, englishQuery, err)
		return "", false
	}
	if russianTitle == englishQuery {
		// No Russian translation available — TMDB fell back to the same
		// title we searched with. Nothing useful to retry with.
		return "", false
	}

	if err := r.Cache.Put(cacheKey, cache.Mapping{ResolvedTitle: russianTitle, TMDBID: tmdbID}); err != nil {
		log.Printf("resolver: failed to cache reverse mapping for %q: %v", cacheKey, err)
	}

	return russianTitle, true
}
```

- [ ] **Step 4: Run the full suite to verify everything passes**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all packages build, all tests `ok`, `go vet` silent, `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
git add internal/resolver/resolver.go internal/resolver/resolver_test.go
git commit -m "Add Resolver.ResolveQuery for reverse title resolution

Translates an English TV search query to its Russian title via TMDB
(text search -> id -> ru-RU localized title), caching the result. Not
yet wired into the HTTP proxy layer."
```

---

### Task 4: Wire the retry into `proxy.Handler.ServeHTTP`

**Files:**
- Modify: `internal/proxy/proxy.go`
- Modify: `internal/proxy/proxy_test.go`

**Interfaces:**
- Consumes: `resolver.Resolver.ResolveQuery` (Task 3), via the `TitleResolver` interface.
- Produces: `proxy.Handler` now retries once on an empty `t=tvsearch` result.

- [ ] **Step 1: Write the failing tests in `internal/proxy/proxy_test.go`**

Add `"net/url"` to the import block:
```go
import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	resolverPkg "github.com/sidun-av/kinoadaptarr/internal/resolver"
	"github.com/sidun-av/kinoadaptarr/internal/torznab"
)
```

Replace the `fakeResolver` type and its `Resolve` method with a version that also implements `ResolveQuery`:
```go
type fakeResolver struct {
	rewrites      map[string]string
	gotMediaType  resolverPkg.MediaType
	queryResolves map[string]string
	gotQueries    []string
}

func (f *fakeResolver) Resolve(title string, mediaType resolverPkg.MediaType) string {
	f.gotMediaType = mediaType
	if rewritten, ok := f.rewrites[title]; ok {
		return rewritten
	}
	return title
}

func (f *fakeResolver) ResolveQuery(englishQuery string) (string, bool) {
	f.gotQueries = append(f.gotQueries, englishQuery)
	ru, ok := f.queryResolves[englishQuery]
	return ru, ok
}
```

Then append these new tests to the end of the file (before the final `resolverFunc` type/method, or after — placement among existing tests doesn't matter):
```go
func TestServeHTTPRetriesWithReverseResolvedQueryOnEmptyResult(t *testing.T) {
	var gotQueries []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQueries = append(gotQueries, r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/rss+xml")
		if r.URL.Query().Get("q") == "Top Tennis Player" {
			fmt.Fprint(w, `<?xml version="1.0"?><rss><channel></channel></rss>`)
			return
		}
		fmt.Fprint(w, `<?xml version="1.0"?>
<rss><channel>
  <item><title>Первая ракетка S1E8</title></item>
</channel></rss>`)
	}))
	defer upstream.Close()

	fr := &fakeResolver{
		rewrites:      map[string]string{"Первая ракетка S1E8": "Top Tennis Player S1E8"},
		queryResolves: map[string]string{"Top Tennis Player": "Первая ракетка"},
	}
	h := NewHandler(upstream.URL, fr, nil)

	req := httptest.NewRequest(http.MethodGet, "/rutracker/api?t=tvsearch&season=1&ep=8&q=Top+Tennis+Player", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(gotQueries) != 2 {
		t.Fatalf("expected 2 upstream requests (original + retry), got %d: %v", len(gotQueries), gotQueries)
	}

	retryQuery, err := url.ParseQuery(gotQueries[1])
	if err != nil {
		t.Fatalf("failed to parse retry query %q: %v", gotQueries[1], err)
	}
	if got := retryQuery.Get("q"); got != "Первая ракетка" {
		t.Errorf("expected retry q=%q, got %q", "Первая ракетка", got)
	}
	if retryQuery.Get("season") != "1" || retryQuery.Get("ep") != "8" {
		t.Errorf("expected season/ep preserved on retry, got %+v", retryQuery)
	}

	got := rec.Body.String()
	if !strings.Contains(got, "<title>Top Tennis Player S1E8</title>") {
		t.Errorf("expected the retried response's item to be present and title-rewritten, got:\n%s", got)
	}
}

func TestServeHTTPDoesNotRetryForNonTVSearch(t *testing.T) {
	callCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, `<?xml version="1.0"?><rss><channel></channel></rss>`)
	}))
	defer upstream.Close()

	fr := &fakeResolver{queryResolves: map[string]string{"Some Movie": "Какой-то Фильм"}}
	h := NewHandler(upstream.URL, fr, nil)

	req := httptest.NewRequest(http.MethodGet, "/api?t=movie&q=Some+Movie", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if callCount != 1 {
		t.Errorf("expected exactly 1 upstream request for a movie search (no reverse-query retry), got %d", callCount)
	}
}

func TestServeHTTPFallsBackWhenNoReverseResolutionFound(t *testing.T) {
	callCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, `<?xml version="1.0"?><rss><channel></channel></rss>`)
	}))
	defer upstream.Close()

	fr := &fakeResolver{} // queryResolves is nil: ResolveQuery always returns ok=false
	h := NewHandler(upstream.URL, fr, nil)

	req := httptest.NewRequest(http.MethodGet, "/rutracker/api?t=tvsearch&q=Unknown+Show", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if callCount != 1 {
		t.Errorf("expected exactly 1 upstream request when no reverse resolution is found, got %d", callCount)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/proxy/...`
Expected: FAIL. This compiles fine (`fakeResolver` implementing an extra `ResolveQuery` method beyond what the old `TitleResolver` interface requires isn't a compile error), but `TestServeHTTPRetriesWithReverseResolvedQueryOnEmptyResult` fails at runtime: the old `ServeHTTP` never retries, so it only ever makes 1 upstream request, and the test asserts 2 (`len(gotQueries) != 2`). The other two new tests may already pass against the old code (it never retries at all, so "don't retry" is trivially true) — that's fine, they become meaningful regression guards once Step 3 adds real retry logic.

- [ ] **Step 3: Implement in `internal/proxy/proxy.go`**

Add `"net/url"` to the import block:
```go
import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/sidun-av/kinoadaptarr/internal/resolver"
	"github.com/sidun-av/kinoadaptarr/internal/torznab"
)
```

Replace the `TitleResolver` interface and `rewriteTitles`'s signature to segregate the per-item rewrite dependency from the full handler dependency:
```go
// itemTitleResolver is the narrow dependency rewriteTitles needs — just
// enough to resolve one item's title. TitleResolver (below) is a superset,
// satisfied by *resolver.Resolver.
type itemTitleResolver interface {
	Resolve(releaseTitle string, mediaType resolver.MediaType) string
}

// TitleResolver is satisfied by *resolver.Resolver.
type TitleResolver interface {
	itemTitleResolver
	// ResolveQuery translates an English TV search query to its Russian
	// title, for retrying a search that returned zero results. ok is false
	// if no translation could be found.
	ResolveQuery(englishQuery string) (russianTitle string, ok bool)
}
```

Replace the `ServeHTTP` method body entirely:
```go
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

	result, err := h.fetch(upstream)
	if err != nil {
		log.Printf("proxy: upstream request failed: %v", err)
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		return
	}

	if result.statusCode != http.StatusOK {
		w.WriteHeader(result.statusCode)
		w.Write(result.body)
		return
	}

	rss, err := torznab.Parse(result.body)
	if err != nil {
		// Not parseable as Torznab XML (could be an error page, or a
		// caps/non-search request) — pass it through unchanged rather than
		// failing the whole request.
		w.Header().Set("Content-Type", result.header.Get("Content-Type"))
		w.Write(result.body)
		return
	}

	if len(rss.Channel.Items) == 0 {
		if retried, ok := h.retryWithReverseResolvedQuery(r, upstream); ok {
			result = retried.result
			rss = retried.rss
		}
	}

	out := rewriteTitles(result.body, rss.Channel.Items, h.Resolver, mediaType)

	w.Header().Set("Content-Type", result.header.Get("Content-Type"))
	w.Write(out)
}

// fetchResult holds the pieces of an upstream response ServeHTTP needs.
type fetchResult struct {
	body       []byte
	statusCode int
	header     http.Header
}

// fetch performs a GET against upstream and reads the full body.
func (h *Handler) fetch(upstream string) (*fetchResult, error) {
	resp, err := h.HTTPClient.Get(upstream)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return &fetchResult{body: body, statusCode: resp.StatusCode, header: resp.Header}, nil
}

// retriedFetch is the outcome of a successful reverse-query retry.
type retriedFetch struct {
	result *fetchResult
	rss    *torznab.RSS
}

// retryWithReverseResolvedQuery re-issues a t=tvsearch request with q
// translated to its Russian equivalent, for the case where the original
// (English) query found nothing. Returns ok=false if this isn't a
// tvsearch request, q is empty, no translation was found, the retry
// request itself failed, or the retry also came back with zero items —
// in every one of those cases the caller should keep using the original
// (empty) result.
func (h *Handler) retryWithReverseResolvedQuery(r *http.Request, upstream string) (*retriedFetch, bool) {
	if r.URL.Query().Get("t") != "tvsearch" {
		return nil, false
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		return nil, false
	}

	russianQuery, ok := h.Resolver.ResolveQuery(q)
	if !ok {
		return nil, false
	}

	retryURL, err := replaceQueryParam(upstream, "q", russianQuery)
	if err != nil {
		log.Printf("proxy: failed to build retry URL: %v", err)
		return nil, false
	}

	result, err := h.fetch(retryURL)
	if err != nil {
		log.Printf("proxy: retry upstream request failed: %v", err)
		return nil, false
	}
	if result.statusCode != http.StatusOK {
		return nil, false
	}

	rss, err := torznab.Parse(result.body)
	if err != nil || len(rss.Channel.Items) == 0 {
		return nil, false
	}

	return &retriedFetch{result: result, rss: rss}, true
}

// replaceQueryParam returns rawURL with the query parameter name set to
// value, preserving every other parameter and the base URL untouched.
func replaceQueryParam(rawURL, name, value string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set(name, value)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
```

Finally, update `rewriteTitles`'s parameter type from `TitleResolver` to the narrower `itemTitleResolver` (it only ever calls `.Resolve`, and this keeps the existing `resolverFunc` test helper — which implements only `Resolve` — compiling unchanged):
```go
func rewriteTitles(body []byte, items []torznab.Item, res itemTitleResolver, mediaType resolver.MediaType) []byte {
```

- [ ] **Step 4: Run the full suite to verify everything passes**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all packages build, all tests `ok`, `go vet` silent, `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
git add internal/proxy/proxy.go internal/proxy/proxy_test.go
git commit -m "Retry tv-search once with a reverse-resolved Russian query

Sonarr always searches with its English (TVDB) series title, and this
tracker's caps only support plain-text q search (no tvdbid). A
Russian-original series whose only TVDB title is an English translation
was therefore unfindable by search, no matter what the existing
response-side title rewriting did. On a zero-item result for
t=tvsearch, retry once with q replaced by the TMDB-derived Russian
title; any failure falls through to the original (empty) response."
```

---

### Task 5: Document the fallback and do a final full verification

**Files:**
- Modify: `README.md`

**Interfaces:** None (documentation only).

- [ ] **Step 1: Add a new subsection to `README.md`**

Find this paragraph (currently right after the numbered "How it works" list):
```markdown
If Kinopoisk or TMDB has no match for a title, it's passed through
unchanged and logged — this reduces manual curation to the rare, truly
obscure/very-recent case, not eliminates it entirely.
```

Replace it with itself plus a new subsection immediately after:
```markdown
If Kinopoisk or TMDB has no match for a title, it's passed through
unchanged and logged — this reduces manual curation to the rare, truly
obscure/very-recent case, not eliminates it entirely.

### TV search fallback

The above only helps once a search has actually returned something. Most
Russian trackers' Torznab search only supports plain-text `q` (no
`tvdbid`), and Sonarr always searches using its English/TVDB-registered
series title — so a Russian-original series whose only TVDB entry is an
English translation (e.g. `Первая ракетка` registered as `Top Tennis
Player`) would never be found by search at all, since that English text
never appears anywhere in the tracker's own release titles.

To fix this, `t=tvsearch` requests that come back with zero results are
retried once: kinoadaptarr translates `q` to its Russian equivalent via
TMDB (a text search by `q`, then a `ru-RU`-localized title for the
matched TMDB ID) and re-issues the same search with that instead. If no
Russian title can be resolved, or the retry also finds nothing, the
original (empty) result is returned unchanged — this can only turn some
zero-result searches into successful ones, never make anything worse.
Movie search (Radarr) doesn't have this fallback yet.
```

- [ ] **Step 2: Run the full verification suite**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l .`
Expected: all packages build, all tests `ok`, `go vet` silent, `gofmt -l .` prints nothing.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "Document the TV search reverse-resolution fallback"
```
