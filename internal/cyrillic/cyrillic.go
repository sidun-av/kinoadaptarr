// Package cyrillic detects Cyrillic text in release titles and extracts the
// probable series/movie name segment so it can be resolved against Kinopoisk.
package cyrillic

import (
	"regexp"
	"strings"
)

var cyrillicRe = regexp.MustCompile(`\p{Cyrillic}`)

// HasCyrillic reports whether s contains at least one Cyrillic letter.
func HasCyrillic(s string) bool {
	return cyrillicRe.MatchString(s)
}

// seasonEpisodeMarkers matches the Russian words that typically separate a
// release's title from its season/episode/quality metadata, so everything
// before the first match is treated as the title segment.
//
// Go's RE2 \b word-boundary is ASCII-only and does not recognize Cyrillic
// letters as word characters, so it can't be used around Cyrillic keywords —
// these are matched as plain substrings instead.
var seasonEpisodeMarkers = regexp.MustCompile(`(?i)[\(\[/]|сезон\w*|сери[ияй]\w*|эпизод\w*|выпуск\w*|\d{4}`)

// ExtractTitle returns the probable series/movie title segment from a raw
// release title, e.g. "Первая ракетка (2 Сезон: 1 Серии: 1-8 из 8) WEBDL"
// -> "Первая ракетка". Returns the trimmed input unchanged if no marker is
// found.
func ExtractTitle(releaseTitle string) string {
	loc := seasonEpisodeMarkers.FindStringIndex(releaseTitle)
	segment := releaseTitle
	if loc != nil {
		segment = releaseTitle[:loc[0]]
	}
	segment = strings.TrimSpace(segment)
	segment = strings.Trim(segment, "-/ \t")
	return segment
}
