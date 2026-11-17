package collect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"golang.org/x/net/html"
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
	// TextFromPage fetches the entry's own page (its link) and keeps its
	// article body, found as the Source's PageBody says, as the publisher's
	// HTML. Only for a publisher whose terms allow keeping its text and
	// whose feeds and APIs carry none (system design: Collection is polite
	// by these rules). A page with no article body is a failed fetch.
	TextFromPage
)

// PageBody says where a page's article body is: the element, among the
// children of the element whose id is Within, that has the class Class and
// not the class Except. What it holds is the text, as the publisher's
// HTML; the page around it (navigation, headings, sharing links) is not.
type PageBody struct {
	Within string
	Class  string
	Except string
}

// find is the page's article body, or nil if it has none.
func (b PageBody) find(page *html.Node) *html.Node {
	if b.Within == "" || b.Class == "" {
		return nil
	}
	for n := range page.Descendants() {
		if n.Type != html.ElementNode || attr(n, "id") != b.Within {
			continue
		}
		for c := range n.ChildNodes() {
			if c.Type == html.ElementNode && hasClass(c, b.Class) && (b.Except == "" || !hasClass(c, b.Except)) {
				return c
			}
		}
		return nil
	}
	return nil
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Namespace == "" && a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasClass(n *html.Node, class string) bool {
	return slices.Contains(strings.Fields(attr(n, "class")), class)
}

// articleBody is the publisher's HTML inside the page's article body.
func (b PageBody) articleBody(page []byte) (string, error) {
	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		return "", fmt.Errorf("parse page: %w", err)
	}
	body := b.find(doc)
	if body == nil {
		return "", fmt.Errorf("page has no article body (no child of #%s with class %s)", b.Within, b.Class)
	}
	var out strings.Builder
	for c := range body.ChildNodes() {
		if err := html.Render(&out, c); err != nil {
			return "", fmt.Errorf("render article body: %w", err)
		}
	}
	text := strings.TrimSpace(out.String())
	if text == "" {
		return "", errors.New("page's article body is empty")
	}
	return text, nil
}

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
	case TextFromPage:
		u, err := url.Parse(it.Link)
		if err != nil || u.Host == "" {
			return "", fmt.Errorf("entry %s has no link to fetch its page by", it.EntryID)
		}
		return it.Link, nil
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
	case TextFromPage:
		return s.PageBody.articleBody(body)
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
