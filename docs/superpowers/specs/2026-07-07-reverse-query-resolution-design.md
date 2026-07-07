# Reverse query resolution for TV search (design)

## Problem

kinoadaptarr currently only fixes the *response* side of a Torznab search:
Cyrillic release titles coming back from Prowlarr get rewritten to their
canonical English (TMDB) title so Sonarr can match them.

It does not touch the *request* side. Sonarr always searches using its own
TVDB-registered (English) series title. For a Russian-original series whose
TVDB entry is an English translation (e.g. `Первая ракетка` registered on
TVDB as `Top Tennis Player`), the release on rutracker never contains the
English string anywhere — title, description, or otherwise. A literal
text search for `Top Tennis Player` on a Russian-language tracker's own
search engine finds nothing, forever, regardless of anything kinoadaptarr
does to the (empty) response.

Confirmed via the indexer's own Torznab caps: `tv-search` for this upstream
supports only `supportedParams="q,season,ep"` — no `tvdbid`/`imdbid`, so
Sonarr always falls back to a plain-text `q` search. There is no numeric ID
available in the request to resolve against; only the English title text.

This is a distinct problem from the response-side one and needs a
request-side fix: translate `q` to the show's Russian title *before*
forwarding to the upstream indexer.

## Scope

- **TV search only** (Sonarr, `t=tvsearch`). Movie search (Radarr,
  `t=movie`) has the same underlying limitation (its caps also advertise
  only `q`), but is explicitly out of scope for this iteration — can be
  added later as a follow-up using the same mechanism.
- Only triggers as a **fallback on empty results**: the original request is
  sent unchanged first (today's exact behavior). Only if the upstream
  response parses with **zero items** does kinoadaptarr retry the search
  once, with `q` replaced by a Kinopoisk/TMDB-derived Russian title.
  Rationale: zero regression risk for anything that already works (e.g.
  foreign shows whose Russian releases already embed the English original
  title and match fine as-is); the extra upstream round-trip only happens
  in the case that's already returning nothing useful anyway.
- `t=caps` and generic `t=search` (RSS sync) are untouched.

## Resolution mechanism

Reverse direction: English query text -> TMDB text search -> TMDB id ->
TMDB `ru-RU` localized title -> substituted into `q`.

This mirrors, in reverse, the existing forward pipeline (Cyrillic text ->
Kinopoisk search -> TMDB id -> TMDB `en-US` title) but uses **TMDB only**,
not Kinopoisk. Rationale: TMDB has ample rate limits (already using a v3
API key); Kinopoisk/poiskkino.dev's free tier has a very tight daily quota
that has already been exhausted once this week, and every forward
resolution already spends against it — adding the reverse direction to the
same budget would make that worse. A hybrid (TMDB first, Kinopoisk
fallback) was considered and rejected for now as unnecessary complexity
(YAGNI) unless TMDB proves insufficient in practice.

## Components

### `internal/tmdb`

- New method: `SearchTV(title string) (tmdbID int, err error)`. Calls
  `/search/tv?query=<title>`, returns the first result's `id`. Returns
  `(0, err)` on request failure, or `(0, nil)` if `results` is empty (a
  miss, not an error — matches the existing "no match" convention used
  elsewhere in this codebase, e.g. `kinopoisk.Search`).
- `TVTitle` gains a `language` parameter: `TVTitle(tmdbID int, language
  string) (string, error)`. The existing forward-resolution call site
  (`resolver.go`) is updated to pass `"en-US"` explicitly. `MovieTitle` is
  unchanged (movies are out of scope).

### `internal/resolver`

- New method on `Resolver`:
  ```go
  func (r *Resolver) ResolveQuery(englishQuery string) (russianTitle string, ok bool)
  ```
  Flow: normalize `englishQuery` (trim/lowercase) as a cache key prefixed
  `revtv:` -> on cache hit, return it. On miss: `TMDB.SearchTV(englishQuery)`
  -> if a match, `TMDB.TVTitle(id, "ru-RU")` -> if the result is non-empty
  and differs from `englishQuery`, cache it and return `(title, true)`.
  Any failure or miss at any step: log (matching the existing `resolver:
  ...` log message style) and return `("", false)` — the caller simply
  does not retry, identical to today's behavior.
- Does **not** call Kinopoisk.

### `internal/cache`

- No schema change. Reuses the existing `title_mappings` table with a new
  key namespace (`revtv:` prefix) that doesn't collide with the existing
  `tv:`/`movie:` forward-direction keys.
- `Mapping.EnglishTitle` is renamed to `Mapping.ResolvedTitle` — the field
  holds a Russian title in reverse-direction rows, so the old name would be
  actively misleading. Two forward-direction call sites in `resolver.go`
  are updated to match; no other behavior change.

### `internal/proxy`

In `Handler.ServeHTTP`, after the first upstream fetch and
`torznab.Parse`:

1. If the request is `t=tvsearch`, has a non-empty `q`, and the parsed
   response has zero `Channel.Items`:
   a. Call `resolver.ResolveQuery(q)`.
   b. On success, build a retry request: same upstream URL and query
      string as the original, with only `q` replaced by the resolved
      Russian title. Fetch it.
   c. If the retry succeeds and parses with at least one item, use *that*
      body/`rss` for the rest of the existing pipeline (title rewriting,
      response writing) instead of the original empty one.
   d. On any failure/miss at any point in (a)-(c), fall through silently
      to today's behavior (return the original, empty response).
2. Everything downstream (per-item forward title rewriting) is unchanged
   and still runs on whichever response body was ultimately selected.

The `TitleResolver` interface gains a second method (or a sibling interface
is introduced) so `Handler` depends on an interface, not a concrete type —
matching the existing dependency-injection/testability pattern.

## Error handling

Every failure mode (TMDB search miss, TMDB detail-fetch failure, network
error, retry itself returning zero items) degrades to exactly today's
behavior: the original empty response is returned to Sonarr, unchanged.
Nothing about this feature can make a search result *worse* than it
currently is — it can only turn some zero-result searches into
successful ones.

One edge case worth naming explicitly: if TMDB has no `ru-RU` translation
for a given id, `TVTitle(id, "ru-RU")` may return the original/English name
rather than an error (TMDB's own fallback behavior, not something this
code controls). In that case `ResolveQuery` returns the same text that was
just searched with, the retry is sent with an unchanged `q`, and it
predictably returns the same zero-item response again — a harmless no-op,
not a new failure mode.

## Testing

- `internal/tmdb`: unit tests for `SearchTV` (match, no-match, error) and
  for the now-parameterized `TVTitle` (language param reaches the request),
  following the existing `httptest`-based patterns in `tmdb_test.go`.
- `internal/resolver`: unit tests for `ResolveQuery` (cache hit, cache miss
  with successful TMDB resolution, TMDB search miss, TMDB detail-fetch
  failure), using mock `TMDBTitleFetcher`/`KinopoiskSearcher`/`Cache` as
  `resolver_test.go` already does.
- `internal/proxy`: one new test case in `proxy_test.go` simulating a
  two-request sequence — first upstream response has zero items, second
  (triggered by the retry with a different `q`) has items — asserting the
  final response reflects the retry's (title-rewritten) items.
