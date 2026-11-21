package editor_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// hydrationByTitle answers each hydration request with the hydration kept for the
// Item it shows, by title, and one judging nothing for any other.
type hydrationByTitle map[string]string

func (m hydrationByTitle) Complete(_ context.Context, req editor.Request) (editor.Response, error) {
	out := goodHydrationOfRelease
	for title, hydration := range m {
		if strings.HasPrefix(req.Input, "Title: "+title+"\n") {
			out = hydration
		}
	}
	return editor.Response{Output: json.RawMessage(out), Model: "fake"}, nil
}

// hydrateAt hydrates every Item awaiting hydration in store at at, each Item's
// hydration as hydrations has it.
func hydrateAt(t *testing.T, store *sqlite.Store, hydrations hydrationByTitle, at time.Time) {
	t.Helper()
	ed := editor.New(editor.Deps{Store: store, Model: hydrations, Log: slog.New(slog.DiscardHandler)})
	if err := ed.RunWaiting(t.Context(), at); err != nil {
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

// releaseHydrated is the release's Story as a run is shown it once hydrated by
// releaseHydration, after the speech's two Spans: its S1, Rejected, left out;
// "the city" and "He" read as what they refer to; its S2 and S3 Andy
// Burnham's words. The Spans keep the numbers they have as written.
const releaseHydrated = `Story from "GBR headquarters in Derby", published 2026-10-15
S4 (spoken by Andy Burnham): “This is fantastic news for ⟦Derby⟧.
S5 (spoken by Andy Burnham): Derby has waited long enough.”
S6: ⟦Andy Burnham⟧ visited the site on Monday.
`

func TestARunIsShownAHydratedSpanAsReadLeavingOutARejectedOne(t *testing.T) {
	store := record(t)
	withRelease(t, store, saturday)
	hydrateAt(t, store, hydrationByTitle{release.Title: releaseHydration}, saturday)
	batcher := &fakeBatcher{}
	runAt(t, weavingBatches(store, batcher), saturday)

	in := batcher.submitted[0][0].Request.Input
	if !strings.Contains(in, releaseHydrated) {
		t.Errorf("the run was shown:\n%s\nwant it to hold:\n%s", in, releaseHydrated)
	}
}

// releaseHydratedRecord is a Record holding the speech and the release, the release
// hydrated by releaseHydration, and the runner whose Saturday run has been sent.
func releaseHydratedRecord(t *testing.T) (*sqlite.Store, *fakeBatcher, *editor.Batches) {
	t.Helper()
	store := record(t)
	withRelease(t, store, saturday)
	hydrateAt(t, store, hydrationByTitle{release.Title: releaseHydration}, saturday)
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)
	runAt(t, b, saturday)
	return store, batcher, b
}

func TestACitationOfAHydratedSpanHoldsItsAddressAndItsWordsAsWritten(t *testing.T) {
	store, batcher, b := releaseHydratedRecord(t)
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
// written, and the release is hydrated before its result arrives. The result is
// applied to what the run was shown, S3 included, though the release's
// hydration has Rejected it since.
func TestAResultIsAppliedToWhatTheRunWasShownWhenASpanIsHydratedAfterItsRequestWent(t *testing.T) {
	store := record(t)
	withRelease(t, store, saturday)
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)
	runAt(t, b, saturday)
	if in := batcher.submitted[0][0].Request.Input; !strings.Contains(in, "S3: Andy Burnham said:\n") {
		t.Fatalf("the run was not shown the release as written:\n%s", in)
	}
	hydrateAt(t, store, hydrationByTitle{release.Title: releaseHydration}, saturday.Add(30*time.Minute))
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

// An earlier Story the Editor made, opened, is shown whole: the Spans its
// Editor's Spans cite read hydrated, and shown whether or not they are
// Rejected, since the Editor's Span rests on them, each numbered on from
// what the run was shown, then its Editor's Spans, marked as the Editor's.
func TestAnOpenedStoryOfTheEditorsIsShownWholeItsCitedSpansHydrated(t *testing.T) {
	store, batcher, b := releaseHydratedRecord(t)
	answerLast(batcher, `{"stories": [{"editor_spans": [{"body": "Derby waited.", "cites": [4, 6]}]}]}`)
	runAt(t, b, saturday.Add(time.Hour))
	earlier := madeStories(t, store)[0]
	sunday := saturday.Add(24 * time.Hour)
	cutAt(t, store, sunday)
	runAt(t, b, sunday)
	respondLast(batcher, asking("open_story", fmt.Sprintf(`{"story": %d}`, earlier.ID)))
	runAt(t, b, sunday.Add(time.Hour))

	want := fmt.Sprintf(`Story %d, the Editor's, made 2026-10-24; cited by no later Editor's Span
S2 (from "GBR headquarters in Derby", spoken by Andy Burnham): “This is fantastic news for ⟦Derby⟧.
S3 (from "GBR headquarters in Derby"): ⟦Andy Burnham⟧ visited the site on Monday.
S4 (the Editor's, citing S2, S3): Derby waited.
`, earlier.ID)
	if got := answerOf(t, lastSent(t, batcher), "call-1"); got != want {
		t.Errorf("open_story answered:\n%s\nwant:\n%s", got, want)
	}
}

// A run asking for a name finds the Spans that refer to it in other words,
// as hydrated ("He" read as Andy Burnham), but no Span hydration Rejects, and
// is shown each match between the Spans around it, unnumbered.
func TestAnAskForWordsFindsSpansByTheirHydratedText(t *testing.T) {
	store, batcher, b := releaseHydratedRecord(t)
	answerLast(batcher, `{"stories": []}`)
	runAt(t, b, saturday.Add(time.Hour))
	sunday := saturday.Add(24 * time.Hour)
	cutAt(t, store, sunday)
	runAt(t, b, sunday)
	respondLast(batcher, asking("find", `{"words": "Andy  BURNHAM"}`))
	runAt(t, b, sunday.Add(time.Hour))

	got := answerOf(t, lastSent(t, batcher), "call-1")
	want := `1 earlier Story has Spans with the words "Andy  BURNHAM".

Story %d, from "GBR headquarters in Derby", published 2026-10-15, from govuk-number-10; cited by no later Editor's Span
  · Derby has waited long enough.”
  S2: ⟦Andy Burnham⟧ visited the site on Monday.
`
	cuts, err := store.StoriesCutThrough(t.Context(), 1<<40)
	if err != nil {
		t.Fatal(err)
	}
	var releaseCut int64
	for _, c := range cuts {
		if c.Item.EntryID == release.EntryID {
			releaseCut = c.CutID
		}
	}
	if want = fmt.Sprintf(want, releaseCut); got != want {
		t.Errorf("find answered:\n%s\nwant:\n%s", got, want)
	}
}

// A Span an earlier Editor's Span cites, hydrated after the run was shown
// it, leaves the run's answer applied to what it was shown.
func TestAResultIsAppliedWhenASpanTheRunOpenedIsHydratedAfter(t *testing.T) {
	store := record(t)
	withRelease(t, store, saturday)
	batcher := &fakeBatcher{}
	b := weavingBatches(store, batcher)
	runAt(t, b, saturday)
	answerLast(batcher, `{"stories": [{"editor_spans": [{"body": "Derby waited.", "cites": [4, 6]}]}]}`)
	runAt(t, b, saturday.Add(time.Hour))
	earlier := madeStories(t, store)[0]
	sunday := saturday.Add(24 * time.Hour)
	cutAt(t, store, sunday)
	runAt(t, b, sunday)
	respondLast(batcher, asking("open_story", fmt.Sprintf(`{"story": %d}`, earlier.ID)))
	runAt(t, b, sunday.Add(time.Hour))
	hydrateAt(t, store, hydrationByTitle{release.Title: releaseHydration}, sunday.Add(90*time.Minute))
	answerLast(batcher, `{"stories": [{"editor_spans": [{"body": "Ferries and Derby.", "cites": [3, 1]}]}]}`)
	runAt(t, b, sunday.Add(2*time.Hour))

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

// A stretch that says whose something is reads as the possessive of what it
// refers to, as the Item and Span Pages read it.
func TestARunIsShownAPossessiveHydratedAsAPossessive(t *testing.T) {
	store := record(t)
	at := saturday
	if _, err := store.AddItems(t.Context(), []collect.Item{{
		SourceID: release.SourceID, EntryID: "derby-station", Title: "Derby station", Link: "https://www.gov.uk/derby-station",
		Content:   "<p>Derby has a station.</p><p>Its role will grow.</p><p>Engineers will mark their work there.</p>",
		Published: release.Published, Attribution: collect.OGL, CollectedAt: at,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := cut.New(store).CutNew(t.Context(), at); err != nil {
		t.Fatal(err)
	}
	hydrateAt(t, store, hydrationByTitle{"Derby station": `{"names": [], "refers_to": [
		{"from": {"span": 2, "words": "Its"}, "to": {"span": 1, "words": "Derby"}},
		{"from": {"span": 3, "words": "their"}, "to": {"span": 3, "words": "Engineers"}}
	], "rejected": [], "speakers": []}`}, at)
	batcher := &fakeBatcher{}
	runAt(t, weavingBatches(store, batcher), at)

	in := batcher.submitted[0][0].Request.Input
	for _, want := range []string{": ⟦Derby’s⟧ role will grow.\n", ": Engineers will mark ⟦Engineers’⟧ work there.\n"} {
		if !strings.Contains(in, want) {
			t.Errorf("the run was shown:\n%s\nwant it to hold %q", in, want)
		}
	}
}

// A publisher's own square brackets ("yesterday [TUE]") are shown as written,
// and a hydrated stretch in marks no publisher writes, so the two are told
// apart.
func TestARunIsShownAPublishersOwnSquareBracketsApartFromHydration(t *testing.T) {
	store := record(t)
	at := saturday
	if _, err := store.AddItems(t.Context(), []collect.Item{{
		SourceID: release.SourceID, EntryID: "warsaw", Title: "Warsaw", Link: "https://www.gov.uk/warsaw",
		Content:   "<p>The Foreign Secretary went to Warsaw.</p><p>She spoke there yesterday [TUE].</p>",
		Published: release.Published, Attribution: collect.OGL, CollectedAt: at,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := cut.New(store).CutNew(t.Context(), at); err != nil {
		t.Fatal(err)
	}
	hydrateAt(t, store, hydrationByTitle{"Warsaw": `{"names": [], "refers_to": [
		{"from": {"span": 2, "words": "She"}, "to": {"span": 1, "words": "The Foreign Secretary"}}
	], "rejected": [], "speakers": []}`}, at)
	batcher := &fakeBatcher{}
	runAt(t, weavingBatches(store, batcher), at)

	in := batcher.submitted[0][0].Request.Input
	if want := ": ⟦The Foreign Secretary⟧ spoke there yesterday [TUE].\n"; !strings.Contains(in, want) {
		t.Errorf("the run was shown:\n%s\nwant it to hold %q", in, want)
	}
}

func TestACitationOfARejectedSpanLeftOutIsRefused(t *testing.T) {
	store, batcher, b := releaseHydratedRecord(t)
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
