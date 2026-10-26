package editor_test

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/interest"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// The recorded run of reading with a precedent (system design: Reading is
// shown the wrong Feedback on Facts from Items on its Topics). The precedent
// is a fixture, not a model's output: a forecast in the Prime Minister's
// statement to the House of Commons, that the UK-EU deals will add over £10
// billion a year to the economy by 2040, drawn by hand as a Fact, which the
// Owner said was wrong (a Claim taken for a Fact) while following a Topic on
// the economy. The Great British Railways announcement, tied to the same
// Topic and carrying its own estimate (a £151 million boost to the region's
// economy), is then read by the real model, shown that precedent. It is
// sampled five times, each over a fresh Record, and kept under
// testdata/recorded/precedent/reading-<the announcement>/. Recording costs
// five calls, and more for attempts the checks refuse:
//
//	go test -count=1 ./internal/editor -run TestRecordedReadingPrecedent -record
//
// with GOSLOW_ANTHROPIC_API_KEY and READING_MODEL set in the environment.
func TestRecordedReadingPrecedent(t *testing.T) {
	t.Parallel()
	recordedReadingPrecedent(t, ownDirectory(t, "precedent"))
}

// recordedReadingPrecedent replays (or records) reading with a precedent
// from the recordings under root.
func recordedReadingPrecedent(t *testing.T, root string) {
	name := "great-british-railways-takes-shape-as-hq-and-apprenticeship-scheme-confirmed"
	for _, err := range sampled(t, filepath.Join(root, "precedent"), "reading-"+name, dailyRule, readingPrecedentCommand, func(m *sampleModel) sampleResult {
		return readingPrecedentSample(t, name, m)
	}) {
		t.Error(err)
	}
}

// readingPrecedentSample is one sample of reading the announcement name
// shown the precedent, on m, over a fresh Record.
func readingPrecedentSample(t *testing.T, name string, m *sampleModel) sampleResult {
	t.Helper()
	ctx := t.Context()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "go-slow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SyncSources(ctx, []collect.Source{number10}, saturday); err != nil {
		t.Fatal(err)
	}
	topics := interest.New(store)
	if _, errs, err := topics.Add(ctx, interest.Draft{Name: "The economy", Depth: "following", Terms: "economy"}); err != nil || errs != nil {
		t.Fatalf("add Topic: %v %v", errs, err)
	}
	addItem := func(it editor.Item) {
		t.Helper()
		if _, err := store.AddItems(ctx, []collect.Item{{
			SourceID: it.SourceID, EntryID: it.EntryID, Title: it.Title, Link: it.Link, Summary: it.Summary,
			Content: it.Content, Published: it.Published, Attribution: collect.OGL, CollectedAt: saturday,
		}}); err != nil {
			t.Fatal(err)
		}
		if err := topics.CheckNew(ctx); err != nil {
			t.Fatal(err)
		}
	}

	// The precedent: the statement's forecast, drawn as a Fact and said to
	// be wrong.
	statement, body := fixture(t, filepath.Join("testdata", "govuk", "pm-statement-to-the-house-of-commons-12-october-2026.json"))
	addItem(statement)
	forecast := editor.Fact{
		Statement: "The UK-EU deals will add the equivalent of over £10 billion per year to the UK economy by 2040.",
		AsAt:      time.Date(2026, time.October, 12, 0, 0, 0, 0, time.UTC),
		Quote:     "these deals will add the equivalent of over 10 billion pounds per year to the UK economy by 2040",
		Entities:  []string{"United Kingdom"},
	}
	if !strings.Contains(normal(stripTags(body)), normal(forecast.Quote)) {
		t.Fatalf("the precedent's quote is not in the statement: %q", forecast.Quote)
	}
	if err := store.SaveReading(ctx, editor.Reading{
		SourceID: statement.SourceID, EntryID: statement.EntryID, Model: "fixture",
		Entities: []editor.Entity{{Name: "United Kingdom", Kind: "place"}},
		Facts:    []editor.Fact{forecast},
	}, saturday); err != nil {
		t.Fatal(err)
	}
	read, _, err := store.ReadItem(ctx, statement.SourceID, statement.EntryID)
	if err != nil || len(read.Facts) != 1 {
		t.Fatalf("the statement's reading: %+v %v", read.Facts, err)
	}
	wrong := wrongAt(t, store, read.Facts[0].ID, saturday)
	if wrong.Depth != interest.Following {
		t.Fatalf("the precedent was given at %v, want following", wrong.Depth)
	}

	// The announcement to read.
	it, _ := fixture(t, filepath.Join("testdata", "govuk", name+".json"))
	addItem(it)

	passed, err := runSample(m, func(model editor.Model) (bool, error) {
		var logs bytes.Buffer
		ed := editor.New(editor.Deps{Store: store, Model: model, Wikidata: &fakeWikidata{},
			Log: slog.New(slog.NewTextHandler(&logs, nil))})
		err := ed.ReadWaiting(ctx, saturday)
		return !strings.Contains(logs.String(), "reading failed"), err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !passed {
		return sampleResult{}
	}

	shown, err := store.PrecedentsShown(ctx, "reading", it.SourceID, it.EntryID)
	if err != nil {
		t.Fatal(err)
	}
	if len(shown) != 1 || shown[0] != wrong.ID {
		t.Errorf("the reading was shown precedents %v, want the wrong forecast %d", shown, wrong.ID)
	}
	// An estimate like the forecast the Owner said was wrong is not drawn
	// as a Fact.
	p := properties{}
	estimate := p.property("an estimate like the forecast said to be wrong is not drawn as a Fact")
	got, _, err := store.ReadItem(ctx, it.SourceID, it.EntryID)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range got.Facts {
		if strings.Contains(f.Statement, "151") || strings.Contains(f.Quote, "151") {
			estimate("the estimated boost is drawn as a Fact: %q", f.Statement)
		}
	}
	return sampleResult{passed: true, held: p}
}
