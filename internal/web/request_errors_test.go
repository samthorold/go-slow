package web_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// requestErrors matches the layout's one region for request errors, as served.
var requestErrors = regexp.MustCompile(`<div id="request-errors" role="alert"[^>]*>([^<]*)</div>`)

func TestEveryPageHasAnEmptyRegionForRequestErrors(t *testing.T) {
	app := newApp(t, saturday)

	for _, path := range []string{"/", "/briefings", "/stories", "/items", "/topics", "/feedback", "/register"} {
		res := get(t, app, path)
		if res.status != http.StatusOK {
			t.Fatalf("GET %s: status = %d, want 200", path, res.status)
		}
		regions := requestErrors.FindAllStringSubmatch(res.body, -1)
		if len(regions) != 1 {
			t.Fatalf("GET %s: %d regions for request errors, want 1:\n%s", path, len(regions), res.body)
		}
		if regions[0][1] != "" {
			t.Errorf("GET %s: region for request errors holds %q, want it empty", path, regions[0][1])
		}
	}
}

// The behaviour itself runs only in a browser; what the server can be held to
// is that every page loads it and that it is served, listening for the events
// htmx fires when a request fails and when one succeeds.
func TestEveryPageLoadsTheBehaviourThatShowsRequestErrors(t *testing.T) {
	app := newApp(t, saturday)
	res := get(t, app, "/")

	src := regexp.MustCompile(`<script src="(/static/request-errors\.js)"></script>`).FindStringSubmatch(res.body)
	if src == nil {
		t.Fatalf("page loads no request-errors behaviour:\n%s", res.body)
	}
	js := get(t, app, src[1])
	if js.status != http.StatusOK {
		t.Fatalf("GET %s: status = %d, want 200", src[1], js.status)
	}
	if ct := js.header.Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("GET %s: Content-Type = %q, want JavaScript", src[1], ct)
	}
	for _, event := range []string{"htmx:responseError", "htmx:sendError", "htmx:afterRequest", "request-errors"} {
		if !strings.Contains(js.body, event) {
			t.Errorf("GET %s does not mention %q:\n%s", src[1], event, js.body)
		}
	}
}
