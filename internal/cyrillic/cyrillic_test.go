package cyrillic

import "testing"

func TestHasCyrillic(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"Первая ракетка", true},
		{"Top Tennis Player S01E01", false},
		{"Mixed Первая text", true},
		{"", false},
	}
	for _, c := range cases {
		if got := HasCyrillic(c.in); got != c.want {
			t.Errorf("HasCyrillic(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestExtractTitle(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Первая ракетка (2 Сезон: 1 Серии: 1-8 из 8) WEBDL", "Первая ракетка"},
		{"Первая ракетка / S1E1-8 of 8 [WEBDL 1080p]", "Первая ракетка"},
		{"Психологини Сезон 1 Серия 5", "Психологини"},
		{"Кеша должен умереть 2024 WEB-DL", "Кеша должен умереть"},
		{"NoMarkersHere", "NoMarkersHere"},
	}
	for _, c := range cases {
		if got := ExtractTitle(c.in); got != c.want {
			t.Errorf("ExtractTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
