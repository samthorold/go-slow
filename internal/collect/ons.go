package collect

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// onsSite is where the ONS publishes a release's page; the API gives its
// path.
const onsSite = "https://www.ons.gov.uk"

type onsReleases struct {
	Releases []json.RawMessage `json:"releases"`
}

type onsRelease struct {
	URI         string `json:"uri"`
	Description struct {
		Title       string `json:"title"`
		Summary     string `json:"summary"`
		ReleaseDate string `json:"release_date"`
	} `json:"description"`
}

// parseONSReleases reads the ONS API's release search. A release with no uri
// is skipped: the uri is its identity.
func parseONSReleases(body []byte) ([]Item, error) {
	var page onsReleases
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, fmt.Errorf("parse ONS releases: %w", err)
	}
	var items []Item
	for _, raw := range page.Releases {
		var r onsRelease
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("parse ONS release: %w", err)
		}
		uri := strings.TrimSpace(r.URI)
		if uri == "" {
			continue
		}
		released := parseTime(r.Description.ReleaseDate)
		items = append(items, Item{
			EntryID:   uri,
			Title:     strings.TrimSpace(r.Description.Title),
			Link:      onsSite + uri,
			Summary:   strings.TrimSpace(r.Description.Summary),
			Published: released,
			Updated:   released,
			Raw:       string(raw),
		})
	}
	return items, nil
}

// parseONSCalendar reads the ONS release search's upcoming releases as
// scheduled events. The calendar says when a release is due, not when the
// entry was announced, so an entry has no publication time. Its identity
// includes the date it is due, so a release moved to another date is a new
// Item and the Record keeps both.
func parseONSCalendar(body []byte) ([]Item, error) {
	items, err := parseONSReleases(body)
	if err != nil {
		return nil, err
	}
	for i := range items {
		it := &items[i]
		it.Due = it.Published
		it.EntryID += "#" + it.Due.UTC().Format(time.RFC3339)
		it.Published, it.Updated = time.Time{}, time.Time{}
	}
	return items, nil
}
