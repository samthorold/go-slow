package polite_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/polite"
)

// host is a fake publisher. Each path answers with the handler set for it;
// every request is recorded.
type host struct {
	mu       sync.Mutex
	routes   map[string]http.HandlerFunc
	requests []*http.Request
}

func (h *host) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.requests = append(h.requests, r)
	route := h.routes[r.URL.Path]
	h.mu.Unlock()
	if route == nil {
		http.NotFound(w, r)
		return
	}
	route(w, r)
}

func (h *host) requestsFor(path string) []*http.Request {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []*http.Request
	for _, r := range h.requests {
		if r.URL.Path == path {
			out = append(out, r)
		}
	}
	return out
}

func text(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }
}

// clock is a fake clock. Sleeping advances it, and records how long.
type clock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Sleep(_ context.Context, d time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
	return nil
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func (c *clock) slept() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	var total time.Duration
	for _, d := range c.sleeps {
		total += d
	}
	return total
}

func newHost(t *testing.T, routes map[string]http.HandlerFunc) (*host, *httptest.Server) {
	t.Helper()
	h := &host{routes: routes}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return h, srv
}

func newClient(srv *httptest.Server, c *clock) *polite.Client {
	return polite.New(polite.Options{
		HTTP:      srv.Client(),
		UserAgent: polite.UserAgent("owner@example.org"),
		Now:       c.Now,
		Sleep:     c.Sleep,
	})
}

var start = time.Date(2026, time.October, 24, 7, 0, 0, 0, time.UTC)

func TestRobotsTxtIsFetchedAtMostOnceADay(t *testing.T) {
	h, srv := newHost(t, map[string]http.HandlerFunc{
		"/robots.txt": text("User-agent: *\nAllow: /\n"),
		"/feed.atom":  text("<feed/>"),
	})
	c := &clock{now: start}
	client := newClient(srv, c)
	fetch := func() {
		t.Helper()
		if _, err := client.Fetch(t.Context(), srv.URL+"/feed.atom", collect.Validators{}); err != nil {
			t.Fatal(err)
		}
	}

	fetch()
	c.advance(23 * time.Hour)
	fetch()
	if n := len(h.requestsFor("/robots.txt")); n != 1 {
		t.Errorf("robots.txt fetched %d times within a day, want 1", n)
	}

	c.advance(2 * time.Hour)
	fetch()
	if n := len(h.requestsFor("/robots.txt")); n != 2 {
		t.Errorf("robots.txt fetched %d times after a day, want 2", n)
	}
	if n := len(h.requestsFor("/feed.atom")); n != 3 {
		t.Errorf("feed fetched %d times, want 3", n)
	}
}

func TestAFeedRobotsTxtDisallowsIsNotFetched(t *testing.T) {
	h, srv := newHost(t, map[string]http.HandlerFunc{
		"/robots.txt": text("User-agent: *\nDisallow: /feeds/\n"),
		"/feeds/a":    text("<feed/>"),
	})
	client := newClient(srv, &clock{now: start})

	_, err := client.Fetch(t.Context(), srv.URL+"/feeds/a", collect.Validators{})

	if err == nil || !strings.Contains(err.Error(), "robots.txt") {
		t.Errorf("Fetch error = %v, want one naming robots.txt", err)
	}
	if n := len(h.requestsFor("/feeds/a")); n != 0 {
		t.Errorf("disallowed feed fetched %d times", n)
	}
}

func TestRobotsTxtRulesFollowRFC9309(t *testing.T) {
	status := func(code int) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
	}
	cases := []struct {
		name    string
		robots  http.HandlerFunc
		allowed bool
	}{
		{"a group naming go-slow wins over *",
			text("User-agent: *\nDisallow: /\n\nUser-agent: go-slow\nAllow: /\n"), true},
		{"a group naming go-slow can disallow",
			text("User-agent: *\nAllow: /\n\nUser-agent: Go-Slow\nDisallow: /feeds\n"), false},
		{"the longest match decides",
			text("User-agent: *\nDisallow: /feeds\nAllow: /feeds/a\n"), true},
		{"wildcards match", text("User-agent: *\nDisallow: /*.atom$\n"), false},
		{"other agents' rules do not apply", text("User-agent: GPTBot\nDisallow: /\n"), true},
		{"a missing robots.txt allows everything", status(http.StatusNotFound), true},
		{"a failing robots.txt disallows everything", status(http.StatusInternalServerError), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, srv := newHost(t, map[string]http.HandlerFunc{
				"/robots.txt":     tc.robots,
				"/feeds/a/x.atom": text("<feed/>"),
			})
			client := newClient(srv, &clock{now: start})

			_, err := client.Fetch(t.Context(), srv.URL+"/feeds/a/x.atom", collect.Validators{})

			if allowed := err == nil; allowed != tc.allowed {
				t.Errorf("allowed = %v (err %v), want %v", allowed, err, tc.allowed)
			}
		})
	}
}

func TestRetryAfterDelaysTheNextRequestToThatHost(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		retryAfter string
	}{
		{"429 in seconds", http.StatusTooManyRequests, "120"},
		{"503 as an HTTP date", http.StatusServiceUnavailable,
			start.Add(120 * time.Second).Format(http.TimeFormat)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			first := true
			var mu sync.Mutex
			h, srv := newHost(t, map[string]http.HandlerFunc{
				"/robots.txt": text("User-agent: *\nAllow: /\n"),
				"/feed.atom": func(w http.ResponseWriter, _ *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					if first {
						first = false
						w.Header().Set("Retry-After", tc.retryAfter)
						w.WriteHeader(tc.status)
						return
					}
					_, _ = io.WriteString(w, "<feed/>")
				},
			})
			c := &clock{now: start}
			client := newClient(srv, c)

			if _, err := client.Fetch(t.Context(), srv.URL+"/feed.atom", collect.Validators{}); err == nil {
				t.Fatal("Fetch succeeded on a backoff answer")
			}
			// Another URL on the same host waits too: the whole host backs off.
			_, _ = client.Fetch(t.Context(), srv.URL+"/other", collect.Validators{})
			if got := c.slept(); got < 120*time.Second {
				t.Errorf("waited %v before the next request, want at least 2m0s", got)
			}
			if _, err := client.Fetch(t.Context(), srv.URL+"/feed.atom", collect.Validators{}); err != nil {
				t.Fatal(err)
			}
			if got := c.slept(); got > 121*time.Second {
				t.Errorf("waited %v in all, want only the one backoff", got)
			}
			if n := len(h.requestsFor("/feed.atom")); n != 2 {
				t.Errorf("feed fetched %d times, want 2", n)
			}
		})
	}
}

func TestAHostSeesOneRequestAtATime(t *testing.T) {
	var mu sync.Mutex
	inFlight, most := 0, 0
	slow := func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		inFlight++
		most = max(most, inFlight)
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		_, _ = io.WriteString(w, "<feed/>")
	}
	_, srv := newHost(t, map[string]http.HandlerFunc{"/robots.txt": slow, "/a": slow, "/b": slow})
	client := newClient(srv, &clock{now: start})

	var wg sync.WaitGroup
	for _, path := range []string{"/a", "/b", "/a", "/b"} {
		wg.Go(func() {
			_, _ = client.Fetch(t.Context(), srv.URL+path, collect.Validators{})
		})
	}
	wg.Wait()

	if most != 1 {
		t.Errorf("host saw %d requests at once, want 1", most)
	}
}

func TestEveryRequestCarriesTheGoSlowUserAgentAndContact(t *testing.T) {
	h, srv := newHost(t, map[string]http.HandlerFunc{
		"/robots.txt": text("User-agent: *\nAllow: /\n"),
		"/feed.atom":  text("<feed/>"),
	})
	client := newClient(srv, &clock{now: start})

	if _, err := client.Fetch(t.Context(), srv.URL+"/feed.atom", collect.Validators{}); err != nil {
		t.Fatal(err)
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.requests) < 2 {
		t.Fatalf("host saw %d requests, want robots.txt and the feed", len(h.requests))
	}
	for _, r := range h.requests {
		ua := r.Header.Get("User-Agent")
		if !strings.Contains(ua, "go-slow") || !strings.Contains(ua, "owner@example.org") {
			t.Errorf("%s sent User-Agent %q, want go-slow and the contact", r.URL.Path, ua)
		}
	}
}
