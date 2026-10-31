package web_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/feedback"
	"github.com/samthorold/go-slow/internal/interest"
	"github.com/samthorold/go-slow/internal/sqlite"
	"github.com/samthorold/go-slow/internal/web"
)

// newApp builds the app the way main does, with a real SQLite store in a
// temporary directory and a fixed clock.
func newApp(t *testing.T, now time.Time) http.Handler {
	t.Helper()
	app, _ := newAppWithLog(t, now, openStore(t))
	return app
}

func openStore(t *testing.T) *sqlite.Store {
	t.Helper()
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "go-slow.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// newAppWithLog builds the app on the given store and returns what it logs.
func newAppWithLog(t *testing.T, now time.Time, store *sqlite.Store) (http.Handler, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	return web.New(appDeps(now, store, &logs)), &logs
}

// appDeps are the app's dependencies as main builds them, on the given store,
// logging to logs.
func appDeps(now time.Time, store *sqlite.Store, logs *bytes.Buffer) web.Deps {
	return web.Deps{
		Briefings:   store,
		Items:       store,
		Readings:    store,
		Cuts:        store,
		Relations:   store,
		Topics:      interest.New(store),
		Stories:     store,
		Pieces:      feedback.NewPieces(store),
		Given:       store,
		Register:    store,
		SundayReads: store,
		Calls:       store,
		Sources:     []collect.Source{number10},
		BriefingAt:  7 * time.Hour,
		Now:         func() time.Time { return now },
		Log:         slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
}

// response is what a browser would see of one request.
type response struct {
	status int
	header http.Header
	body   string
}

func get(t *testing.T, app http.Handler, path string) response {
	t.Helper()
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
	return response{rec.Code, rec.Header(), rec.Body.String()}
}

var saturday = time.Date(2026, time.October, 24, 7, 30, 0, 0, time.UTC)

func TestTodaysBriefingSaysThereIsNoBriefingYet(t *testing.T) {
	app := newApp(t, saturday)

	res := get(t, app, "/")

	if res.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.status)
	}
	if !strings.Contains(res.body, "There is no Briefing yet") {
		t.Errorf("body does not say there is no Briefing yet:\n%s", res.body)
	}
}

func TestTodaysBriefingIsAFullPageThatRequiresTheVendoredHtmx(t *testing.T) {
	app := newApp(t, saturday)

	res := get(t, app, "/")

	if ct := res.header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	for _, want := range []string{"<!doctype html>", "<title>", `<body hx-boost="true">`} {
		if !strings.Contains(res.body, want) {
			t.Errorf("page lacks %q:\n%s", want, res.body)
		}
	}

	src := regexp.MustCompile(`<script src="([^"]*/htmx-[0-9.]+\.min\.js)"`).FindStringSubmatch(res.body)
	if src == nil {
		t.Fatalf("page loads no vendored htmx:\n%s", res.body)
	}
	js := get(t, app, src[1])
	if js.status != http.StatusOK {
		t.Fatalf("GET %s: status = %d, want 200", src[1], js.status)
	}
	if ct := js.header.Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("GET %s: Content-Type = %q, want JavaScript", src[1], ct)
	}
	if !strings.Contains(js.body, "htmx") {
		t.Errorf("GET %s did not serve htmx", src[1])
	}
}

func TestAnUnknownPathIsNotFound(t *testing.T) {
	app := newApp(t, saturday)

	res := get(t, app, "/no-such-page")

	if res.status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", res.status)
	}
}

func TestAFailingStoreIsAServerErrorAndIsLogged(t *testing.T) {
	store := openStore(t)
	app, logs := newAppWithLog(t, saturday, store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	res := get(t, app, "/")

	if res.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", res.status)
	}
	if !strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Errorf("no error logged; logs:\n%s", logs)
	}
}

func TestTodaysBriefingIsDatedTodayInTheClocksTimeZone(t *testing.T) {
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	// 23:30 UTC on Friday is already Saturday in London (BST).
	lateFriday := time.Date(2026, time.October, 23, 23, 30, 0, 0, time.UTC).In(london)
	app := newApp(t, lateFriday)

	res := get(t, app, "/")

	if !strings.Contains(res.body, "Saturday 24 October 2026") {
		t.Errorf("body is not dated Saturday 24 October 2026:\n%s", res.body)
	}
}
