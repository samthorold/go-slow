// Package polite is the HTTP adapter Collection fetches through. It carries
// the politeness rules of the system design (Collection is polite by these
// rules): an honest User-Agent, conditional requests, one request at a time
// per host, backing off a host on 429 or 503, and robots.txt at most daily.
package polite

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
)

// UserAgent is go-slow's fixed, honest User-Agent, naming it and a contact
// address.
func UserAgent(contact string) string {
	return "go-slow/0.1 (private slow-news briefing; contact: " + contact + ")"
}

// Sleep waits for d, or until ctx is done. It is the Sleep main passes.
func Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Options are the Client's dependencies.
type Options struct {
	HTTP      *http.Client
	UserAgent string
	// Now is the clock; Sleep waits for d or until ctx is done. Both are
	// injected so tests control time.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
}

// robotsTTL is how long a host's robots.txt is kept before it is fetched
// again.
const robotsTTL = 24 * time.Hour

// Client fetches feeds politely. It is safe for concurrent use.
type Client struct {
	Options
	mu    sync.Mutex
	hosts map[string]*hostState
}

// hostState is what the Client knows about one host. Its lock is held for
// the whole of a request, so a host sees one request at a time.
type hostState struct {
	sync.Mutex
	robotsFetched time.Time
	robots        robots
	// backoffUntil is when the host, having answered 429 or 503, may be
	// asked again.
	backoffUntil time.Time
}

// New builds a Client.
func New(o Options) *Client { return &Client{Options: o, hosts: map[string]*hostState{}} }

func (c *Client) host(u *url.URL) *hostState {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := u.Scheme + "://" + u.Host
	h := c.hosts[key]
	if h == nil {
		h = &hostState{}
		c.hosts[key] = h
	}
	return h
}

// Fetch fetches rawURL, conditionally on v.
func (c *Client) Fetch(ctx context.Context, rawURL string, v collect.Validators) (collect.Fetched, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return collect.Fetched{}, fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	host := c.host(u)
	host.Lock()
	defer host.Unlock()

	if host.robotsFetched.IsZero() || c.Now().Sub(host.robotsFetched) >= robotsTTL {
		c.fetchRobots(ctx, u, host)
	}
	if !host.robots.allowed(u.EscapedPath()) {
		return collect.Fetched{}, fmt.Errorf("fetch %s: disallowed by robots.txt", rawURL)
	}

	h := http.Header{}
	if v.ETag != "" {
		h.Set("If-None-Match", v.ETag)
	}
	if v.LastModified != "" {
		h.Set("If-Modified-Since", v.LastModified)
	}
	res, err := c.get(ctx, host, rawURL, h)
	if err != nil {
		return collect.Fetched{}, err
	}
	defer func() { _ = res.Body.Close() }()
	switch res.StatusCode {
	case http.StatusNotModified:
		return collect.Fetched{NotModified: true, Validators: v}, nil
	case http.StatusOK:
	default:
		return collect.Fetched{}, fmt.Errorf("fetch %s: %s", rawURL, res.Status)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxFeed))
	if err != nil {
		return collect.Fetched{}, fmt.Errorf("read %s: %w", rawURL, err)
	}
	return collect.Fetched{Body: body, Validators: collect.Validators{
		ETag:         res.Header.Get("ETag"),
		LastModified: res.Header.Get("Last-Modified"),
	}}, nil
}

// fetchRobots fetches the host's robots.txt (RFC 9309). A 4xx means no
// rules; a 5xx or no answer means disallow everything, and is not kept, so
// the next fetch tries again.
func (c *Client) fetchRobots(ctx context.Context, u *url.URL, host *hostState) {
	robotsURL := &url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/robots.txt"}
	res, err := c.get(ctx, host, robotsURL.String(), nil)
	if err != nil {
		host.robots = robots{disallowAll: true}
		return
	}
	defer func() { _ = res.Body.Close() }()
	switch {
	case res.StatusCode >= 200 && res.StatusCode < 300:
		body, err := io.ReadAll(io.LimitReader(res.Body, maxRobots))
		if err != nil {
			host.robots = robots{disallowAll: true}
			return
		}
		host.robots = parseRobots(string(body))
	case res.StatusCode >= 400 && res.StatusCode < 500:
		host.robots = robots{}
	default:
		host.robots = robots{disallowAll: true}
		return
	}
	host.robotsFetched = c.Now()
}

// maxRobots is how much of a robots.txt is read: RFC 9309 asks crawlers to
// read at least 500 KiB.
const maxRobots = 512 << 10

// maxFeed is how much of a feed is read; a GOV.UK feed is tens of KiB.
const maxFeed = 16 << 20

// defaultBackoff is how long a host is left alone after a 429 or 503 that
// gives no usable Retry-After.
const defaultBackoff = 5 * time.Minute

// get makes one GET request to host, as go-slow, with the extra headers h.
// It first waits out any backoff the host asked for, and a 429 or 503 sets
// the next one. The caller holds the host's lock.
func (c *Client) get(ctx context.Context, host *hostState, rawURL string, h http.Header) (*http.Response, error) {
	if wait := host.backoffUntil.Sub(c.Now()); wait > 0 {
		if err := c.Sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	for k, vs := range h {
		req.Header[k] = vs
	}
	req.Header.Set("User-Agent", c.UserAgent)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	if res.StatusCode == http.StatusTooManyRequests || res.StatusCode == http.StatusServiceUnavailable {
		now := c.Now()
		host.backoffUntil = now.Add(retryAfter(res.Header.Get("Retry-After"), now))
	}
	return res, nil
}

// retryAfter reads a Retry-After header, in seconds or as an HTTP date, as
// a wait from now.
func retryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := t.Sub(now); d > 0 {
			return d
		}
		return 0
	}
	return defaultBackoff
}
