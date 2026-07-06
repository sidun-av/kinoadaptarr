// Package torznab implements read-only parsing of the Torznab RSS feed
// format used by Prowlarr/Jackett. Deliberately does NOT re-serialize the
// document: Go's encoding/xml cannot reliably round-trip xmlns namespace
// declarations captured via a generic ",any,attr" catch-all (it re-emits
// them as malformed attributes like `xmlns:_xmlns="xmlns"` instead of the
// original `xmlns:atom="..."`). Callers that need to rewrite a release's
// title should splice Item.TitleRaw() directly into the original response
// bytes instead of re-marshaling the whole document — see internal/proxy.
package torznab

import (
	"encoding/xml"
	"html"
)

// RSS is the root element of a Torznab search response.
type RSS struct {
	XMLName xml.Name `xml:"rss"`
	Channel Channel  `xml:"channel"`
}

// Channel is the Torznab <channel> element.
type Channel struct {
	Title string `xml:"title,omitempty"`
	Items []Item `xml:"item"`
}

// Item is a single Torznab <item> (one release).
type Item struct {
	// TitleElem captures the raw, still entity-escaped inner content of
	// <title> — encoding/xml can't combine a named-element selector with
	// ",innerxml" on the same field, so it's captured via this nested
	// struct instead (and must be exported for encoding/xml's reflection to
	// see it). Use Title()/TitleRaw() rather than this field directly.
	TitleElem struct {
		Raw string `xml:",innerxml"`
	} `xml:"title"`
	GUID string `xml:"guid,omitempty"`
}

// Title returns the release title with XML entities decoded (e.g. "&#39;"
// -> "'"), suitable for Cyrillic detection and title-resolution logic.
func (i Item) Title() string {
	return html.UnescapeString(i.TitleElem.Raw)
}

// TitleRaw returns the exact original bytes between <title> and </title>,
// still entity-escaped as they appeared in the source document. Use this
// (not Title()) to locate the exact byte span to replace when rewriting
// the response, since Title() has already been unescaped and won't match
// the raw source text verbatim.
func (i Item) TitleRaw() string {
	return i.TitleElem.Raw
}

// Parse decodes a Torznab RSS response body. Returns an error if data isn't
// a well-formed <rss><channel> document (e.g. an error page, or a
// non-search response) — callers should treat that as "pass through
// unchanged" rather than a fatal error.
func Parse(data []byte) (*RSS, error) {
	var rss RSS
	if err := xml.Unmarshal(data, &rss); err != nil {
		return nil, err
	}
	return &rss, nil
}
