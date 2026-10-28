package web_test

import (
	"regexp"
	"strings"
	"testing"
)

// block is the block (a Fact, a Claim) whose body holds text: its body
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
	bodyAt := strings.Index(rest, `class="block-body"`)
	marginAt := strings.Index(rest, `class="block-margin"`)
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

func TestAnItemsFactsAreTextInABodyColumnWithTheirNotesInAMargin(t *testing.T) {
	store := openStore(t)
	readSpeech(t, store, reading)
	app, _ := newAppWithLog(t, saturday, store)

	page := itemPage(t, app, "PM speech in Scotland").body
	fact := blockOf(t, page, "Andy Burnham spoke in Motherwell.")

	if strings.Contains(fact.body, "good to be here in Motherwell") {
		t.Errorf("the quote is in the body column, want it in the margin:\n%s", fact.body)
	}
	if !strings.Contains(fact.margin, "good to be here in Motherwell") {
		t.Errorf("the margin lacks the quote:\n%s", fact.margin)
	}
	if !regexp.MustCompile(`<a class="machine" href="/calls/\d+">read · call \d+</a>`).MatchString(fact.margin) {
		t.Errorf("the margin has no machine label \"read · call N\" linking the call:\n%s", fact.margin)
	}
}

// How the fold looks is judged in a browser; what the server can be held to
// is that the page holds its blocks in one margined region with its state in
// the DOM, starting open on an inspect page, and one control, a button (so
// it takes keyboard focus and Enter and Space), carrying the behaviour that
// folds it.
func TestAnItemsPageHasOneControlThatFoldsTheMarginStartingOpen(t *testing.T) {
	store := openStore(t)
	readSpeech(t, store, reading)
	app, _ := newAppWithLog(t, saturday, store)

	page := itemPage(t, app, "PM speech in Scotland").body

	regions := regexp.MustCompile(`<div class="margined" data-margin="(open|folded)">`).FindAllStringSubmatch(page, -1)
	if len(regions) != 1 || regions[0][1] != "open" {
		t.Fatalf("want one margined region, open; got %v:\n%s", regions, page)
	}
	region := page[strings.Index(page, `class="margined"`):]
	for _, text := range []string{"Andy Burnham spoke in Motherwell.", "Scotland is the true north."} {
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

func TestAnItemsClaimsAreTextInABodyColumnWithTheirNotesInAMargin(t *testing.T) {
	store := openStore(t)
	readSpeech(t, store, reading)
	app, _ := newAppWithLog(t, saturday, store)

	page := itemPage(t, app, "PM speech in Scotland").body
	claim := blockOf(t, page, "Scotland is the true north.")

	// Who made a Claim is part of what it says, so it stays with the text.
	if !strings.Contains(claim.body, "Andy Burnham") {
		t.Errorf("the body column does not say who made the Claim:\n%s", claim.body)
	}
	if strings.Contains(claim.body, "the true north<") || !strings.Contains(claim.margin, "the true north") {
		t.Errorf("the quote is not in the margin alone:\n%s%s", claim.body, claim.margin)
	}
	if !regexp.MustCompile(`<a class="machine" href="/calls/\d+">read · call \d+</a>`).MatchString(claim.margin) {
		t.Errorf("the margin has no machine label \"read · call N\":\n%s", claim.margin)
	}
}

func TestAFactsDateIsShownOnlyWhereItDiffersFromItsItems(t *testing.T) {
	store := openStore(t)
	// The speech is published on 22 October.
	readSpeech(t, store, `{
	  "facts": [{"statement": "Andy Burnham spoke in Motherwell.", "as_at": "2026-10-22", "quote": "good to be here in Motherwell"},
	            {"statement": "Andy Burnham visited Glasgow.", "as_at": "2026-10-15", "quote": "good to be here"}],
	  "claims": [], "mentions": []
	}`)
	app, _ := newAppWithLog(t, saturday, store)

	page := itemPage(t, app, "PM speech in Scotland").body

	if same := blockOf(t, page, "Andy Burnham spoke in Motherwell."); strings.Contains(same.body+same.margin, "October 2026") {
		t.Errorf("a Fact dated as its Item shows its date:\n%s%s", same.body, same.margin)
	}
	differs := blockOf(t, page, "Andy Burnham visited Glasgow.")
	if !strings.Contains(differs.margin, "15 October 2026") || strings.Contains(differs.body, "October") {
		t.Errorf("a Fact dated otherwise does not show its date in its margin:\n%s%s", differs.body, differs.margin)
	}
}
