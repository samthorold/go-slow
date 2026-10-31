package web_test

import (
	"regexp"
	"strings"
	"testing"
)

// block is the block (a Span, a statement) whose body holds text: its body
// column and its margin, as rendered.
type block struct{ body, margin string }

// blockOf finds the block whose body holds text in page.
func blockOf(t *testing.T, page, text string) block {
	t.Helper()
	at := strings.Index(page, text)
	if at < 0 {
		t.Fatalf("no %q on the page:\n%s", text, page)
	}
	start := strings.LastIndex(page[:at], `class="block"`)
	if start < 0 {
		t.Fatalf("%q is in no block:\n%s", text, page)
	}
	rest := page[start:]
	// A column may carry classes of its own (class="block-body assessment").
	bodyAt := strings.Index(rest, `class="block-body`)
	marginAt := strings.Index(rest, `class="block-margin`)
	// A block ends where the next begins, or with its section.
	end := strings.Index(rest[1:], `class="block"`) + 1
	if sec := strings.Index(rest, "</section>"); end == 0 || (sec >= 0 && sec < end) {
		end = sec
	}
	if end < 0 {
		end = len(rest)
	}
	if bodyAt < 0 || marginAt < bodyAt || end < marginAt {
		t.Fatalf("the block holding %q has no body column then margin:\n%s", text, rest)
	}
	b := block{body: rest[bodyAt:marginAt], margin: rest[marginAt:end]}
	if !strings.Contains(b.body, text) {
		t.Fatalf("%q is not in its block's body column:\n%s", text, b.body)
	}
	return b
}

// How the fold looks is judged in a browser; what the server can be held to
// is that the page holds its blocks in one margined region with its state in
// the DOM, starting open on an inspect page, and one control, a button (so
// it takes keyboard focus and Enter and Space), carrying the behaviour that
// folds it.
func TestAnItemsPageHasOneControlThatFoldsTheMarginStartingOpen(t *testing.T) {
	store := openStore(t)
	readSpeech(t, store, speechReading)
	app, _ := newAppWithLog(t, saturday, store)

	page := itemPage(t, app, "PM speech in Scotland").body

	regions := regexp.MustCompile(`<div class="margined" data-margin="(open|folded)">`).FindAllStringSubmatch(page, -1)
	if len(regions) != 1 || regions[0][1] != "open" {
		t.Fatalf("want one margined region, open; got %v:\n%s", regions, page)
	}
	region := page[strings.Index(page, `class="margined"`):]
	for _, text := range []string{"good to be here in Motherwell."} {
		if !strings.Contains(region, text) {
			t.Errorf("%q is outside the margined region", text)
		}
	}
	controls := regexp.MustCompile(`<button type="button" class="margin-fold" data-margin-fold aria-expanded="true">[^<]+</button>`).FindAllString(page, -1)
	if len(controls) != 1 {
		t.Errorf("want one fold button, expanded, got %d:\n%s", len(controls), page)
	}

	src := regexp.MustCompile(`<script src="(/static/margin-fold\.js)"></script>`).FindStringSubmatch(page)
	if src == nil {
		t.Fatalf("the page does not load the fold behaviour:\n%s", page)
	}
	js := get(t, app, src[1])
	if js.status != 200 || !strings.Contains(js.header.Get("Content-Type"), "javascript") {
		t.Fatalf("GET %s: status %d, %s", src[1], js.status, js.header.Get("Content-Type"))
	}
	for _, want := range []string{"htmx:load", "data-margin-fold", "data-margin", "aria-expanded"} {
		if !strings.Contains(js.body, want) {
			t.Errorf("the fold behaviour does not mention %q:\n%s", want, js.body)
		}
	}
}
