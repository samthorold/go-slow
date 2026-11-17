package collect

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// rssItem is the part of an RSS 2.0 item an Item carries. Its full text is
// the RSS content module's encoded element.
type rssItem struct {
	Title       string    `xml:"title"`
	Links       []rssText `xml:"link"`
	GUID        string    `xml:"guid"`
	PubDate     string    `xml:"pubDate"`
	Description string    `xml:"description"`
	Encoded     string    `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
	Inner       []byte    `xml:",innerxml"`
}

// rssText is an element whose namespace matters: RSS's own link has none,
// while an atom:link in the same item is something else.
type rssText struct {
	XMLName xml.Name
	Value   string `xml:",chardata"`
}

// parseRSS reads an RSS 2.0 feed's items as Items (without Source or
// collection time). An item's guid is its entry id; RSS makes the guid
// optional, so an item without one is known by its link, and one with
// neither is skipped: a second run could not tell it was already collected.
func parseRSS(body []byte) ([]Item, error) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	var items []Item
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return items, nil
			}
			return nil, fmt.Errorf("parse feed: %w", err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "item" || start.Name.Space != "" {
			continue
		}
		var e rssItem
		if err := dec.DecodeElement(&e, &start); err != nil {
			return nil, fmt.Errorf("parse feed item: %w", err)
		}
		link := e.link()
		id := strings.TrimSpace(e.GUID)
		if id == "" {
			id = link
		}
		if id == "" {
			continue
		}
		items = append(items, Item{
			EntryID:   id,
			Title:     strings.TrimSpace(e.Title),
			Link:      link,
			Summary:   strings.TrimSpace(e.Description),
			Content:   strings.TrimSpace(e.Encoded),
			Published: parseRFC822(e.PubDate),
			Raw:       "<item>" + string(e.Inner) + "</item>",
		})
	}
}

// link is the item's own link, not an atom:link beside it.
func (e rssItem) link() string {
	for _, l := range e.Links {
		if l.XMLName.Space == "" {
			return strings.TrimSpace(l.Value)
		}
	}
	return ""
}

// rfc822Zones are the zone names RFC 822 allows in a date. Go's time.Parse
// reads a zone name it doesn't know as UTC, so these are mapped by hand.
var rfc822Zones = map[string]int{
	"UT": 0, "GMT": 0, "Z": 0,
	"EST": -5, "EDT": -4, "CST": -6, "CDT": -5,
	"MST": -7, "MDT": -6, "PST": -8, "PDT": -7,
}

// parseRFC822 reads an RSS pubDate (RFC 822, with four-digit years as RSS
// feeds write them), or returns the zero time.
func parseRFC822(s string) time.Time {
	s = strings.TrimSpace(s)
	if _, rest, ok := strings.Cut(s, ", "); ok {
		s = rest // the day of the week is optional, and adds nothing
	}
	for _, layout := range []string{"2 Jan 2006 15:04:05 -0700", "2 Jan 2006 15:04 -0700"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	i := strings.LastIndexByte(s, ' ')
	if i < 0 {
		return time.Time{}
	}
	hours, ok := rfc822Zones[s[i+1:]]
	if !ok {
		return time.Time{}
	}
	zone := time.FixedZone(s[i+1:], hours*60*60)
	for _, layout := range []string{"2 Jan 2006 15:04:05", "2 Jan 2006 15:04"} {
		if t, err := time.ParseInLocation(layout, s[:i], zone); err == nil {
			return t
		}
	}
	return time.Time{}
}
