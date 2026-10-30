package editor_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
)

// fakeModel stands in for a model behind the port: it answers every request
// with the same output, and keeps the requests it was sent.
type fakeModel struct {
	output string
	err    error
	sent   []editor.Request
}

func (f *fakeModel) Complete(_ context.Context, r editor.Request) (editor.Response, error) {
	f.sent = append(f.sent, r)
	if f.err != nil {
		return editor.Response{}, f.err
	}
	return editor.Response{Output: json.RawMessage(f.output), Model: "fake"}, nil
}

var speech = editor.Item{
	SourceID:  "govuk-number-10",
	EntryID:   "tag:www.gov.uk,2005:/government/speeches/pm-speech-in-scotland-16-october-2026",
	Title:     "PM speech in Scotland: 16 October 2026",
	Link:      "https://www.gov.uk/government/speeches/pm-speech-in-scotland-16-october-2026",
	Summary:   "Prime Minister Andy Burnham delivered a speech in Scotland today.",
	Content:   "<p>It’s good to be back in Scotland, to be here in Motherwell.</p><p>Scotland will get £2 billion.</p>",
	Published: time.Date(2026, time.October, 16, 15, 59, 24, 0, time.UTC),
}

// goodReading is a reading of the speech, cut into its two sentences: S1
// "It’s good to be back in Scotland, to be here in Motherwell." and S2
// "Scotland will get £2 billion."
const goodReading = `{
  "names": [
    {"at": {"span": 1, "words": "Scotland"}, "kind": "place", "name": "Scotland", "office": ""},
    {"at": {"span": 1, "words": "Motherwell"}, "kind": "place", "name": "Motherwell", "office": ""}
  ],
  "refers_to": [],
  "rejected": [],
  "speakers": []
}`

// release is an Item with a bare attribution, a quotation and references in
// other words, cut into S1 "Andy Burnham said:", S2 "“This is fantastic news
// for the city.", S3 "Derby has waited long enough.”" and S4 "He visited
// the site on Monday."
var release = editor.Item{
	SourceID: "govuk-number-10", EntryID: "release", Title: "GBR headquarters in Derby",
	Content:   "<p>Andy Burnham said:</p><p>“This is fantastic news for the city. Derby has waited long enough.”</p><p>He visited the site on Monday.</p>",
	Published: time.Date(2026, time.October, 15, 9, 0, 0, 0, time.UTC),
}

const releaseReading = `{
  "names": [
    {"at": {"span": 1, "words": "Andy Burnham"}, "kind": "person", "name": "Andy Burnham", "office": "Prime Minister of the United Kingdom"},
    {"at": {"span": 3, "words": "Derby"}, "kind": "place", "name": "Derby", "office": "City of Derby"}
  ],
  "refers_to": [
    {"from": {"span": 2, "words": "the city"}, "to": {"span": 3, "words": "Derby"}},
    {"from": {"span": 4, "words": "He"}, "to": {"span": 1, "words": "Andy Burnham"}}
  ],
  "rejected": [1],
  "speakers": [
    {"span": {"span": 2, "words": ""}, "speaker": {"span": 1, "words": "Andy Burnham"}},
    {"span": {"span": 3, "words": ""}, "speaker": {"span": 1, "words": "Andy Burnham"}}
  ]
}`

// toRead is the Item as reading is given it: with its Cut by the sentence
// cutter.
func toRead(it editor.Item) editor.ToRead {
	return editor.ToRead{Item: it, Cut: cut.Cut{ID: 7, SourceID: it.SourceID, EntryID: it.EntryID,
		Judge: cut.SentenceCutter, Spans: cut.Sentences(it.Content)}}
}

// spanWords is the words at an address in it.
func spanWords(it editor.Item, a editor.Address) string { return cut.Words(it.Content, a.Span) }

func TestReadingShowsTheItemAsItsNumberedSpans(t *testing.T) {
	model := &fakeModel{output: releaseReading}

	if _, err := editor.Read(t.Context(), model, "claude-test", toRead(release)); err != nil {
		t.Fatal(err)
	}

	in := model.sent[0].Input
	for _, want := range []string{
		"Title: GBR headquarters in Derby\n",
		"Published: 2026-10-15\n",
		"S1: Andy Burnham said:\n",
		"S2: “This is fantastic news for the city.\n",
		"S3: Derby has waited long enough.”\n",
		"S4: He visited the site on Monday.\n",
	} {
		if !strings.Contains(in, want) {
			t.Errorf("the input does not show %q:\n%s", want, in)
		}
	}
	if strings.Contains(in, "<p>") {
		t.Errorf("the input shows the Item's markup:\n%s", in)
	}
	props := model.sent[0].Schema["properties"].(map[string]any)
	for _, kind := range []string{"names", "refers_to", "rejected", "speakers"} {
		if _, ok := props[kind]; !ok {
			t.Errorf("the schema asks for no %s: %v", kind, props)
		}
	}
	for _, old := range []string{"facts", "claims", "mentions"} {
		if _, ok := props[old]; ok {
			t.Errorf("the schema still asks for %s", old)
		}
	}
}

func TestReadingJudgesEachKindAtAnAddressInItsItem(t *testing.T) {
	r, err := editor.Read(t.Context(), &fakeModel{output: releaseReading}, "claude-test", toRead(release))
	if err != nil {
		t.Fatal(err)
	}

	if r.SourceID != release.SourceID || r.EntryID != release.EntryID || r.CutID != 7 {
		t.Errorf("the reading is of %s %s on Cut %d", r.SourceID, r.EntryID, r.CutID)
	}
	if len(r.RefersTo) != 2 {
		t.Fatalf("refers to = %+v", r.RefersTo)
	}
	if from, to := spanWords(release, r.RefersTo[0].From), spanWords(release, r.RefersTo[0].To); from != "the city" || to != "Derby" {
		t.Errorf("the first refers to is %q to %q", from, to)
	}
	if len(r.Names) != 2 || spanWords(release, r.Names[0].At) != "Andy Burnham" || r.Names[0].Kind != "person" ||
		r.Names[0].Office != "Prime Minister of the United Kingdom" {
		t.Errorf("names = %+v", r.Names)
	}
	if len(r.Speakers) != 2 || spanWords(release, r.Speakers[0].Of) != "“This is fantastic news for the city." ||
		spanWords(release, r.Speakers[0].By) != "Andy Burnham" {
		t.Errorf("speakers = %+v", r.Speakers)
	}
	if len(r.Rejected) != 1 || spanWords(release, r.Rejected[0]) != "Andy Burnham said:" {
		t.Errorf("rejected = %+v", r.Rejected)
	}
	for _, a := range append([]editor.Address{r.Rejected[0], r.Names[1].At}, r.RefersTo[1].From, r.Speakers[1].By) {
		if a.SourceID != release.SourceID || a.EntryID != release.EntryID {
			t.Errorf("an address outside the Item: %+v", a)
		}
	}
}

func TestReadingsJudgeIsTheCall(t *testing.T) {
	model := &fakeModel{output: releaseReading}

	r, err := editor.Read(t.Context(), model, "claude-test", toRead(release))
	if err != nil {
		t.Fatal(err)
	}

	want := editor.Judge{Act: "reading", Version: editor.ReadingVersion, Model: "fake",
		RequestHash: editor.RequestHash(model.sent[0], "claude-test")}
	if r.Judge != want {
		t.Errorf("judge = %+v, want %+v", r.Judge, want)
	}
}

func TestAnOfficeGivenToAnythingButAPersonIsDropped(t *testing.T) {
	r, err := editor.Read(t.Context(), &fakeModel{output: releaseReading}, "", toRead(release))
	if err != nil {
		t.Fatal(err)
	}

	if derby := r.Names[1]; derby.Name != "Derby" || derby.Office != "" {
		t.Errorf("Derby, a place, keeps an office: %+v", derby)
	}
}

func TestAQuoteNotInItsSpanIsRefusedNamingTheSpanAndTheWords(t *testing.T) {
	for name, output := range map[string]string{
		"a name":       `{"names": [{"at": {"span": 2, "words": "Derby"}, "kind": "place", "name": "Derby", "office": ""}], "refers_to": [], "rejected": [], "speakers": []}`,
		"a refers to":  `{"names": [], "refers_to": [{"from": {"span": 4, "words": "He"}, "to": {"span": 2, "words": "Derby"}}], "rejected": [], "speakers": []}`,
		"a speaker":    `{"names": [], "refers_to": [], "rejected": [], "speakers": [{"span": {"span": 3, "words": ""}, "speaker": {"span": 2, "words": "Derby"}}]}`,
		"a paraphrase": `{"names": [], "refers_to": [], "rejected": [], "speakers": [{"span": {"span": 2, "words": "Derby"}, "speaker": {"span": 1, "words": "Andy Burnham"}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := editor.Read(t.Context(), &fakeModel{output: output}, "", toRead(release))

			if !errors.Is(err, editor.ErrFailedChecks) {
				t.Fatalf("err = %v, want ErrFailedChecks", err)
			}
			if !strings.Contains(err.Error(), "S2") || !strings.Contains(err.Error(), `"Derby"`) {
				t.Errorf("the refusal does not name the Span and the words: %v", err)
			}
		})
	}
}

func TestASpanNotInTheCutIsRefused(t *testing.T) {
	for name, output := range map[string]string{
		"rejected":   `{"names": [], "refers_to": [], "rejected": [5], "speakers": []}`,
		"a name":     `{"names": [{"at": {"span": 0, "words": "x"}, "kind": "place", "name": "x", "office": ""}], "refers_to": [], "rejected": [], "speakers": []}`,
		"a speaker":  `{"names": [], "refers_to": [], "rejected": [], "speakers": [{"span": {"span": 9, "words": ""}, "speaker": {"span": 1, "words": "Andy Burnham"}}]}`,
		"no name":    `{"names": [{"at": {"span": 1, "words": "Andy Burnham"}, "kind": "person", "name": " ", "office": ""}], "refers_to": [], "rejected": [], "speakers": []}`,
		"old schema": goodFactReading,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := editor.Read(t.Context(), &fakeModel{output: output}, "", toRead(release)); !errors.Is(err, editor.ErrFailedChecks) {
				t.Errorf("err = %v, want ErrFailedChecks", err)
			}
		})
	}
}

// goodFactReading is an answer in the earlier reading act's schema.
const goodFactReading = `{"facts": [], "claims": [], "mentions": []}`

func TestAReferenceToItselfIsNoReference(t *testing.T) {
	output := `{"names": [], "refers_to": [{"from": {"span": 3, "words": "Derby"}, "to": {"span": 3, "words": "Derby"}}], "rejected": [], "speakers": []}`

	r, err := editor.Read(t.Context(), &fakeModel{output: output}, "", toRead(release))

	if err != nil || len(r.RefersTo) != 0 {
		t.Errorf("a Span referring to itself: %+v, %v", r.RefersTo, err)
	}
}
