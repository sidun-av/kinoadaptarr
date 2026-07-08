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

func TestResolveQueryReturnsFalseWhenTMDBFallbackDiffersButIsStillNotRussian(t *testing.T) {
	// TMDB's ru-RU fallback need not be byte-identical to the query (e.g.
	// it could include a subtitle or differ in punctuation) — the check
	// must catch "not actually Russian" via script detection, not via
	// exact string equality against the original query.
	tm := &fakeTMDB{searchID: 999, tvTitle: "Top Tennis Player (Original)"}
	r := New(&fakeKinopoisk{}, tm, newFakeCache())

	if _, ok := r.ResolveQuery("Top Tennis Player"); ok {
		t.Error("expected ok=false when TMDB's ru-RU fallback is non-Russian even if it differs from the query")
	}
}

func TestResolveQueryCachesNegativeResultOnSearchMiss(t *testing.T) {
	tm := &fakeTMDB{searchID: 0}
	r := New(&fakeKinopoisk{}, tm, newFakeCache())

	r.ResolveQuery("Some Unknown Show")
	if _, ok := r.ResolveQuery("Some Unknown Show"); ok {
		t.Error("expected ok=false on the cached second call")
	}
	if tm.searchCallCount != 1 {
		t.Errorf("expected the second call to be served from the negative cache (no repeat TMDB search), got %d search calls", tm.searchCallCount)
	}
}

func TestResolveQueryCachesNegativeResultWhenNoRussianTranslationExists(t *testing.T) {
	tm := &fakeTMDB{searchID: 999, tvTitle: "Top Tennis Player"}
	r := New(&fakeKinopoisk{}, tm, newFakeCache())

	r.ResolveQuery("Top Tennis Player")
	if _, ok := r.ResolveQuery("Top Tennis Player"); ok {
		t.Error("expected ok=false on the cached second call")
	}
	if tm.searchCallCount != 1 || tm.tvCallCount != 1 {
		t.Errorf("expected the second call to be served from the negative cache, got search=%d tv=%d", tm.searchCallCount, tm.tvCallCount)
	}
}

func TestResolveCachesNegativeResultOnKinopoiskMiss(t *testing.T) {
	kp := &fakeKinopoisk{match: nil}
	tm := &fakeTMDB{}
	r := New(kp, tm, newFakeCache())

	in := "Неизвестный Сериал Сезон 1 Серия 1"
	first := r.Resolve(in, MediaTV)
	second := r.Resolve(in, MediaTV)

	if first != in || second != in {
		t.Errorf("expected both calls to return input unchanged, got %q then %q", first, second)
	}
	if kp.callCount != 1 {
		t.Errorf("expected the second call to be served from the negative cache (no repeat kinopoisk search), got %d calls", kp.callCount)
	}
}

func TestResolveDoesNotCacheTransientKinopoiskErrorInForwardLookup(t *testing.T) {
	kp := &fakeKinopoisk{err: errors.New("network error")}
	tm := &fakeTMDB{}
	r := New(kp, tm, newFakeCache())

	in := "Первая ракетка Сезон 1"
	r.Resolve(in, MediaTV)

	kp.err = nil
	kpMatch := &kinopoisk.Match{Name: "Первая ракетка"}
	kpMatch.ExternalID.TMDB = 123456
	kp.match = kpMatch
	tm.tvTitle = "Top Tennis Player"

	want := "Top Tennis Player S1"
	if got := r.Resolve(in, MediaTV); got != want {
		t.Errorf("expected a transient kinopoisk error not to be cached, so a later call still resolves; got %q, want %q", got, want)
	}
}

func TestResolveQueryFallsBackToKinopoiskWhenTMDBMisses(t *testing.T) {
	tm := &fakeTMDB{searchID: 0}
	kpMatch := &kinopoisk.Match{Name: "Первая ракетка"}
	kp := &fakeKinopoisk{match: kpMatch}
	r := New(kp, tm, newFakeCache())

	got, ok := r.ResolveQuery("Top Tennis Player")
	if !ok {
		t.Fatal("expected ok=true from the kinopoisk fallback")
	}
	if got != "Первая ракетка" {
		t.Errorf("ResolveQuery() = %q, want %q", got, "Первая ракетка")
	}
	if kp.callCount != 1 {
		t.Errorf("expected one kinopoisk lookup, got %d", kp.callCount)
	}
}

func TestResolveQueryDoesNotCallKinopoiskWhenTMDBSucceeds(t *testing.T) {
	tm := &fakeTMDB{searchID: 999, tvTitle: "Первая ракетка"}
	kp := &fakeKinopoisk{match: &kinopoisk.Match{Name: "Первая ракетка"}}
	r := New(kp, tm, newFakeCache())

	if _, ok := r.ResolveQuery("Top Tennis Player"); !ok {
		t.Fatal("expected ok=true from TMDB")
	}
	if kp.callCount != 0 {
		t.Errorf("expected kinopoisk not to be called when TMDB already succeeded, got %d calls", kp.callCount)
	}
}

func TestResolveQueryReturnsFalseWhenKinopoiskAlsoMisses(t *testing.T) {
	tm := &fakeTMDB{searchID: 0}
	kp := &fakeKinopoisk{match: nil}
	r := New(kp, tm, newFakeCache())

	if _, ok := r.ResolveQuery("Some Unknown Show"); ok {
		t.Error("expected ok=false when both tmdb and kinopoisk find nothing")
	}
}

func TestResolveQueryReturnsFalseWhenKinopoiskMatchIsNotRussian(t *testing.T) {
	tm := &fakeTMDB{searchID: 0}
	kp := &fakeKinopoisk{match: &kinopoisk.Match{Name: "Top Tennis Player"}}
	r := New(kp, tm, newFakeCache())

	if _, ok := r.ResolveQuery("Top Tennis Player"); ok {
		t.Error("expected ok=false when kinopoisk's match name isn't russian")
	}
}

func TestResolveQueryCachesKinopoiskFallbackResult(t *testing.T) {
	tm := &fakeTMDB{searchID: 0}
	kp := &fakeKinopoisk{match: &kinopoisk.Match{Name: "Первая ракетка"}}
	r := New(kp, tm, newFakeCache())

	first, _ := r.ResolveQuery("Top Tennis Player")
	second, _ := r.ResolveQuery("Top Tennis Player")

	if first != second {
		t.Errorf("expected identical results, got %q then %q", first, second)
	}
	if tm.searchCallCount != 1 || kp.callCount != 1 {
		t.Errorf("expected the second call to hit the cache, got tmdb search=%d kinopoisk=%d", tm.searchCallCount, kp.callCount)
	}
}

func TestResolveQueryDoesNotCacheWhenKinopoiskErrorsTransiently(t *testing.T) {
	tm := &fakeTMDB{searchID: 0}
	kp := &fakeKinopoisk{err: errors.New("kinopoisk down")}
	r := New(kp, tm, newFakeCache())

	if _, ok := r.ResolveQuery("Top Tennis Player"); ok {
		t.Error("expected ok=false on a kinopoisk error")
	}

	kp.err = nil
	kp.match = &kinopoisk.Match{Name: "Первая ракетка"}

	got, ok := r.ResolveQuery("Top Tennis Player")
	if !ok || got != "Первая ракетка" {
		t.Errorf("expected a transient kinopoisk error not to be cached, so a later call still resolves; got (%q, %v)", got, ok)
	}
}

func TestResolveQueryDoesNotCacheTransientSearchError(t *testing.T) {
	tm := &fakeTMDB{searchErr: errors.New("tmdb search down")}
	r := New(&fakeKinopoisk{}, tm, newFakeCache())

	r.ResolveQuery("Top Tennis Player")
	tm.searchErr = nil
	tm.searchID = 999
	tm.tvTitle = "Первая ракетка"

	got, ok := r.ResolveQuery("Top Tennis Player")
	if !ok || got != "Первая ракетка" {
		t.Errorf("expected a transient search error not to be cached, so a later successful call still resolves; got (%q, %v)", got, ok)
	}
}
