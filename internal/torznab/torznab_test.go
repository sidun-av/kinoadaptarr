package torznab

import "testing"

const sampleRSS = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom" xmlns:torznab="http://torznab.com/schemas/2015/feed">
  <channel>
    <title>Prowlarr</title>
    <item>
      <title>Первая ракетка / S1E1-8 of 8 [WEBDL 1080p]</title>
      <guid>abc123</guid>
      <link>http://example.com/details?id=1</link>
      <pubDate>Mon, 01 Jan 2024 00:00:00 +0000</pubDate>
      <enclosure url="magnet:?xt=urn:btih:abc" length="1000" type="application/x-bittorrent"/>
      <torznab:attr name="seeders" value="42"></torznab:attr>
      <torznab:attr name="peers" value="10"></torznab:attr>
    </item>
    <item>
      <title>Top Tennis Player S01E02 WEBRip</title>
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
	if rss.Channel.Items[0].Title != "Первая ракетка / S1E1-8 of 8 [WEBDL 1080p]" {
		t.Errorf("unexpected title: %q", rss.Channel.Items[0].Title)
	}
	if rss.Channel.Items[0].Enclosure == nil || rss.Channel.Items[0].Enclosure.URL != "magnet:?xt=urn:btih:abc" {
		t.Errorf("expected enclosure to be preserved, got %+v", rss.Channel.Items[0].Enclosure)
	}
	if attrs := rss.Channel.Items[0].Attrs(); len(attrs) != 2 {
		t.Errorf("expected 2 torznab:attr elements preserved, got %d", len(attrs))
	}
}

func TestMarshalRoundTripsRewrittenTitle(t *testing.T) {
	rss, err := Parse([]byte(sampleRSS))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rss.Channel.Items[0].Title = "Top Tennis Player S01E01-08 of 08 WEBDL 1080p"

	out, err := Marshal(rss)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reparsed, err := Parse(out)
	if err != nil {
		t.Fatalf("re-parse failed: %v\noutput was:\n%s", err, out)
	}
	if reparsed.Channel.Items[0].Title != "Top Tennis Player S01E01-08 of 08 WEBDL 1080p" {
		t.Errorf("rewritten title did not round-trip, got %q", reparsed.Channel.Items[0].Title)
	}
	if reparsed.Channel.Items[0].Enclosure.URL != "magnet:?xt=urn:btih:abc" {
		t.Errorf("enclosure lost on round-trip: %+v", reparsed.Channel.Items[0].Enclosure)
	}
	if len(reparsed.Channel.Items) != 2 {
		t.Fatalf("expected 2 items after round-trip, got %d", len(reparsed.Channel.Items))
	}
}
