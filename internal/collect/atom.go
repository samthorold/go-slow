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

// atomEntry is the part of an Atom (RFC 4287) entry an Item carries.
type atomEntry struct {
	ID        string     `xml:"id"`
	Title     string     `xml:"title"`
	Links     []atomLink `xml:"link"`
	Summary   string     `xml:"summary"`
	Content   string     `xml:"content"`
	Published string     `xml:"published"`
	Updated   string     `xml:"updated"`
	Inner     []byte     `xml:",innerxml"`
}

type atomLink struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
}

// parseAtom reads an Atom feed's entries as Items (without Source or
// collection time). An entry with no id is skipped: without one, a second
// run could not tell it was already collected.
func parseAtom(body []byte) ([]Item, error) {
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
		if !ok || start.Name.Local != "entry" {
			continue
		}
		var e atomEntry
		if err := dec.DecodeElement(&e, &start); err != nil {
			return nil, fmt.Errorf("parse feed entry: %w", err)
		}
		id := strings.TrimSpace(e.ID)
		if id == "" {
			continue
		}
		updated := parseTime(e.Updated)
		published := parseTime(e.Published)
		if published.IsZero() {
			// Atom requires updated but not published; GOV.UK sends only
			// updated, which is when the entry was published to the feed.
			published = updated
		}
		items = append(items, Item{
			EntryID:   id,
			Title:     strings.TrimSpace(e.Title),
			Link:      e.alternate(),
			Summary:   strings.TrimSpace(e.Summary),
			Content:   strings.TrimSpace(e.Content),
			Published: published,
			Updated:   updated,
			Raw:       "<entry>" + string(e.Inner) + "</entry>",
		})
	}
}

// alternate is the entry's link to its original: rel="alternate", which is
// also what a link with no rel means.
func (e atomEntry) alternate() string {
	for _, l := range e.Links {
		if l.Rel == "" || l.Rel == "alternate" {
			return l.Href
		}
	}
	return ""
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}
	}
	return t
}
