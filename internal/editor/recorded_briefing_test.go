package editor_test

import (
	"bytes"
	"errors"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/interest"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// briefingTopics are the fixed Topics and Depths the recorded Briefing is
// written for. No Topic follows the Lord-Lieutenancy of County Tyrone, so
// its Story is not in the Briefing.
var briefingTopics = []interest.Draft{
	{Name: "Ukraine", Depth: "close", Terms: "Ukraine\nZelenskyy\nRussia"},
	{Name: "Railways", Depth: "following", Terms: "Great British Railways"},
	{Name: "Scotland", Depth: "passing", Terms: "Scotland\nScottish\nHolyrood"},
}

// The recorded run of writing the Briefing. The fixtures' readings,
// placings and connectings, from the Briefing's own frozen copies
// (testdata/recorded/briefing/inputs), are replayed into a Record with the
// Topics above, and the Briefing for the day is written from it in one
// call. It is sampled five times, each over a fresh copy of that Record, and
// kept under testdata/recorded/briefing/saturday/. Recording costs five
// calls, and more for attempts the checks refuse:
//
//	go test -count=1 ./internal/editor -run TestRecordedBriefing -record
//
// with GOSLOW_ANTHROPIC_API_KEY and BRIEFING_MODEL set in the environment.
// Re-recording an earlier act leaves it valid; bringing it onto newer ones
// is a refresh: copy the current readings, placings and connectings together
// over its inputs, then re-record it.
func TestRecordedBriefing(t *testing.T) {
	t.Parallel()
	recordedBriefing(t, ownDirectory(t, "briefing"))
}

// briefingAt is when the recorded Briefing is written.
var briefingAt = saturday.Add(time.Hour)

// briefingSample runs one sample of writing the Briefing on m into store,
// as in use: up to its attempts against the act's checks.
func briefingSample(t *testing.T, store *sqlite.Store, m *sampleModel) bool {
	t.Helper()
	passed, err := runSample(m, func(model editor.Model) (bool, error) {
		var logs bytes.Buffer
		ed := editor.New(editor.Deps{Store: store, Briefings: store, BriefingModel: model, Log: slog.New(slog.NewTextHandler(&logs, nil))})
		err := ed.Brief(t.Context(), briefingAt)
		return !strings.Contains(logs.String(), "writing the Briefing failed"), err
	})
	if err != nil {
		t.Fatal(err)
	}
	return passed
}

// recordedBriefing replays (or records) the Briefing from the recordings
// under root.
func recordedBriefing(t *testing.T, root string) {
	ctx := t.Context()
	inputs := filepath.Join(root, "briefing", "inputs")
	at := briefingAt
	frozen := frozenRecordFile(t, inputs)
	given, err := copyOfRecord(t, frozen).ToBrief(ctx, at.Add(-24*time.Hour), at)
	if err != nil {
		t.Fatal(err)
	}
	if len(given) == 0 {
		t.Fatal("the Topics select no Story")
	}
	for _, tb := range given {
		// The Topics select the Stories: none follows County Tyrone.
		if strings.Contains(tb.Story.Title, "Tyrone") {
			t.Errorf("a Story no Topic follows was given: %q", tb.Story.Title)
		}
	}
	for _, err := range sampled(t, filepath.Join(root, "briefing"), "saturday", dailyRule, briefingCommand, func(m *sampleModel) sampleResult {
		store := copyOfRecord(t, frozen)
		if !briefingSample(t, store, m) {
			return sampleResult{}
		}
		b, found, err := store.Briefing(ctx, at.Format(time.DateOnly))
		if err != nil || !found {
			t.Fatalf("no Briefing written: %v %v", found, err)
		}
		p := properties{}
		p.holds("the Briefing tells a Story", len(b.Stories) > 0, "the Briefing tells no Story")
		told := p.property("the Briefing tells only the Stories it was given")
		traced := p.property("every statement cites Developments given it, which trace to their Items")
		cited := map[int64]bool{}
		for _, bs := range b.Stories {
			i := slices.IndexFunc(given, func(tb editor.ToBrief) bool { return tb.Story.ID == bs.Story.ID })
			if i < 0 {
				told("the Briefing tells %q, which it was not given", bs.Story.Title)
				continue
			}
			for _, st := range bs.Statements {
				if len(st.Developments) == 0 {
					traced("%q: an untraced statement: %q", bs.Story.Title, st.Text)
				}
				for _, d := range st.Developments {
					if !slices.ContainsFunc(given[i].Today, func(x editor.Development) bool { return x.ID == d.ID }) {
						traced("%q: %q cites a Development not given: %+v", bs.Story.Title, st.Text, d)
					}
					if d.EntryID == "" || d.ItemTitle == "" {
						traced("%q: a cited Development that does not trace to its Item: %+v", bs.Story.Title, d)
					}
					cited[d.ID] = true
				}
			}
		}
		// At following Depth or closer, every Development is told.
		following := p.property("at following Depth or closer, every Development is told")
		for _, tb := range given {
			if tb.Depth >= interest.Following {
				for _, d := range tb.Today {
					if !cited[d.ID] {
						following("%q, at %s Depth: %q is left out", tb.Story.Title, tb.Depth, d.Statement)
					}
				}
			}
		}

		// The next day nothing has moved on: the Briefing says so, with no
		// call.
		ed := editor.New(editor.Deps{Store: store, Briefings: store, BriefingModel: &fakeModel{err: errors.New("a call was made")},
			Log: slog.New(slog.DiscardHandler)})
		if err := ed.Brief(ctx, at.AddDate(0, 0, 1)); err != nil {
			t.Fatal(err)
		}
		next, found, err := store.Briefing(ctx, at.AddDate(0, 0, 1).Format(time.DateOnly))
		if err != nil || !found || len(next.Stories) != 0 {
			t.Errorf("the next day's Briefing = %+v, %v, %v; want one written empty", next, found, err)
		}
		return sampleResult{passed: true, held: p}
	}) {
		t.Error(err)
	}
}
