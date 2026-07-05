// Package rewrite normalizes Russian season/episode phrasing into the
// SxxEyy form Sonarr's parser expects, and substitutes a resolved English
// title into a release title in place of its Cyrillic segment.
package rewrite

import (
	"regexp"
)

// These match the natural Russian word order "Сезон N ... Серия M" (word
// before number). Some trackers instead use "N Сезон ... M Серии" (number
// before word); that's a tracker-specific quirk left for a future pass
// rather than handled generically here.
var (
	// "Сезон N ... Серия/Эпизод/Выпуск M из K" -> "SNEM of K"
	seasonEpisodeOf = regexp.MustCompile(
		`(?i)[Сс]езон\w*[\s:]*(\d+(?:-\d+)?).*?(?:[Сс]ери[ияй]\w*|[Ээ]пизод\w*|[Вв]ыпуск\w*)[\s:]*(\d+(?:-\d+)?)\s*из\s*(\w+)`,
	)
	// "Сезон N ... Серия/Эпизод/Выпуск M" -> "SNEM"
	seasonEpisode = regexp.MustCompile(
		`(?i)[Сс]езон\w*[\s:]*(\d+(?:-\d+)?).*?(?:[Сс]ери[ияй]\w*|[Ээ]пизод\w*|[Вв]ыпуск\w*)[\s:]*(\d+(?:-\d+)?)`,
	)
	// bare "Сезон N" -> "SN"
	seasonOnly = regexp.MustCompile(`(?i)[Сс]езон\w*[\s:]*(\d+(?:-\d+)?)`)
)

// NormalizeSeasonEpisode rewrites Russian season/episode phrasing in s into
// Sonarr-parseable SxxEyy notation. Unmatched input is returned unchanged.
func NormalizeSeasonEpisode(s string) string {
	if seasonEpisodeOf.MatchString(s) {
		return seasonEpisodeOf.ReplaceAllString(s, "S${1}E${2} of ${3}")
	}
	if seasonEpisode.MatchString(s) {
		return seasonEpisode.ReplaceAllString(s, "S${1}E${2}")
	}
	if seasonOnly.MatchString(s) {
		return seasonOnly.ReplaceAllString(s, "S${1}")
	}
	return s
}

// Title replaces the first occurrence of cyrillicSegment in releaseTitle
// with englishTitle, then normalizes any remaining season/episode phrasing.
// If cyrillicSegment isn't found verbatim in releaseTitle, releaseTitle is
// returned with season/episode normalization applied only.
func Title(releaseTitle, cyrillicSegment, englishTitle string) string {
	out := releaseTitle
	if cyrillicSegment != "" {
		re := regexp.MustCompile(regexp.QuoteMeta(cyrillicSegment))
		out = re.ReplaceAllString(out, englishTitle)
	}
	return NormalizeSeasonEpisode(out)
}
