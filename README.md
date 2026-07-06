# kinoadaptarr

A Torznab proxy that resolves Cyrillic/Russian release titles to their
canonical TMDB English title, so Sonarr and Radarr can match releases they
would otherwise silently reject.

## The problem

Sonarr only matches a release against a series' primary (English) TVDB
title plus TheXEM/TVDB-registered aliases — it has no user-facing way to add
a custom alias, and this is a deliberate, repeatedly-rejected design
decision by its maintainers, not a bug. For a Russian-original series whose
only available releases are titled in Cyrillic (e.g. `Первая ракетка`) while
TVDB only knows the English title (`Top Tennis Player`), automatic search
returns zero results forever — every release gets rejected on title
mismatch, no matter how well-seeded it is.

Radarr, notably, *does* do the equivalent of what this project does —
automatically pulling every TMDB alternative title/translation for a movie
and using them as match candidates — proving the pattern works. Sonarr's
team has explicitly and repeatedly declined to build the equivalent
("Never going to happen"), so the fix has to live outside Sonarr entirely.

## How it works

```
Sonarr/Radarr → kinoadaptarr → Prowlarr → indexers
```

Works for both TV series (Sonarr) and movies (Radarr) — it detects which
from the Torznab request's `t=` parameter (`tvsearch` vs `movie`) and
resolves against the matching TMDB endpoint/field. Note that for movies,
Radarr already has its own automatic TMDB alternative-title matching (see
above) — kinoadaptarr only adds value there when TMDB itself doesn't have
the Russian title registered but Kinopoisk does, which is common for niche
or very recent releases but not universal like it is for series (where
Sonarr has no such matching at all).

Prowlarr syncs each of its indexers to Sonarr/Radarr as its own separate
Torznab entry rather than one combined feed, so kinoadaptarr proxies **one
route per indexer**: `/{name}/api` for each entry under `upstreams:` in its
config. Sonarr/Radarr's Torznab client always appends a literal `/api`
segment to whatever base URL an indexer is configured with (that's how the
original direct-to-Prowlarr URLs worked too), so set each of Sonarr's/
Radarr's existing per-indexer URLs to `http://kinoadaptarr:8080/{name}`
(**no** trailing `/api` — Sonarr/Radarr adds that itself) instead of
pointing directly at Prowlarr.

For every search:

1. kinoadaptarr forwards the request to the corresponding real Prowlarr
   indexer endpoint verbatim.
2. For each result whose title contains Cyrillic text, it extracts the
   probable series-name segment (everything before season/episode/year
   markers).
3. It checks a local SQLite cache for a previously-resolved mapping for that
   segment.
4. On a cache miss, it queries the [Kinopoisk](https://poiskkino.dev/) API
   (the authoritative Russian-content database) by that Cyrillic title, and
   reads back its `externalId.tmdb` field.
5. It queries TMDB **by that ID** (not a fuzzy text search — ID lookups are
   reliable regardless of TMDB's patchy crowd-sourced Russian-language
   search coverage) for the canonical English title.
6. It rewrites the release title (English series name + normalized
   `SxxEyy` episode notation) and caches the mapping so repeat lookups for
   the same show are free.
7. The rewritten Torznab response is returned to Sonarr, which now matches
   it like any other English-titled release.

If Kinopoisk or TMDB has no match for a title, it's passed through
unchanged and logged — this reduces manual curation to the rare, truly
obscure/very-recent case, not eliminates it entirely.

## Setup

### 1. Get API keys

- **Kinopoisk**: free tier (200 requests/day) at
  [poiskkino.dev](https://poiskkino.dev/) — plenty for a home library with
  caching enabled.
- **TMDB**: free API key from
  [themoviedb.org/settings/api](https://www.themoviedb.org/settings/api).

### 2. Configure

Copy [`config.example.yml`](config.example.yml) to `config.yml` and edit:

- `upstreams` — one entry per indexer Sonarr currently has synced from
  Prowlarr. Find each one's existing URL in Sonarr's Settings > Indexers
  page (each shows a path like `http://prowlarr:9696/1/api?apikey=...` —
  the number differs per indexer/app pair) and add a matching named entry
  here, e.g. `kinozal: "http://prowlarr:9696/1/api?apikey=${PROWLARR_API_KEY}&t=search"`.
- `kinopoisk.api_key`, `tmdb.api_key` — from step 1.

### 3. Run it alongside Prowlarr

```yaml
services:
  kinoadaptarr:
    image: ghcr.io/sidun-av/kinoadaptarr:latest
    restart: unless-stopped
    environment:
      - PROWLARR_API_KEY=${PROWLARR_API_KEY}
      - KINOPOISK_API_KEY=${KINOPOISK_API_KEY}
      - TMDB_API_KEY=${TMDB_API_KEY}
    volumes:
      - ./kinoadaptarr/config.yml:/config.yml:ro
      - kinoadaptarr_data:/data
    ports:
      - "8090:8080"

volumes:
  kinoadaptarr_data:
```

Add it to the same Docker network as Prowlarr.

### 4. Point Sonarr's indexers at it instead of Prowlarr directly

In Sonarr's Settings > Indexers, edit **each** indexer currently synced from
Prowlarr and change its URL from `http://prowlarr:9696/{N}/api` to
`http://kinoadaptarr:8080/{name}` (no trailing `/api` — Sonarr adds it
itself) — using the matching name you gave it under `upstreams:` in
kinoadaptarr's config. Leave everything else (API key, categories,
priority) as Sonarr already has it.

## Configuration reference

| Field | Default | Description |
|---|---|---|
| `port` | `8080` | Port the HTTP server listens on |
| `upstreams.<name>` | — (at least one required) | Full Torznab URL for one Prowlarr-synced indexer, including its own `apikey`. Exposed at `/api/<name>` |
| `kinopoisk.base_url` | `https://api.kinopoisk.dev` | Kinopoisk API root |
| `kinopoisk.api_key` | — (required) | Kinopoisk API key |
| `tmdb.base_url` | `https://api.themoviedb.org/3` | TMDB API root |
| `tmdb.api_key` | — (required) | TMDB API key |
| `cache_path` | `/data/kinoadaptarr.db` | Path to the SQLite mapping cache |

## Development

```bash
go test ./...
docker build -t kinoadaptarr:dev .
```

## License

MIT
