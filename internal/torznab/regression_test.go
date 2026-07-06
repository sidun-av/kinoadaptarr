package torznab

import "testing"

// TestParsesActualBrokenNamespaceResponse uses the exact malformed-looking
// root element byte-for-byte as captured from the real bug report
// (xmlns:_xmlns="xmlns" _xmlns:atom=... _xmlns:torznab=...) to confirm
// read-only Parse() still works fine against it — the corruption only ever
// happened on the old Marshal() re-serialization path, which no longer
// exists.
func TestParsesActualBrokenNamespaceResponse(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="1.0" xmlns:_xmlns="xmlns" _xmlns:atom="http://www.w3.org/2005/Atom" _xmlns:torznab="http://torznab.com/schemas/2015/feed">
  <channel>
    <title>Prowlarr</title>
    <item>
      <title>Я тебя отыщу / Я найду тебя / I Will Find You / S1E1-8 of 8 [2026]</title>
      <guid>abc</guid>
    </item>
  </channel>
</rss>`)
	rss, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}
	if len(rss.Channel.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(rss.Channel.Items))
	}
	want := "Я тебя отыщу / Я найду тебя / I Will Find You / S1E1-8 of 8 [2026]"
	if got := rss.Channel.Items[0].Title(); got != want {
		t.Errorf("Title() = %q, want %q", got, want)
	}
}
