package resolver

import (
	"errors"
	"testing"

	"github.com/sidun-av/kinoadaptarr/internal/cache"
	"github.com/sidun-av/kinoadaptarr/internal/kinopoisk"
)

type fakeKinopoisk struct {
	match     *kinopoisk.Match
	err       error
	callCount int
}

func (f *fakeKinopoisk) Search(title string) (*kinopoisk.Match, error) {
	f.callCount++
	return f.match, f.err
}

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

type fakeCache struct {
	store map[string]cache.Mapping
}

func newFakeCache() *fakeCache {
	return &fakeCache{store: make(map[string]cache.Mapping)}
}

func (f *fakeCache) Get(key string) (*cache.Mapping, bool, error) {
	m, ok := f.store[key]
	if !ok {
		return nil, false, nil
	}
	return &m, true, nil
}

func (f *fakeCache) Put(key string, m cache.Mapping) error {
	f.store[key] = m
	return nil
}

func TestResolvePassesThroughNonCyrillicTitles(t *testing.T) {
	kp := &fakeKinopoisk{}
	tm := &fakeTMDB{}
	r := New(kp, tm, newFakeCache())

	in := "Top Tennis Player S01E01 WEBRip"
	if got := r.Resolve(in, MediaTV); got != in {
		t.Errorf("Resolve() = %q, want unchanged %q", got, in)
	}
	if kp.callCount != 0 || tm.tvCallCount != 0 {
		t.Errorf("expected no external lookups for a non-Cyrillic title, got kp=%d tm=%d", kp.callCount, tm.tvCallCount)
	}
}

func TestResolveFullLookupChainTV(t *testing.T) {
	kpMatch := &kinopoisk.Match{Name: "Первая ракетка"}
	kpMatch.ExternalID.TMDB = 123456

	kp := &fakeKinopoisk{match: kpMatch}
	tm := &fakeTMDB{tvTitle: "Top Tennis Player"}
	r := New(kp, tm, newFakeCache())

	in := "Первая ракетка (Сезон 1 Серия 5) WEBDL"
	want := "Top Tennis Player (S1E5) WEBDL"
	if got := r.Resolve(in, MediaTV); got != want {
		t.Errorf("Resolve() = %q, want %q", got, want)
	}
	if kp.callCount != 1 || tm.tvCallCount != 1 || tm.movieCallCount != 0 {
		t.Errorf("expected one kinopoisk+tv lookup and no movie lookup, got kp=%d tv=%d movie=%d", kp.callCount, tm.tvCallCount, tm.movieCallCount)
	}
}

func TestResolveFullLookupChainMovie(t *testing.T) {
	kpMatch := &kinopoisk.Match{Name: "Какой-то Фильм"}
	kpMatch.ExternalID.TMDB = 654321

	kp := &fakeKinopoisk{match: kpMatch}
	tm := &fakeTMDB{movieTitle: "Some Movie"}
	r := New(kp, tm, newFakeCache())

	in := "Какой-то Фильм 2024 WEBDL"
	want := "Some Movie 2024 WEBDL"
	if got := r.Resolve(in, MediaMovie); got != want {
		t.Errorf("Resolve() = %q, want %q", got, want)
	}
	if kp.callCount != 1 || tm.movieCallCount != 1 || tm.tvCallCount != 0 {
		t.Errorf("expected one kinopoisk+movie lookup and no tv lookup, got kp=%d tv=%d movie=%d", kp.callCount, tm.tvCallCount, tm.movieCallCount)
	}
}

func TestResolveUsesCacheOnSecondCall(t *testing.T) {
	kpMatch := &kinopoisk.Match{Name: "Первая ракетка"}
	kpMatch.ExternalID.TMDB = 123456

	kp := &fakeKinopoisk{match: kpMatch}
	tm := &fakeTMDB{tvTitle: "Top Tennis Player"}
	r := New(kp, tm, newFakeCache())

	in := "Первая ракетка (Сезон 1 Серия 5) WEBDL"
	first := r.Resolve(in, MediaTV)
	second := r.Resolve(in, MediaTV)

	if first != second {
		t.Errorf("expected identical results, got %q then %q", first, second)
	}
	if kp.callCount != 1 || tm.tvCallCount != 1 {
		t.Errorf("expected the second call to hit the cache (no new lookups), got kp=%d tv=%d", kp.callCount, tm.tvCallCount)
	}
}

func TestResolveDoesNotConfuseMovieAndTVCacheEntries(t *testing.T) {
	kpMatch := &kinopoisk.Match{Name: "Первая ракетка"}
	kpMatch.ExternalID.TMDB = 123456

	kp := &fakeKinopoisk{match: kpMatch}
	tm := &fakeTMDB{tvTitle: "Top Tennis Player (Series)", movieTitle: "Top Tennis Player (Movie)"}
	r := New(kp, tm, newFakeCache())

	in := "Первая ракетка 2024"
	tvResult := r.Resolve(in, MediaTV)
	movieResult := r.Resolve(in, MediaMovie)

	if tvResult == movieResult {
		t.Errorf("expected distinct tv/movie resolutions, both came back as %q", tvResult)
	}
	if kp.callCount != 2 {
		t.Errorf("expected a separate kinopoisk lookup per media type (no cross-contamination), got %d", kp.callCount)
	}
}

func TestResolveReturnsOriginalOnKinopoiskMiss(t *testing.T) {
	kp := &fakeKinopoisk{match: nil}
	tm := &fakeTMDB{}
	r := New(kp, tm, newFakeCache())

	in := "Неизвестный Сериал Сезон 1 Серия 1"
	if got := r.Resolve(in, MediaTV); got != in {
		t.Errorf("Resolve() = %q, want unchanged %q on a kinopoisk miss", got, in)
	}
	if tm.tvCallCount != 0 {
		t.Errorf("expected no TMDB call when kinopoisk found nothing, got %d", tm.tvCallCount)
	}
}

func TestResolveReturnsOriginalOnKinopoiskError(t *testing.T) {
	kp := &fakeKinopoisk{err: errors.New("network error")}
	tm := &fakeTMDB{}
	r := New(kp, tm, newFakeCache())

	in := "Первая ракетка Сезон 1"
	if got := r.Resolve(in, MediaTV); got != in {
		t.Errorf("Resolve() = %q, want unchanged %q on a kinopoisk error", got, in)
	}
}

func TestResolveReturnsOriginalOnTMDBError(t *testing.T) {
	kpMatch := &kinopoisk.Match{Name: "Первая ракетка"}
	kpMatch.ExternalID.TMDB = 123456

	kp := &fakeKinopoisk{match: kpMatch}
	tm := &fakeTMDB{err: errors.New("tmdb down")}
	r := New(kp, tm, newFakeCache())

	in := "Первая ракетка Сезон 1"
	if got := r.Resolve(in, MediaTV); got != in {
		t.Errorf("Resolve() = %q, want unchanged %q on a tmdb error", got, in)
	}
}
