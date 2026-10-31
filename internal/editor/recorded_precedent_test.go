package editor_test

import (
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// The recorded runs of precedent (system design: Feedback becomes
// precedent, shown as cases): for reading, relating and the Briefing, a case
// recorded with one piece of hand-made Feedback standing, on the fixtures,
// over the precedents' own frozen copies of the readings and relatings
// (testdata/recorded/precedent/inputs/{reading,relating}, copied from the
// first samples, as the Briefing's are). Each names the Feedback it rests
// on and fails unless the act is shown it, and asserts a property the
// precedent calls for: that the judge doesn't repeat what the Owner said
// was wrong or unwanted, on a case like it, and still judges what is sound.
// Each is sampled five times and kept under
// testdata/recorded/precedent/<case>/:
//
//	go test -count=1 ./internal/editor -run TestRecordedPrecedentReading -record   (READING_MODEL set)
//	go test -count=1 ./internal/editor -run TestRecordedPrecedentRelating -record  (RELATING_MODEL set)
//	go test -count=1 ./internal/editor -run TestRecordedPrecedentBriefing -record  (BRIEFING_MODEL set)
//
// with GOSLOW_ANTHROPIC_API_KEY set too.
var (
	readingPrecedentCommand  = rerecordCmd{"TestRecordedPrecedentReading", "READING_MODEL"}
	relatingPrecedentCommand = rerecordCmd{"TestRecordedPrecedentRelating", "RELATING_MODEL"}
	briefingPrecedentCommand = rerecordCmd{"TestRecordedPrecedentBriefing", "BRIEFING_MODEL"}
	// precedentRule is the daily acts' own.
	precedentRule = dailyRule
)

// The fixtures the precedents' cases are on.
const (
	railways        = "great-british-railways-takes-shape-as-hq-and-apprenticeship-scheme-confirmed"
	commons         = "pm-statement-to-the-house-of-commons-12-october-2026"
	countyTyrone    = "appointment-of-lord-lieutenant-of-county-tyrone-15-october-2026"
	homeWords       = "the place they call home"
	seriousAbout    = "Ukraine has repeatedly demonstrated it is serious about peace"
	threeWeeks      = "In the three weeks since that meeting"
	rejecting       = "Rejecting aggression wherever we see it"
	remainCommitted = "committed to supporting Ukraine for as long as it takes"
)

// precedentInputs is the precedents' frozen copies of upstream act's
// recordings, under root.
func precedentInputs(root, upstream string) string {
	return filepath.Join(root, "precedent", "inputs", upstream)
}

// fixtureNamed is the fixture Item kept under name, and its place in the
// order the fixtures were published.
func fixtureNamed(t *testing.T, name string) (namedItem, int) {
	t.Helper()
	fs := fixtureItems(t)
	i := slices.IndexFunc(fs, func(f namedItem) bool { return f.name == name })
	if i < 0 {
		t.Fatalf("no fixture %s", name)
	}
	return fs[i], i
}

// The case for reading: the Owner said the railways release's "the place
// they call home" does not refer to Derby (the release never says where the
// young people's home is), and the release is read again, as re-judging
// reads it: the case most like the one the Feedback was given on.
func TestRecordedPrecedentReading(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	root := ownDirectory(t, "precedent")
	f, _ := fixtureNamed(t, railways)
	it := f.item
	store, topics := newRecord(t, nil)
	if err := (chain{reading: replayed(precedentInputs(root, "reading"))}).run(ctx, store, topics, []namedItem{f}); err != nil {
		t.Fatal(err)
	}
	r, _, err := store.NewestReading(ctx, it.SourceID, it.EntryID)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(r.RefersTo, func(ref editor.RefersTo) bool {
		return cut.Words(it.Content, ref.From.Span) == homeWords && strings.Contains(cut.Words(it.Content, ref.To.Span), "Derby")
	})
	if i < 0 {
		t.Fatalf("the frozen reading no longer refers %q to Derby, the case the Feedback is given on", homeWords)
	}
	wrong := giveWrong(t, store, r.RefersTo[i].ID, saturday.Add(time.Hour))
	standing, err := store.StandingWrong(ctx, []string{"refers_to", "speaker"})
	if err != nil {
		t.Fatal(err)
	}
	c, _, _, err := store.NewestCut(ctx, it.SourceID, it.EntryID)
	if err != nil {
		t.Fatal(err)
	}
	tr := editor.ToRead{Item: it, Cut: c, Precedents: editor.ReadingPrecedents(standing, it)}
	if got := ids(tr.Precedents); !slices.Equal(got, []int64{wrong.ID}) {
		t.Fatalf("reading is shown the precedents %v, want the case's wrong, %d", got, wrong.ID)
	}

	for _, err := range sampled(t, filepath.Join(root, "precedent"), "reading-railways", precedentRule, readingPrecedentCommand,
		func(m *sampleModel) sampleResult {
			var r editor.Reading
			passed, err := runSample(m, func(model editor.Model) (bool, error) {
				var err error
				r, err = editor.Read(ctx, model, m.model, tr)
				return err == nil, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if !passed {
				return sampleResult{}
			}
			p := properties{}
			again := p.property("a reference the Owner said was wrong is not judged again")
			for _, ref := range r.RefersTo {
				if strings.Contains(cut.Words(it.Content, ref.From.Span), "place they call home") &&
					strings.Contains(cut.Words(it.Content, ref.To.Span), "Derby") {
					again("%q refers to %q", cut.Words(it.Content, ref.From.Span), cut.Words(it.Content, ref.To.Span))
				}
			}
			for _, ref := range references[railways] {
				referenceFollowed(p, readingOf{it: it, r: r, spans: c.Spans}, ref)
			}
			return sampleResult{passed: true, held: p}
		}) {
		t.Error(err)
	}
}

// The case for relating: the Owner said the Commons statement's "In the
// three weeks since that meeting, Russian strikes have intensified..." is
// not the same as the response to the US-Russia agreement's "Ukraine has
// repeatedly demonstrated it is serious about peace...": both tell of
// attacks on civilians, but say different things. The statement is related
// again, as re-judging would relate it: the case most like the one the
// Feedback was given on. (A probe relating the visit's press release, whose
// "As Russia continues its bombardment..." stands to the same Span, carried
// the precedent to no other pair: see the PR that added this.)
func TestRecordedPrecedentRelating(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	root := ownDirectory(t, "precedent")
	fixtures := fixtureItems(t)
	cm, k := fixtureNamed(t, commons)
	content := map[string]string{}
	for _, f := range fixtures {
		content[f.item.EntryID] = f.item.Content
	}
	words := func(a editor.Address) string { return cut.Words(content[a.EntryID], a.Span) }
	between := func(r editor.Relation, a, b string) bool {
		return (strings.Contains(words(r.From), a) && strings.Contains(words(r.To), b)) ||
			(strings.Contains(words(r.From), b) && strings.Contains(words(r.To), a))
	}

	// The fixtures before the statement read and related, and the statement
	// read; then the statement as relating is shown it, and related.
	store, topics := newRecord(t, nil)
	readings, relatings := replayed(precedentInputs(root, "reading")), replayed(precedentInputs(root, "relating"))
	if err := (chain{reading: readings, relating: relatings}).run(ctx, store, topics, fixtures[:k]); err != nil {
		t.Fatal(err)
	}
	if err := (chain{reading: readings}).run(ctx, store, topics, fixtures[k:k+1]); err != nil {
		t.Fatal(err)
	}
	ed, _ := relatingEditor(store, nil)
	waiting, err := ed.ToRelate(ctx)
	if err != nil || len(waiting) != 1 || waiting[0].Item.EntryID != cm.item.EntryID {
		t.Fatalf("awaiting relating: %+v, %v; want the statement", waiting, err)
	}
	tr := waiting[0]
	related, err := relatings(commons)
	if err != nil {
		t.Fatal(err)
	}
	if err := editor.New(editor.Deps{Store: store, RelatingModel: related, Log: slog.New(slog.DiscardHandler)}).RelateWaiting(ctx, saturday); err != nil {
		t.Fatal(err)
	}
	var wrongOn int64
	for _, sp := range cut.Sentences(cm.item.Content) {
		rels, err := store.RelationsOf(ctx, editor.Address{SourceID: cm.item.SourceID, EntryID: cm.item.EntryID, Span: sp})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rels {
			if r.Relation.Kind == editor.SameAs && between(r.Relation, threeWeeks, seriousAbout) {
				wrongOn = r.ID
			}
		}
	}
	if wrongOn == 0 {
		t.Fatalf("the frozen relatings no longer judge %q the same as %q, the case the Feedback is given on", threeWeeks, seriousAbout)
	}
	wrong := giveWrong(t, store, wrongOn, saturday.Add(time.Hour))
	standing, err := store.StandingWrong(ctx, []string{"relation"})
	if err != nil {
		t.Fatal(err)
	}
	tr.Precedents = editor.RelatingPrecedents(standing, tr)
	if got := ids(tr.Precedents); !slices.Equal(got, []int64{wrong.ID}) {
		t.Fatalf("relating is shown the precedents %v, want the case's wrong, %d", got, wrong.ID)
	}

	for _, err := range sampled(t, filepath.Join(root, "precedent"), "relating-commons", precedentRule, relatingPrecedentCommand,
		func(m *sampleModel) sampleResult {
			var r editor.Relating
			passed, err := runSample(m, func(model editor.Model) (bool, error) {
				var err error
				r, err = editor.Relate(ctx, model, m.model, tr)
				return err == nil, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if !passed {
				return sampleResult{}
			}
			p := properties{}
			again := p.property("a same as the Owner said was wrong is not judged again")
			for _, rel := range r.Relations {
				if rel.Kind == editor.SameAs && between(rel, threeWeeks, seriousAbout) {
					again("%q is judged the same as %q", words(rel.From), words(rel.To))
				}
			}
			p.holds("a sound same as is still judged", slices.ContainsFunc(r.Relations, func(rel editor.Relation) bool {
				return rel.Kind == editor.SameAs && between(rel, rejecting, remainCommitted)
			}), "the statement's %q is not the same as the response's %q", rejecting, remainCommitted)
			return sampleResult{passed: true, held: p}
		}) {
		t.Error(err)
	}
}

// The case for the Briefing: a hand-made Friday Briefing told the
// appointment of the Lord-Lieutenant of County Tyrone, a ceremonial Crown
// appointment, and the Owner said it was unwanted. Saturday's Briefing is
// written over every fixture, as the Briefing's own recordings are, the
// appointment among them, which it tells in every one of them unshown: the
// case most like the one the Feedback was given on. (Probes with the
// Feedback on another Crown appointment, the Regius Professorship of
// Medicine at Oxford, carried the precedent to none: see the PR that added
// this.)
func TestRecordedPrecedentBriefing(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	root := ownDirectory(t, "precedent")
	tyrone, _ := fixtureNamed(t, countyTyrone)
	friday := saturday.Add(-time.Hour)

	setup := filepath.Join(t.TempDir(), "setup.db")
	b, err := os.ReadFile(spanRecordFile(t, filepath.Join(root, "precedent", "inputs")))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(setup, b, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(ctx, setup)
	if err != nil {
		t.Fatal(err)
	}
	it := tyrone.item
	spans := cut.Sentences(it.Content)
	i := slices.IndexFunc(spans, func(sp cut.Span) bool {
		w := cut.Words(it.Content, sp)
		return strings.Contains(w, "appoint") && strings.Contains(w, "Lord-Lieutenant of County Tyrone")
	})
	if i < 0 {
		t.Fatal("the County Tyrone appointment has no Span telling the appointment, the case the Feedback is given on")
	}
	at := editor.Address{SourceID: it.SourceID, EntryID: it.EntryID, Span: spans[i]}
	if err := store.KeepBriefing(ctx, editor.Briefing{
		Day: "2026-10-23", WrittenAt: friday, Judge: editor.Judge{Act: "briefing", Version: editor.BriefingVersion, Model: "hand-made"},
		Statements: []editor.Statement{{
			Text:  "William James Baxter has been appointed Lord-Lieutenant of County Tyrone.",
			Cites: []editor.OtherSpan{{At: at, Item: it}},
		}},
		Gathered: []editor.Gathering{{From: at}},
	}); err != nil {
		t.Fatal(err)
	}
	unwanted := giveOnSpan(t, store, "unwanted", at, "2026-10-23", friday.Add(30*time.Minute))
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	for _, err := range sampled(t, filepath.Join(root, "precedent"), "briefing-saturday", precedentRule, briefingPrecedentCommand,
		func(m *sampleModel) sampleResult {
			store := copyOfRecord(t, setup)
			if !briefingSample(t, store, m) {
				return sampleResult{}
			}
			b, found, err := store.Briefing(ctx, briefingAt.Format(time.DateOnly))
			if err != nil || !found {
				t.Fatalf("no Briefing written: %v %v", found, err)
			}
			if !slices.Equal(b.Precedents, []int64{unwanted.ID}) {
				t.Fatalf("the Briefing is shown the precedents %v, want the case's unwanted, %d", b.Precedents, unwanted.ID)
			}
			p := properties{}
			p.holds("the Briefing tells something", len(b.Statements) > 0, "the Briefing makes no statement")
			like := p.property("an appointment like one the Owner said was unwanted is not told")
			for _, st := range b.Statements {
				if slices.ContainsFunc(st.Cites, func(c editor.OtherSpan) bool { return c.At.EntryID == tyrone.item.EntryID }) {
					like("%q tells the County Tyrone appointment", st.Text)
				}
			}
			return sampleResult{passed: true, held: p}
		}) {
		t.Error(err)
	}
}
