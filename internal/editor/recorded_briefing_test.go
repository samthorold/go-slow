package editor_test

import (
	"bytes"
	"errors"
	"log/slog"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// The recorded run of writing the Briefing. The fixtures' readings and
// relatings, from the Briefing's own frozen copies
// (testdata/recorded/briefing/inputs/{reading,relating}), are replayed into
// a Record, what each reading names looked up in a Wikidata that finds
// nothing, as relating's recordings were made; and the Briefing for the day
// is written over every Span read, in one call. It is sampled five times,
// each over a fresh copy of that Record, and kept under
// testdata/recorded/briefing/saturday/. Recording costs five calls, and
// more for attempts the checks refuse:
//
//	go test -count=1 -timeout 90m ./internal/editor -run TestRecordedBriefing -record
//
// with GOSLOW_ANTHROPIC_API_KEY and BRIEFING_MODEL set in the environment.
// Re-recording an earlier act leaves it valid; bringing it onto newer ones
// is a refresh: copy the current readings and relatings together over its
// inputs, then re-record it.
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
		ed := editor.New(editor.Deps{Store: store, Briefings: store, BriefingModel: model,
			ModelFor: func(string) string { return m.model }, Log: slog.New(slog.NewTextHandler(&logs, nil))})
		err := ed.Brief(t.Context(), briefingAt)
		return !strings.Contains(logs.String(), "writing the Briefing failed"), err
	})
	if err != nil {
		t.Fatal(err)
	}
	return passed
}

// spanRecordFile is the file of a closed Record holding the fixture Items,
// with the readings and relatings under dir replayed into it: an act's
// frozen copies, for the acts that rest on both. It is built once for the
// bytes of the fixtures and the copies (frozenOnce).
func spanRecordFile(t *testing.T, dir string) string {
	t.Helper()
	reading, relating := filepath.Join(dir, "reading"), filepath.Join(dir, "relating")
	return frozenOnce(t, "span", []string{filepath.Join("testdata", "govuk"), reading, relating}, func(path string) {
		store, topics := newRecordAt(t, path, nil)
		c := chain{reading: replayed(reading), relating: replayed(relating)}
		if err := c.run(t.Context(), store, topics, fixtureItems(t)); err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	})
}

// The Span of the railways release that projects what its headquarters will
// bring: a projection, never to be told as having happened.
const projectionWords = "£151 million boost"

// projectionMarker is wording that tells a projection as one.
var projectionMarker = regexp.MustCompile(`(?i)\b(will|would|expected|estimated|estimates?|set to|could|forecasts?|projected|predicts?|aims?|promised?|says?|said)\b`)

// namesSpeaker is whether text names the speaker who, as the Span is shown
// spoken by: by a word of how they are shown, four letters or more and not
// "said" ("Burnham" for "Andy Burnham", "government" or "spokesperson" for
// "A UK government spokesperson"). A speaker shown with no such word is
// named by any text.
func namesSpeaker(text, who string) bool {
	words := strings.FieldsFunc(strings.ToLower(who), func(r rune) bool { return !strings.ContainsRune("abcdefghijklmnopqrstuvwxyz'’-", r) })
	var naming []string
	for _, w := range words {
		if len([]rune(w)) >= 4 && w != "said" {
			naming = append(naming, w)
		}
	}
	lower := strings.ToLower(text)
	return len(naming) == 0 || slices.ContainsFunc(naming, func(w string) bool { return strings.Contains(lower, w) })
}

// recordedBriefing replays (or records) the Briefing from the recordings
// under root.
func recordedBriefing(t *testing.T, root string) {
	ctx := t.Context()
	inputs := filepath.Join(root, "briefing", "inputs")
	at := briefingAt
	frozen := spanRecordFile(t, inputs)
	given, err := copyOfRecord(t, frozen).SpansToTell(ctx, at.Add(-24*time.Hour), at)
	if err != nil {
		t.Fatal(err)
	}
	shown := map[editor.Address]editor.SpanToTell{}
	for _, it := range given {
		for _, sp := range it.Spans {
			shown[sp.At] = sp
		}
	}
	runs := casesPresentForBriefing(t, given)
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
		p.holds("the Briefing tells something", len(b.Statements) > 0, "the Briefing makes no statement")
		voice := p.property("a statement keeps its speaker's voice")
		projection := p.property("a projection isn't told as having happened")
		for _, st := range b.Statements {
			for _, c := range st.Cites {
				sp, ok := shown[c.At]
				if !ok {
					continue // an earlier Span a relation reached: shown, but not one of today's
				}
				if sp.Speaker != "" && !namesSpeaker(st.Text, sp.Speaker) {
					voice("%q cites %q, spoken by %s, and does not name them", st.Text, sp.Words, sp.Speaker)
				}
				if strings.Contains(sp.Words, projectionWords) && regexp.MustCompile(`(?i)151|boost|jobs`).MatchString(st.Text) &&
					!projectionMarker.MatchString(st.Text) {
					projection("%q tells the projection %q as having happened", st.Text, sp.Words)
				}
			}
		}
		// Told once: never one statement telling one of two Spans the same
		// as each other and another statement the other. (Telling one and
		// leaving the other untold is still telling the run once.)
		once := p.property("a same as run is told once")
		for _, run := range runs {
			var alone [2]string
			for _, st := range b.Statements {
				cites := func(a editor.Address) bool {
					return slices.ContainsFunc(st.Cites, func(c editor.OtherSpan) bool { return c.At == a })
				}
				switch {
				case cites(run[0]) && !cites(run[1]):
					alone[0] = st.Text
				case cites(run[1]) && !cites(run[0]):
					alone[1] = st.Text
				}
			}
			if alone[0] != "" && alone[1] != "" {
				once("two Spans the same as each other, %q and %q, are told apart: in %q and in %q",
					shown[run[0]].Words, shown[run[1]].Words, alone[0], alone[1])
			}
		}

		// The next day nothing has been read since: the Briefing is written
		// empty, with no call.
		ed := editor.New(editor.Deps{Store: store, Briefings: store, BriefingModel: &fakeModel{err: errors.New("a call was made")},
			Log: slog.New(slog.DiscardHandler)})
		if err := ed.Brief(ctx, at.AddDate(0, 0, 1)); err != nil {
			t.Fatal(err)
		}
		next, found, err := store.Briefing(ctx, at.AddDate(0, 0, 1).Format(time.DateOnly))
		if err != nil || !found || len(next.Statements) != 0 {
			t.Errorf("the next day's Briefing = %+v, %v, %v; want one written empty", next, found, err)
		}
		return sampleResult{passed: true, held: p}
	}) {
		t.Error(err)
	}
}

// casesPresentForBriefing fails unless the Spans the Briefing is shown carry
// each case its properties rest on: a quoted speaker, the projection, and a
// same as between two of today's Spans. It returns the same as pairs.
func casesPresentForBriefing(t *testing.T, given []editor.ItemToTell) [][2]editor.Address {
	t.Helper()
	today := map[editor.Address]bool{}
	spoken, projected := false, false
	for _, it := range given {
		for _, sp := range it.Spans {
			today[sp.At] = true
			spoken = spoken || sp.Speaker != ""
			projected = projected || strings.Contains(sp.Words, projectionWords)
		}
	}
	var runs [][2]editor.Address
	for _, it := range given {
		for _, sp := range it.Spans {
			for _, rel := range sp.Relations {
				r := rel.Relation
				if r.Kind == editor.SameAs && r.From == sp.At && today[r.To] {
					runs = append(runs, [2]editor.Address{r.From, r.To})
				}
			}
		}
	}
	switch {
	case !spoken:
		t.Fatal("no Span the Briefing is shown has a speaker")
	case !projected:
		t.Fatalf("no Span the Briefing is shown has the projection %q", projectionWords)
	case len(runs) == 0:
		t.Fatal("no two Spans the Briefing is shown are the same as each other")
	}
	return runs
}
