package cache

import (
	"path/filepath"
	"testing"
)

func openTestCache(t *testing.T) *Cache {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	c, err := Open(path)
	if err != nil {
		t.Fatalf("Open() failed: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestGetMissReturnsFalse(t *testing.T) {
	c := openTestCache(t)
	m, ok, err := c.Get("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Errorf("expected ok=false for a cache miss, got true with %+v", m)
	}
}

func TestPutThenGetRoundTrips(t *testing.T) {
	c := openTestCache(t)
	want := Mapping{EnglishTitle: "Top Tennis Player", TMDBID: 123456}

	if err := c.Put("Первая ракетка", want); err != nil {
		t.Fatalf("Put() failed: %v", err)
	}

	got, ok, err := c.Get("Первая ракетка")
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true after Put()")
	}
	if got.EnglishTitle != want.EnglishTitle || got.TMDBID != want.TMDBID {
		t.Errorf("Get() = %+v, want %+v", got, want)
	}
}

func TestPutOverwritesExistingKey(t *testing.T) {
	c := openTestCache(t)
	if err := c.Put("key", Mapping{EnglishTitle: "Old Title", TMDBID: 1}); err != nil {
		t.Fatalf("first Put() failed: %v", err)
	}
	if err := c.Put("key", Mapping{EnglishTitle: "New Title", TMDBID: 2}); err != nil {
		t.Fatalf("second Put() failed: %v", err)
	}

	got, ok, err := c.Get("key")
	if err != nil || !ok {
		t.Fatalf("Get() failed: ok=%v err=%v", ok, err)
	}
	if got.EnglishTitle != "New Title" || got.TMDBID != 2 {
		t.Errorf("Get() = %+v, want overwritten mapping", got)
	}
}
