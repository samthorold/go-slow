// Package wikidata is the Wikidata adapter: the editor's Wikidata port over
// Wikidata's Action API (wbsearchentities and wbgetentities).
//
// It is polite by Wikimedia's own rules (sources research, Reference
// sources): an honest User-Agent naming go-slow and a contact address
// (Wikimedia User-Agent Policy), requests one at a time and a fixed gap
// apart (API:Etiquette asks for serial requests; unauthenticated bots with a
// compliant User-Agent are limited to 200 a minute), maxlag on every
// request, as a non-interactive client should, and backing off for as long
// as Retry-After says on a 429, a 503 or a maxlag error. A lookup answered
// maxlag is sent once more after the backoff, as Wikimedia asks (wait and
// retry); a second maxlag answer, or any other error, fails it. robots.txt does not
// govern the API, which is meant for programs; the API's usage policy does.
package wikidata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/polite"
)

// Endpoint is Wikidata's Action API.
const Endpoint = "https://www.wikidata.org/w/api.php"

// Gap is the least time between two requests: a minute's requests stay far
// under Wikimedia's 200 for a compliant bot.
const Gap = time.Second

// searchLimit is how many items a search looks at: Wikidata ranks them, and
// a match is the best ranked one that fits.
const searchLimit = 5

// maxlag is the replication lag, in seconds, past which Wikidata asks a
// non-interactive client to wait (API:Etiquette).
const maxlag = "5"

// Options are the Client's dependencies.
type Options struct {
	HTTP *http.Client
	// Endpoint is the Action API's URL.
	Endpoint  string
	UserAgent string
	// Gap is the least time between two requests.
	Gap time.Duration
	// Now and Sleep are the clock, injected so tests control time.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
}

// Client is Wikidata. It is safe for concurrent use, and makes one request
// at a time.
type Client struct {
	Options
	mu           sync.Mutex
	last         time.Time
	backoffUntil time.Time
}

// New builds a Client.
func New(o Options) *Client { return &Client{Options: o} }

var _ editor.Wikidata = (*Client)(nil)

// Search is the items wbsearchentities finds for text, in English, best
// first.
func (c *Client) Search(ctx context.Context, text string) ([]string, error) {
	var res struct {
		Search []struct {
			ID string `json:"id"`
		} `json:"search"`
	}
	if err := c.get(ctx, url.Values{
		"action": {"wbsearchentities"}, "search": {text}, "language": {"en"}, "uselang": {"en"},
		"type": {"item"}, "limit": {strconv.Itoa(searchLimit)},
	}, &res); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(res.Search))
	for _, s := range res.Search {
		ids = append(ids, s.ID)
	}
	return ids, nil
}

// Items are the items with these ids, in the order asked; an id Wikidata
// has no item for is left out.
func (c *Client) Items(ctx context.Context, ids []string) ([]editor.WikidataItem, error) {
	ents, err := c.entities(ctx, ids, "info|labels|aliases|claims")
	if err != nil {
		return nil, err
	}
	var out []editor.WikidataItem
	for _, id := range ids {
		e, ok := ents[id]
		if !ok || e.Missing != nil {
			continue
		}
		it := editor.WikidataItem{ID: e.ID, Label: e.Labels["en"].Value, Revision: e.LastRevID}
		for _, a := range e.Aliases["en"] {
			it.Aliases = append(it.Aliases, a.Value)
		}
		for _, s := range e.Claims["P31"] {
			if v := s.Mainsnak.item(); v != "" && s.Rank != "deprecated" {
				it.InstanceOf = append(it.InstanceOf, v)
			}
		}
		for _, s := range e.Claims["P39"] {
			if t, ok := s.tenure(); ok {
				t.Holder, t.Office = e.ID, s.Mainsnak.item()
				it.Positions = append(it.Positions, t)
			}
		}
		for _, s := range e.Claims["P1308"] {
			if t, ok := s.tenure(); ok {
				t.Holder, t.Office = s.Mainsnak.item(), e.ID
				it.Holders = append(it.Holders, t)
			}
		}
		out = append(out, it)
	}
	return out, nil
}

// Labels are the English labels of these items, by id.
func (c *Client) Labels(ctx context.Context, ids []string) (map[string]string, error) {
	ents, err := c.entities(ctx, ids, "labels")
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for id, e := range ents {
		if l := e.Labels["en"].Value; l != "" {
			out[id] = l
		}
	}
	return out, nil
}

// maxIDs is how many ids wbgetentities takes in one request.
const maxIDs = 50

func (c *Client) entities(ctx context.Context, ids []string, props string) (map[string]entity, error) {
	out := map[string]entity{}
	for len(ids) > 0 {
		n := min(len(ids), maxIDs)
		var res struct {
			Entities map[string]entity `json:"entities"`
		}
		if err := c.get(ctx, url.Values{
			"action": {"wbgetentities"}, "ids": {strings.Join(ids[:n], "|")},
			"props": {props}, "languages": {"en"},
		}, &res); err != nil {
			return nil, err
		}
		for id, e := range res.Entities {
			out[id] = e
		}
		ids = ids[n:]
	}
	return out, nil
}

// entity is an item as wbgetentities gives it.
type entity struct {
	ID        string                 `json:"id"`
	Missing   *string                `json:"missing"`
	LastRevID int64                  `json:"lastrevid"`
	Labels    map[string]text        `json:"labels"`
	Aliases   map[string][]text      `json:"aliases"`
	Claims    map[string][]statement `json:"claims"`
}

type text struct {
	Value string `json:"value"`
}

type statement struct {
	Mainsnak   snak              `json:"mainsnak"`
	Qualifiers map[string][]snak `json:"qualifiers"`
	Rank       string            `json:"rank"`
}

type snak struct {
	Datavalue *struct {
		Value json.RawMessage `json:"value"`
	} `json:"datavalue"`
}

// item is the item a snak's value names, or empty.
func (s snak) item() string {
	if s.Datavalue == nil {
		return ""
	}
	var v struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(s.Datavalue.Value, &v)
	return v.ID
}

// time is a snak's time value, and whether it is given to the day.
func (s snak) time() (time.Time, bool, bool) {
	if s.Datavalue == nil {
		return time.Time{}, false, false
	}
	var v struct {
		Time      string `json:"time"`
		Precision int    `json:"precision"`
	}
	if json.Unmarshal(s.Datavalue.Value, &v) != nil {
		return time.Time{}, false, false
	}
	t, ok := parseTime(v.Time)
	return t, v.Precision >= dayPrecision, ok
}

// dayPrecision is Wikidata's precision for a time given to the day.
const dayPrecision = 11

// parseTime reads a Wikidata time ("+2026-07-20T00:00:00Z"). A month or day
// not known is 00, and is taken as the first.
func parseTime(v string) (time.Time, bool) {
	v = strings.TrimPrefix(v, "+")
	date, _, _ := strings.Cut(v, "T")
	parts := strings.Split(date, "-")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	y, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	d, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return time.Time{}, false
	}
	return time.Date(y, time.Month(max(m, 1)), max(d, 1), 0, 0, 0, 0, time.UTC), true
}

// tenure is a position-held or officeholder statement's start and end
// (P580, P582). A deprecated statement, or one with no value, is none.
func (s statement) tenure() (editor.Tenure, bool) {
	if s.Rank == "deprecated" || s.Mainsnak.item() == "" {
		return editor.Tenure{}, false
	}
	var t editor.Tenure
	if q := s.Qualifiers["P580"]; len(q) > 0 {
		t.Start, t.StartToTheDay, _ = q[0].time()
	}
	if q := s.Qualifiers["P582"]; len(q) > 0 {
		t.End, _, _ = q[0].time()
	}
	return t, true
}

// maxResponse is how much of a response is read; a politician's every
// statement is a few hundred KiB.
const maxResponse = 16 << 20

// get asks the API with the parameters p, decoding the answer into v. A
// maxlag answer is asked again once, after its backoff (API:Etiquette: wait
// and retry); a second, or any other error, fails. The lock is held across
// both, so the retry is still one request at a time.
func (c *Client) get(ctx context.Context, p url.Values, v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	lagged, err := c.send(ctx, p, v)
	if lagged {
		_, err = c.send(ctx, p, v)
	}
	return err
}

// send makes one request, waiting for the gap since the last request, and
// any backoff asked for, first, and says whether the API answered maxlag.
// The caller holds c.mu.
func (c *Client) send(ctx context.Context, p url.Values, v any) (bool, error) {
	next := c.backoffUntil
	if !c.last.IsZero() {
		next = later(next, c.last.Add(c.Gap))
	}
	if wait := next.Sub(c.Now()); wait > 0 {
		if err := c.Sleep(ctx, wait); err != nil {
			return false, err
		}
	}
	p.Set("format", "json")
	p.Set("maxlag", maxlag)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Endpoint+"?"+p.Encode(), nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	res, err := c.HTTP.Do(req)
	c.last = c.Now()
	if err != nil {
		return false, fmt.Errorf("wikidata %s: %w", p.Get("action"), err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == http.StatusTooManyRequests || res.StatusCode == http.StatusServiceUnavailable {
		c.backoffUntil = c.last.Add(polite.RetryAfter(res.Header.Get("Retry-After"), c.last))
	}
	if res.StatusCode != http.StatusOK {
		return false, fmt.Errorf("wikidata %s: %s", p.Get("action"), res.Status)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxResponse))
	if err != nil {
		return false, fmt.Errorf("wikidata %s: %w", p.Get("action"), err)
	}
	var apiErr struct {
		Error *struct {
			Code string `json:"code"`
			Info string `json:"info"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &apiErr); err != nil {
		return false, fmt.Errorf("wikidata %s: %w", p.Get("action"), err)
	}
	if e := apiErr.Error; e != nil {
		lagged := e.Code == "maxlag"
		if lagged {
			c.backoffUntil = c.last.Add(polite.RetryAfter(res.Header.Get("Retry-After"), c.last))
		}
		return lagged, fmt.Errorf("wikidata %s: %w: %s: %s", p.Get("action"), errAPI, e.Code, e.Info)
	}
	return false, json.Unmarshal(body, v)
}

// errAPI is an error the API answered with.
var errAPI = errors.New("API error")

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
