// Package torznab implements minimal parsing and re-serialization of the
// Torznab RSS feed format used by Prowlarr/Jackett, preserving every field
// and namespace attribute except the ones this proxy deliberately rewrites.
package torznab

import (
	"encoding/xml"
)

// RSS is the root element of a Torznab search response.
type RSS struct {
	XMLName xml.Name `xml:"rss"`
	Version string   `xml:"version,attr"`
	// Attrs preserves any extra namespace declarations (xmlns:atom,
	// xmlns:torznab, etc.) verbatim so we don't need to know them in advance.
	Attrs   []xml.Attr `xml:",any,attr"`
	Channel Channel    `xml:"channel"`
}

// Channel is the Torznab <channel> element.
type Channel struct {
	Title       string   `xml:"title,omitempty"`
	Description string   `xml:"description,omitempty"`
	Link        string   `xml:"link,omitempty"`
	Language    string   `xml:"language,omitempty"`
	Category    string   `xml:"category,omitempty"`
	Items       []Item   `xml:"item"`
	Extra       []RawXML `xml:",any"`
}

// Item is a single Torznab <item> (one release).
type Item struct {
	Title       string     `xml:"title"`
	GUID        string     `xml:"guid,omitempty"`
	Link        string     `xml:"link,omitempty"`
	Comments    string     `xml:"comments,omitempty"`
	PubDate     string     `xml:"pubDate,omitempty"`
	Category    []string   `xml:"category,omitempty"`
	Description string     `xml:"description,omitempty"`
	Enclosure   *Enclosure `xml:"enclosure,omitempty"`
	// Extra catches every child element not explicitly named above —
	// principally the namespaced <torznab:attr> elements (size, seeders,
	// peers, etc). Go's encoding/xml resolves namespaces by URI rather than
	// the "torznab:" prefix, so these can't be matched by a literal
	// "torznab:attr" struct tag; they're captured here instead and passed
	// through untouched. Use Attrs() to filter just the torznab:attr ones.
	Extra []RawXML `xml:",any"`
}

// Attrs returns the item's <torznab:attr> elements (size, seeders, peers,
// category, etc), filtered out of Extra by local element name.
func (i Item) Attrs() []RawXML {
	var attrs []RawXML
	for _, e := range i.Extra {
		if e.XMLName.Local == "attr" {
			attrs = append(attrs, e)
		}
	}
	return attrs
}

// Enclosure is the <enclosure> element pointing at the download/magnet link.
type Enclosure struct {
	URL    string `xml:"url,attr"`
	Length string `xml:"length,attr,omitempty"`
	Type   string `xml:"type,attr,omitempty"`
}

// RawXML preserves an arbitrary XML element (name + attributes + inner
// content) verbatim, for fields this proxy doesn't need to interpret.
type RawXML struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:",any,attr"`
	Content string     `xml:",innerxml"`
}

// Parse decodes a Torznab RSS response body.
func Parse(data []byte) (*RSS, error) {
	var rss RSS
	if err := xml.Unmarshal(data, &rss); err != nil {
		return nil, err
	}
	return &rss, nil
}

// Marshal re-encodes an RSS response, including the XML header.
func Marshal(rss *RSS) ([]byte, error) {
	out, err := xml.MarshalIndent(rss, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), out...), nil
}
