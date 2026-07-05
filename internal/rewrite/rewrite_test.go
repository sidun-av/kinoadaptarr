package rewrite

import "testing"

func TestNormalizeSeasonEpisode(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Сезон 2 Серии 1-8 из 8", "S2E1-8 of 8"},
		{"Сезон 1 Серия 5", "S1E5"},
		{"Сезон 1", "S1"},
		{"no markers here", "no markers here"},
	}
	for _, c := range cases {
		if got := NormalizeSeasonEpisode(c.in); got != c.want {
			t.Errorf("NormalizeSeasonEpisode(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTitleSubstitutesAndNormalizes(t *testing.T) {
	got := Title(
		"Первая ракетка (Сезон 2 Серии 1-8 из 8) WEBDL",
		"Первая ракетка",
		"Top Tennis Player",
	)
	want := "Top Tennis Player (S2E1-8 of 8) WEBDL"
	if got != want {
		t.Errorf("Title() = %q, want %q", got, want)
	}
}

func TestTitleLeavesUnmatchedSegmentUntouched(t *testing.T) {
	got := Title("Some.Release.Title.S01E01", "Кириллица которой нет", "English")
	want := "Some.Release.Title.S01E01"
	if got != want {
		t.Errorf("Title() = %q, want %q", got, want)
	}
}
