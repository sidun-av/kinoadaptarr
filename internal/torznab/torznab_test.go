package torznab

import (
	"strings"
	"testing"
)

const sampleRSS = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom" xmlns:torznab="http://torznab.com/schemas/2015/feed">
  <channel>
    <title>Prowlarr</title>
    <item>
      <title>Первая ракетка / S1E1-8 of 8 [WEBDL 1080p]</title>
      <guid>abc123</guid>
      <enclosure url="magnet:?xt=urn:btih:abc" length="1000" type="application/x-bittorrent"/>
      <torznab:attr name="seeders" value="42"></torznab:attr>
    </item>
    <item>
      <title>Alice Doesn&#39;t Live Here Anymore [1974]</title>
      <guid>def456</guid>
      <enclosure url="magnet:?xt=urn:btih:def" length="2000" type="application/x-bittorrent"/>
    </item>
  </channel>
</rss>`

func TestParseExtractsItems(t *testing.T) {
	rss, err := Parse([]byte(sampleRSS))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rss.Channel.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(rss.Channel.Items))
	}
	if got := rss.Channel.Items[0].Title(); got != "Первая ракетка / S1E1-8 of 8 [WEBDL 1080p]" {
		t.Errorf("unexpected title: %q", got)
	}
	if rss.Channel.Items[0].GUID != "abc123" {
		t.Errorf("unexpected guid: %q", rss.Channel.Items[0].GUID)
	}
}

func TestTitleRawPreservesEntityEscaping(t *testing.T) {
	rss, err := Parse([]byte(sampleRSS))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	item := rss.Channel.Items[1]
	if got := item.Title(); got != "Alice Doesn't Live Here Anymore [1974]" {
		t.Errorf("expected decoded Title() with a real apostrophe, got %q", got)
	}
	if got := item.TitleRaw(); got != "Alice Doesn&#39;t Live Here Anymore [1974]" {
		t.Errorf("expected TitleRaw() to keep the original &#39; entity, got %q", got)
	}
	// TitleRaw() must appear verbatim in the original source bytes, so
	// callers can locate and replace it exactly.
	wrapped := "<title>" + item.TitleRaw() + "</title>"
	if !strings.Contains(sampleRSS, wrapped) {
		t.Errorf("TitleRaw() %q was not found verbatim in the source document", item.TitleRaw())
	}
}
