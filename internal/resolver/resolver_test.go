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
	title     string
	err       error
	callCount int
}

func (f *fakeTMDB) TVTitle(tmdbID int) (string, error) {
	f.callCount++
	return f.title, f.err
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
	if got := r.Resolve(in); got != in {
		t.Errorf("Resolve() = %q, want unchanged %q", got, in)
	}
	if kp.callCount != 0 || tm.callCount != 0 {
		t.Errorf("expected no external lookups for a non-Cyrillic title, got kp=%d tm=%d", kp.callCount, tm.callCount)
	}
}

func TestResolveFullLookupChain(t *testing.T) {
	kpMatch := &kinopoisk.Match{Name: "Первая ракетка"}
	kpMatch.ExternalID.TMDB = 123456

	kp := &fakeKinopoisk{match: kpMatch}
	tm := &fakeTMDB{title: "Top Tennis Player"}
	r := New(kp, tm, newFakeCache())

	in := "Первая ракетка (Сезон 1 Серия 5) WEBDL"
	want := "Top Tennis Player (S1E5) WEBDL"
	if got := r.Resolve(in); got != want {
		t.Errorf("Resolve() = %q, want %q", got, want)
	}
	if kp.callCount != 1 || tm.callCount != 1 {
		t.Errorf("expected exactly one kinopoisk+tmdb lookup, got kp=%d tm=%d", kp.callCount, tm.callCount)
	}
}

func TestResolveUsesCacheOnSecondCall(t *testing.T) {
	kpMatch := &kinopoisk.Match{Name: "Первая ракетка"}
	kpMatch.ExternalID.TMDB = 123456

	kp := &fakeKinopoisk{match: kpMatch}
	tm := &fakeTMDB{title: "Top Tennis Player"}
	r := New(kp, tm, newFakeCache())

	in := "Первая ракетка (Сезон 1 Серия 5) WEBDL"
	first := r.Resolve(in)
	second := r.Resolve(in)

	if first != second {
		t.Errorf("expected identical results, got %q then %q", first, second)
	}
	if kp.callCount != 1 || tm.callCount != 1 {
		t.Errorf("expected the second call to hit the cache (no new lookups), got kp=%d tm=%d", kp.callCount, tm.callCount)
	}
}

func TestResolveReturnsOriginalOnKinopoiskMiss(t *testing.T) {
	kp := &fakeKinopoisk{match: nil}
	tm := &fakeTMDB{}
	r := New(kp, tm, newFakeCache())

	in := "Неизвестный Сериал Сезон 1 Серия 1"
	if got := r.Resolve(in); got != in {
		t.Errorf("Resolve() = %q, want unchanged %q on a kinopoisk miss", got, in)
	}
	if tm.callCount != 0 {
		t.Errorf("expected no TMDB call when kinopoisk found nothing, got %d", tm.callCount)
	}
}

func TestResolveReturnsOriginalOnKinopoiskError(t *testing.T) {
	kp := &fakeKinopoisk{err: errors.New("network error")}
	tm := &fakeTMDB{}
	r := New(kp, tm, newFakeCache())

	in := "Первая ракетка Сезон 1"
	if got := r.Resolve(in); got != in {
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
	if got := r.Resolve(in); got != in {
		t.Errorf("Resolve() = %q, want unchanged %q on a tmdb error", got, in)
	}
}
