package collect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Text is how a Source's entries get their full text (system design: An
// Item is collected with its full text, or not yet).
type Text int

const (
	// TextInEntry is a Source whose entries carry their text, if any,
	// themselves: nothing more is fetched.
	TextInEntry Text = iota
	// TextFromGOVUKContent fetches the GOV.UK Content API document for the
	// entry's path (https://www.gov.uk/api/content/<path>), whose body is
	// the text. GOV.UK's Atom entries carry only a summary.
	TextFromGOVUKContent
	// TextFromWrittenStatement fetches the written statement itself from
	// the Written Questions Service API. Its list carries only each
	// statement's opening.
	TextFromWrittenStatement
)

// fetchesText is whether the Source's entries get their text by a fetch of
// their own: it declares one, and its terms let the text be kept.
func (s Source) fetchesText() bool {
	return s.Text != TextInEntry && s.Keeps == KeepFullText
}

// textURL is where the full text of the Source's entry it is fetched from.
func (s Source) textURL(it Item) (string, error) {
	switch s.Text {
	case TextFromGOVUKContent:
		u, err := url.Parse(it.Link)
		if err != nil || u.Host == "" || u.Path == "" {
			return "", fmt.Errorf("entry %s has no GOV.UK link to fetch its text by", it.EntryID)
		}
		return (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/api/content" + u.Path}).String(), nil
	case TextFromWrittenStatement:
		u, err := url.Parse(s.FeedURL)
		if err != nil || u.Host == "" {
			return "", fmt.Errorf("feed %s has no host to fetch statements from", s.FeedURL)
		}
		return (&url.URL{Scheme: u.Scheme, Host: u.Host,
			Path: "/api/writtenstatements/statements/" + url.PathEscape(it.EntryID)}).String(), nil
	default:
		return "", fmt.Errorf("unknown way of fetching text %d", s.Text)
	}
}

// parseText reads the full text out of a fetched document.
func (s Source) parseText(body []byte) (string, error) {
	switch s.Text {
	case TextFromGOVUKContent:
		var doc struct {
			Details struct {
				Body string `json:"body"`
			} `json:"details"`
		}
		if err := json.Unmarshal(body, &doc); err != nil {
			return "", fmt.Errorf("parse Content API document: %w", err)
		}
		return strings.TrimSpace(doc.Details.Body), nil
	case TextFromWrittenStatement:
		var doc struct {
			Value struct {
				Text string `json:"text"`
			} `json:"value"`
		}
		if err := json.Unmarshal(body, &doc); err != nil {
			return "", fmt.Errorf("parse written statement: %w", err)
		}
		return strings.TrimSpace(doc.Value.Text), nil
	default:
		return "", fmt.Errorf("unknown way of fetching text %d", s.Text)
	}
}

// fetchText fetches the full text of the Source's entry it, politely,
// through f. A document with no text is not a failure: its summary stands
// in for it, as for an entry that carries none.
func fetchText(ctx context.Context, f Fetcher, s Source, it Item) (string, error) {
	u, err := s.textURL(it)
	if err != nil {
		return "", err
	}
	if neverFetched(u) {
		return "", fmt.Errorf("%s is on a host that blocks scripted clients", u)
	}
	got, err := f.Fetch(ctx, u, Validators{})
	if err != nil {
		return "", err
	}
	if got.NotModified {
		return "", errors.New("fetch " + u + ": not modified, though asked unconditionally")
	}
	return s.parseText(got.Body)
}
