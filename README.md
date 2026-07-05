# kinoadaptarr

A Torznab proxy that resolves Cyrillic/Russian release titles to their
canonical TMDB English title, so Sonarr can match releases it would
otherwise silently reject.

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
Sonarr → kinoadaptarr → Prowlarr → indexers
```

Sonarr's indexer config points at kinoadaptarr instead of Prowlarr directly.
For every search:

1. kinoadaptarr forwards the request to the real Prowlarr instance verbatim.
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

- `upstream_url` — the full Prowlarr Torznab endpoint kinoadaptarr should
  forward to, including Prowlarr's own `apikey`.
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

### 4. Point Sonarr at it instead of Prowlarr

In Sonarr, edit the indexer that currently points at Prowlarr and change its
URL to `http://kinoadaptarr:8080/api` (keeping Sonarr's own API key/settings
as they were) — kinoadaptarr forwards every query to the real Prowlarr
instance and rewrites the response before Sonarr ever sees it.

## Configuration reference

| Field | Default | Description |
|---|---|---|
| `port` | `8080` | Port the HTTP server listens on |
| `upstream_url` | — (required) | Full Prowlarr Torznab URL, including its own `apikey` |
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
