package editor_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// readingByTitle answers each reading request with the reading kept for the
// Item it shows, by title, and one judging nothing for any other.
type readingByTitle map[string]string

func (m readingByTitle) Complete(_ context.Context, req editor.Request) (editor.Response, error) {
	out := goodReadingOfRelease
	for title, reading := range m {
		if strings.HasPrefix(req.Input, "Title: "+title+"\n") {
			out = reading
		}
	}
	return editor.Response{Output: json.RawMessage(out), Model: "fake"}, nil
}

// readAt reads every Item awaiting reading in store at at, each Item's
// reading as readings has it.
func readAt(t *testing.T, store *sqlite.Store, readings readingByTitle, at time.Time) {
	t.Helper()
	ed := editor.New(editor.Deps{Store: store, Model: readings, Log: slog.New(slog.DiscardHandler)})
	if err := ed.ReadWaiting(t.Context(), at); err != nil {
		t.Fatal(err)
	}
}

// withRelease adds the release to the Record, collected and cut at at:
// S1 "Andy Burnham said:", S2 "“This is fantastic news for the city.", S3
// "Derby has waited long enough.”" and S4 "He visited the site on Monday."
func withRelease(t *testing.T, store *sqlite.Store, at time.Time) {
	t.Helper()
	if _, err := store.AddItems(t.Context(), []collect.Item{{
		SourceID: release.SourceID, EntryID: release.EntryID, Title: release.Title, Link: "https://www.gov.uk/release",
		Content: release.Content, Published: release.Published, Attribution: collect.OGL, CollectedAt: at,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := cut.New(store).CutNew(t.Context(), at); err != nil {
		t.Fatal(err)
	}
}

// releaseHydrated is the release's Story as a run is shown it once read by
// releaseReading, after the speech's two Spans: its S1, Rejected, left out;
// "the city" and "He" read as what they refer to; its S2 and S3 Andy
// Burnham's words. The Spans keep the numbers they have as written.
const releaseHydrated = `Story from "GBR headquarters in Derby", published 2026-10-15
S4 (spoken by Andy Burnham): “This is fantastic news for [Derby].
S5 (spoken by Andy Burnham): Derby has waited long enough.”
S6: [Andy Burnham] visited the site on Monday.
`

func TestARunIsShownAHydratedSpanAsReadLeavingOutARejectedOne(t *testing.T) {
	store := record(t)
	withRelease(t, store, saturday)
	readAt(t, store, readingByTitle{release.Title: releaseReading}, saturday)
	batcher := &fakeBatcher{}
	runAt(t, weavingBatches(store, batcher), saturday)

	in := batcher.submitted[0][0].Request.Input
	if !strings.Contains(in, releaseHydrated) {
		t.Errorf("the run was shown:\n%s\nwant it to hold:\n%s", in, releaseHydrated)
	}
}

// releaseRead is a Record holding the speech and the release, the release
// read by releaseReading, and the runner whose Saturday run has been sent.
func releaseRead(t *testing.T) (*sqlite.Store, *fakeBatcher, *editor.Batches) {
	t.Helper()
	store := record(t)
	withRelease(t, store, saturday)
	readAt(t, store, readingByTitle{release.Title: releaseReading}, saturday)
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)
	runAt(t, b, saturday)
	return store, batcher, b
}

func TestACitationOfAHydratedSpanHoldsItsAddressAndItsWordsAsWritten(t *testing.T) {
	store, batcher, b := releaseRead(t)
	answerLast(batcher, `{"stories": [{"editor_spans": [{"body": "Derby waited.", "cites": [4, 6]}]}]}`)
	runAt(t, b, saturday.Add(time.Hour))

	stories := madeStories(t, store)
	if len(stories) != 1 {
		t.Fatalf("made %d Stories, want 1", len(stories))
	}
	cites := stories[0].Spans[0].Cites
	spans := cut.Sentences(release.Content)
	if len(cites) != 2 || cites[0].Address.EntryID != release.EntryID || cites[0].Address.Span != spans[1] ||
		cites[1].Address.Span != spans[3] || cites[1].Words != "He visited the site on Monday." {
		t.Errorf("cites %+v, want the release's S2 and S4, as written", cites)
	}
}

// Nothing waits for hydration: a run is sent showing the release as
// written, and the release is read before its result arrives. The result is
// applied to what the run was shown, S3 included, though the release's
// reading has Rejected it since.
func TestAResultIsAppliedToWhatTheRunWasShownWhenASpanIsHydratedAfterItsRequestWent(t *testing.T) {
	store := record(t)
	withRelease(t, store, saturday)
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)
	runAt(t, b, saturday)
	if in := batcher.submitted[0][0].Request.Input; !strings.Contains(in, "S3: Andy Burnham said:\n") {
		t.Fatalf("the run was not shown the release as written:\n%s", in)
	}
	readAt(t, store, readingByTitle{release.Title: releaseReading}, saturday.Add(30*time.Minute))
	answerLast(batcher, `{"stories": [{"editor_spans": [{"body": "He said so.", "cites": [3, 6]}]}]}`)
	runAt(t, b, saturday.Add(time.Hour))

	stories := madeStories(t, store)
	if len(stories) != 1 {
		calls, _ := store.CallsOn(t.Context(), "weaving", "2026-10-24")
		t.Fatalf("made %d Stories, want the result applied; calls %+v", len(stories), calls)
	}
	spans := cut.Sentences(release.Content)
	if cites := stories[0].Spans[0].Cites; len(cites) != 2 || cites[0].Address.Span != spans[0] || cites[1].Address.Span != spans[3] {
		t.Errorf("cites %+v, want the release's S1 and S4", cites)
	}
}

// The Spans an earlier Editor's Span cites are read hydrated too, and shown
// whether or not they are Rejected, since the Editor's Span rests on them.
func TestTheSpansTheEditorsStoriesCiteAreShownHydrated(t *testing.T) {
	store, batcher, b := releaseRead(t)
	answerLast(batcher, `{"stories": [{"editor_spans": [{"body": "Derby waited.", "cites": [4, 6]}]}]}`)
	runAt(t, b, saturday.Add(time.Hour))
	sunday := saturday.Add(24 * time.Hour)
	cutAt(t, store, sunday)
	runAt(t, b, sunday)

	want := `Story the Editor made, 2026-10-24
S1 (from "GBR headquarters in Derby", spoken by Andy Burnham): “This is fantastic news for [Derby].
S2 (from "GBR headquarters in Derby"): [Andy Burnham] visited the site on Monday.
S3 (the Editor's, citing S1, S2): Derby waited.
`
	if in := batcher.submitted[len(batcher.submitted)-1][0].Request.Input; !strings.HasPrefix(in, want) {
		t.Errorf("Sunday's run was shown:\n%s\nwant it to start:\n%s", in, want)
	}
}

// A Span an earlier Editor's Span cites, hydrated after the run showing it
// went, leaves the run's result applied to what it was shown.
func TestAResultIsAppliedWhenASpanTheEditorCitedIsHydratedAfterItsRequestWent(t *testing.T) {
	store := record(t)
	withRelease(t, store, saturday)
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)
	runAt(t, b, saturday)
	answerLast(batcher, `{"stories": [{"editor_spans": [{"body": "Derby waited.", "cites": [4, 6]}]}]}`)
	runAt(t, b, saturday.Add(time.Hour))
	sunday := saturday.Add(24 * time.Hour)
	cutAt(t, store, sunday)
	runAt(t, b, sunday)
	readAt(t, store, readingByTitle{release.Title: releaseReading}, sunday.Add(30*time.Minute))
	answerLast(batcher, `{"stories": [{"editor_spans": [{"body": "Ferries and Derby.", "cites": [2, 4]}]}]}`)
	runAt(t, b, sunday.Add(time.Hour))

	stories := madeStories(t, store)
	if len(stories) != 2 {
		calls, _ := store.CallsOn(t.Context(), "weaving", "2026-10-25")
		t.Fatalf("made %d Stories, want Sunday's applied; calls %+v", len(stories), calls)
	}
	if cites := stories[0].Spans[0].Cites; len(cites) != 2 || cites[0].Address.Span != cut.Sentences(release.Content)[3] ||
		cites[1].Address.EntryID != ferries.EntryID {
		t.Errorf("cites %+v, want the release's S4 and the ferries", cites)
	}
}

func TestACitationOfARejectedSpanLeftOutIsRefused(t *testing.T) {
	store, batcher, b := releaseRead(t)
	answerLast(batcher, `{"stories": [{"editor_spans": [{"body": "Burnham said.", "cites": [3]}]}]}`)
	runAt(t, b, saturday.Add(time.Hour))

	if got := madeStories(t, store); len(got) != 0 {
		t.Errorf("made %+v citing a Span not shown", got)
	}
	calls, err := store.CallsOn(t.Context(), "weaving", "2026-10-24")
	if err != nil || len(calls) != 1 || calls[0].Outcome != editor.Refused || !strings.Contains(calls[0].Why, "cites S3, which was not shown") {
		t.Errorf("calls %+v (%v), want one refused for citing S3", calls, err)
	}
}
