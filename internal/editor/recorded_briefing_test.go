package editor_test

import (
	"bytes"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/claude"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/interest"
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
// call, kept under testdata/recorded/briefing and replayed. (It was
// recorded over the readings and placings alone; connecting adds nothing to
// what the Briefing is given.) Re-recording costs one call:
//
//	go test -count=1 ./internal/editor -run TestRecordedBriefing -record
//
// with GOSLOW_ANTHROPIC_API_KEY and BRIEFING_MODEL set in the environment.
// Re-recording an earlier act leaves it valid; bringing it onto newer ones
// is a refresh: copy the current readings, placings and connectings together
// over its inputs, then re-record it.
func TestRecordedBriefing(t *testing.T) { recordedBriefing(t, recordedDir) }

// recordedBriefing replays (or records) the Briefing from the recordings
// under root.
func recordedBriefing(t *testing.T, root string) {
	ctx := t.Context()
	store := frozenRecord(t, filepath.Join(root, "briefing", "inputs"))
	at := saturday.Add(time.Hour)

	given, err := store.ToBrief(ctx, at.Add(-24*time.Hour), at)
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
		t.Logf("given %q at %s Depth (%s): %d Developments", tb.Story.Title, tb.Depth, strings.Join(tb.Topics, ", "), len(tb.Today))
	}
	path := filepath.Join(root, "briefing", "saturday.json")
	var model editor.Model
	var rec recording
	if *rerecordFlag {
		key, name := os.Getenv("GOSLOW_ANTHROPIC_API_KEY"), os.Getenv("BRIEFING_MODEL")
		if key == "" || name == "" {
			t.Fatal("recording needs GOSLOW_ANTHROPIC_API_KEY and BRIEFING_MODEL")
		}
		model = recorder{claude.New(claude.Options{APIKey: key, Model: name}), &rec, name}
	} else {
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			t.Skipf("the Briefing has not been recorded yet; record it with: %s", briefingCommand)
		}
		rec = kept(t, path)
		model = ownReplay(t, rec, "saturday", briefingCommand)
	}
	var logs bytes.Buffer
	ed := editor.New(editor.Deps{Store: store, Briefings: store, BriefingModel: model, Log: slog.New(slog.NewTextHandler(&logs, nil))})
	if err := ed.Brief(ctx, at); err != nil {
		t.Fatal(err)
	}
	if *rerecordFlag && rec.Output != nil {
		keep(t, path, rec)
	}
	t.Logf("recorded briefing usage: %d input, %d output tokens", rec.Usage.InputTokens, rec.Usage.OutputTokens)
	if strings.Contains(logs.String(), "writing the Briefing failed") {
		t.Fatalf("the recorded Briefing fails the act's checks:\n%s", logs.String())
	}

	b, found, err := store.Briefing(ctx, at.Format(time.DateOnly))
	if err != nil || !found {
		t.Fatalf("no Briefing written: %v %v", found, err)
	}
	if len(b.Stories) == 0 {
		t.Fatal("the Briefing tells no Story")
	}
	cited := map[int64]bool{}
	for _, bs := range b.Stories {
		i := slices.IndexFunc(given, func(tb editor.ToBrief) bool { return tb.Story.ID == bs.Story.ID })
		if i < 0 {
			t.Errorf("the Briefing tells %q, which it was not given", bs.Story.Title)
			continue
		}
		for _, st := range bs.Statements {
			// Every statement cites Developments that exist in the input.
			if len(st.Developments) == 0 {
				t.Errorf("%q: an untraced statement: %q", bs.Story.Title, st.Text)
			}
			for _, d := range st.Developments {
				if !slices.ContainsFunc(given[i].Today, func(x editor.Development) bool { return x.ID == d.ID }) {
					t.Errorf("%q: %q cites a Development not given: %+v", bs.Story.Title, st.Text, d)
				}
				if d.EntryID == "" || d.ItemTitle == "" {
					t.Errorf("%q: a cited Development that does not trace to its Item: %+v", bs.Story.Title, d)
				}
				cited[d.ID] = true
			}
			t.Logf("%s: %s", bs.Story.Title, st.Text)
		}
	}
	for _, tb := range given {
		// At following Depth or closer, every Development is told.
		if tb.Depth >= interest.Following {
			for _, d := range tb.Today {
				if !cited[d.ID] {
					t.Errorf("%q, at %s Depth: %q is left out", tb.Story.Title, tb.Depth, d.Statement)
				}
			}
		}
	}

	// The next day nothing has moved on: the Briefing says so, with no call.
	if err := ed.Brief(ctx, at.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	next, found, err := store.Briefing(ctx, at.AddDate(0, 0, 1).Format(time.DateOnly))
	if err != nil || !found || len(next.Stories) != 0 {
		t.Errorf("the next day's Briefing = %+v, %v, %v; want one written empty", next, found, err)
	}
}
