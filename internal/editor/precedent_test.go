package editor_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/collect"
	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/feedback"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// byTitle answers each request with the output kept for the first title its
// input names, and keeps the requests it was sent.
type byTitle struct {
	outputs map[string]string
	sent    []editor.Request
}

func (m *byTitle) Complete(_ context.Context, r editor.Request) (editor.Response, error) {
	m.sent = append(m.sent, r)
	for title, out := range m.outputs {
		if strings.Contains(r.Input, title) {
			return editor.Response{Output: json.RawMessage(out), Model: "fake"}, nil
		}
	}
	return editor.Response{Output: json.RawMessage(emptyReading), Model: "fake"}, nil
}

const emptyReading = `{"names": [], "refers_to": [], "rejected": [], "speakers": []}`

// sentFor is the request m was sent about the Item titled title.
func (m *byTitle) sentFor(t *testing.T, title string) editor.Request {
	t.Helper()
	for _, r := range m.sent {
		if strings.Contains(r.Input, "Title: "+title+"\n") {
			return r
		}
	}
	t.Fatalf("no request about %q", title)
	return editor.Request{}
}

// derbyLater is an Item published after the release, that names Derby too.
var derbyLater = editor.Item{
	SourceID: number10.ID, EntryID: "derby-later", Title: "Work begins in Derby",
	Content:   "<p>Work began in Derby today. The city welcomed it.</p>",
	Published: release.Published.Add(48 * time.Hour),
}

// collectItem adds it to the Record and cuts it, on Saturday.
func collectItem(t *testing.T, store *sqlite.Store, it editor.Item) {
	t.Helper()
	collectItemAt(t, store, it, saturday)
}

// collectItemAt adds it to the Record and cuts it, at at.
func collectItemAt(t *testing.T, store *sqlite.Store, it editor.Item, at time.Time) {
	t.Helper()
	if _, err := store.AddItems(t.Context(), []collect.Item{{
		SourceID: it.SourceID, EntryID: it.EntryID, Title: it.Title, Link: "https://www.gov.uk/" + it.EntryID,
		Content: it.Content, Published: it.Published, Attribution: collect.OGL, CollectedAt: at,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := cut.New(store).CutNew(t.Context(), at); err != nil {
		t.Fatal(err)
	}
}

// releaseRead is a Record holding the speech and the release, both read,
// the release with releaseReading, and the model that read them.
func releaseRead(t *testing.T) (*sqlite.Store, *byTitle) {
	t.Helper()
	store := record(t)
	collectItem(t, store, release)
	model := &byTitle{outputs: map[string]string{release.Title: releaseReading, speech.Title: goodReading}}
	ed, _ := newEditor(store, model, &fakeWikidata{})
	if err := ed.ReadWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	return store, model
}

// giveWrong gives wrong on the judgement id, at at.
func giveWrong(t *testing.T, store *sqlite.Store, id int64, at time.Time) feedback.Piece {
	t.Helper()
	p, errs, err := feedback.NewPieces(store).Give(t.Context(), feedback.Ask{Kind: "wrong", Judgement: strconv.FormatInt(id, 10)}, at)
	if err != nil || errs != nil {
		t.Fatalf("give wrong on %d: %v %v", id, errs, err)
	}
	return p
}

// releaseRefersTo is the release's reading's refers to from the words.
func releaseRefersTo(t *testing.T, store *sqlite.Store, from string) editor.RefersTo {
	t.Helper()
	r, _, err := store.NewestReading(t.Context(), release.SourceID, release.EntryID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range r.RefersTo {
		if spanWords(release, ref.From) == from {
			return ref
		}
	}
	t.Fatalf("the release's reading has no refers to from %q: %+v", from, r.RefersTo)
	return editor.RefersTo{}
}

// Reading is shown no precedents yet (system design: Feedback becomes
// precedent, shown as cases): wrong standing on a reference naming what an
// Item names leaves its request exactly as it would be with none.
func TestReadingIsShownNoPrecedentsEvenWithWrongStandingOnAReferenceNamingWhatTheItemNames(t *testing.T) {
	store, _ := releaseRead(t)
	giveWrong(t, store, releaseRefersTo(t, store, "the city").ID, saturday.Add(time.Hour))
	collectItem(t, store, derbyLater)
	model := &byTitle{}
	ed, _ := newEditor(store, model, &fakeWikidata{})

	if err := ed.ReadWaiting(t.Context(), saturday.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}

	without := &fakeModel{output: emptyReading}
	if _, err := editor.Read(t.Context(), without, "", toRead(derbyLater)); err != nil {
		t.Fatal(err)
	}
	if got := model.sentFor(t, derbyLater.Title); editor.RequestHash(got, "m") != editor.RequestHash(without.sent[0], "m") {
		t.Errorf("reading an Item naming Derby is sent:\n%s\nnot as with no Feedback:\n%s", got.Input, without.sent[0].Input)
	}
	r, _, err := store.NewestReading(t.Context(), derbyLater.SourceID, derbyLater.EntryID)
	if err != nil {
		t.Fatal(err)
	}
	if c, _, err := store.Call(t.Context(), r.Call); err != nil || len(c.Precedents) != 0 {
		t.Errorf("reading's call keeps the precedents %v (%v), want none", c.Precedents, err)
	}
}

// relatedWithWrong is a Record where the Holyrood speech's S2 was related
// as the same as the press release's, wrong given on that, and undone if
// undo; then a later Item naming Scotland too, related: the Record, the
// request relating it, and the wrong.
func relatedWithWrong(t *testing.T, undo bool) (*sqlite.Store, editor.Request, feedback.Piece) {
	t.Helper()
	store := record(t)
	readNaming(t, store, pressRelease, "Scotland")
	readNaming(t, store, holyrood, "Scotland")
	ed, _ := relatingEditor(store, &fakeModel{output: `{"relations": [{"span": "S2", "to": "C1", "verdict": "same_as"}]}`})
	if err := ed.RelateWaiting(t.Context(), saturday); err != nil {
		t.Fatal(err)
	}
	rels := relationsOf(t, store, holyrood, 2)
	if len(rels) != 1 {
		t.Fatalf("the speech's S2 has relations %+v, want one", rels)
	}
	wrong := giveWrong(t, store, rels[0].ID, saturday.Add(time.Hour))
	if undo {
		if _, err := feedback.NewPieces(store).Undo(t.Context(), wrong.ID, saturday.Add(90*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	later := holyrood
	later.EntryID, later.Title, later.Published = "later", "Scotland's regions respond", holyrood.Published.AddDate(0, 0, 1)
	readNaming(t, store, later, "Scotland")
	model := &fakeModel{output: `{"relations": []}`}
	ed, _ = relatingEditor(store, model)
	if err := ed.RelateWaiting(t.Context(), saturday.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(model.sent) != 1 {
		t.Fatalf("relating the later Item sent %d requests, want one", len(model.sent))
	}
	return store, model.sent[0], wrong
}

func TestRelatingIsShownStandingWrongOnRelationsBetweenSpansMentioningWhatItsSpansMention(t *testing.T) {
	store, req, wrong := relatedWithWrong(t, false)
	_, plain, _ := relatedWithWrong(t, true)

	before, after, found := strings.Cut(req.Input, "\nPrecedents")
	if !found {
		t.Fatalf("no precedents shown:\n%s", req.Input)
	}
	if strings.Contains(plain.Input, "Precedents") {
		t.Errorf("undone Feedback is shown as precedent:\n%s", plain.Input)
	}
	if req.Instructions != plain.Instructions || before != plain.Input {
		t.Errorf("with precedents, the request before them is not the request with the wrong undone:\n%s\n---\n%s", before, plain.Input)
	}
	lines := strings.Split(strings.TrimSpace(after), "\n")
	if len(lines) != 2 {
		t.Fatalf("precedents shown as %q, want a heading and one line", lines)
	}
	var p struct {
		Feedback, Judgement string
		Span, To            struct{ Item, Published, Words string }
		Given               string
	}
	if err := json.Unmarshal([]byte(lines[1]), &p); err != nil {
		t.Fatalf("a precedent is not a JSON object: %v: %s", err, lines[1])
	}
	if !strings.HasPrefix(p.Feedback, "wrong: ") || p.Judgement != "same_as" || p.Span.Item != holyrood.Title ||
		p.Span.Words != "Scotland is one of the most centralised nations in Europe." || p.To.Item != pressRelease.Title ||
		p.Span.Published != "2026-10-16" || p.Given != "2026-10-24" {
		t.Errorf("precedent shown as %s", lines[1])
	}
	calls, err := store.CallsOn(t.Context(), "relating", holyrood.SourceID+"\nlater")
	if err != nil || len(calls) != 1 || len(calls[0].Precedents) != 1 || calls[0].Precedents[0] != wrong.ID {
		t.Errorf("relating's call keeps the precedents %+v (%v), want [%d]", calls, err, wrong.ID)
	}
}

// giveOnSpan gives unwanted (on the Briefing for day) or missed on the Span
// at a, at at.
func giveOnSpan(t *testing.T, store *sqlite.Store, kind string, a editor.Address, day string, at time.Time) feedback.Piece {
	t.Helper()
	p, errs, err := feedback.NewPieces(store).Give(t.Context(), feedback.Ask{Kind: kind, SourceID: a.SourceID, EntryID: a.EntryID,
		Span: fmt.Sprintf("%d-%d", a.Span.Start, a.Span.End), Day: day}, at)
	if err != nil || errs != nil {
		t.Fatalf("give %s: %v %v", kind, errs, err)
	}
	return p
}

func TestTheBriefingIsShownStandingUnwantedAndMissedNewestFirstAndKeepsThem(t *testing.T) {
	store := record(t)
	ed, _ := briefer(store, &fakeModel{output: spokeInMotherwell})
	if err := ed.Brief(t.Context(), saturday.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	unwanted := giveOnSpan(t, store, "unwanted", speechSpan(1), "2026-10-24", saturday.Add(2*time.Hour))
	missed := giveOnSpan(t, store, "missed", speechSpan(2), "", saturday.Add(3*time.Hour))
	undone := giveOnSpan(t, store, "missed", speechSpan(2), "", saturday.Add(4*time.Hour))
	if _, err := feedback.NewPieces(store).Undo(t.Context(), undone.ID, saturday.Add(5*time.Hour)); err != nil {
		t.Fatal(err)
	}
	collectItemAt(t, store, derbyLater, saturday.Add(6*time.Hour))
	writer := &fakeModel{output: `{"statements": [{"spans": ["S1"], "stories": [], "text": "Work began in Derby."}]}`}
	ed, _ = briefer(store, writer)

	sunday := saturday.AddDate(0, 0, 1)
	if err := ed.Brief(t.Context(), sunday); err != nil {
		t.Fatal(err)
	}

	if len(writer.sent) != 1 {
		t.Fatalf("sent %d requests, want one", len(writer.sent))
	}
	_, after, found := strings.Cut(writer.sent[0].Input, "\nPrecedents")
	if !found {
		t.Fatalf("no precedents shown:\n%s", writer.sent[0].Input)
	}
	lines := strings.Split(strings.TrimSpace(after), "\n")
	if len(lines) != 3 {
		t.Fatalf("precedents shown as %q, want a heading and two lines", lines)
	}
	var first, second map[string]string
	if err := json.Unmarshal([]byte(lines[1]), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[2]), &second); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first["feedback"], "missed: ") || first["span"] != "Scotland will get £2 billion." || first["item"] != speech.Title ||
		first["given"] != "2026-10-24" {
		t.Errorf("the newest precedent is shown as %s, want the missed", lines[1])
	}
	if !strings.HasPrefix(second["feedback"], "unwanted: ") || second["told"] != "2026-10-24" ||
		second["span"] != "It’s good to be back in Scotland, to be here in Motherwell." {
		t.Errorf("the older precedent is shown as %s, want the unwanted", lines[2])
	}
	b, _, err := store.Briefing(t.Context(), "2026-10-25")
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{missed.ID, unwanted.ID}; !slices.Equal(b.Precedents, want) {
		t.Errorf("the Briefing keeps the precedents %v, want %v", b.Precedents, want)
	}
	if c, _, err := store.Call(t.Context(), b.Call); err != nil || !slices.Equal(c.Precedents, []int64{missed.ID, unwanted.ID}) {
		t.Errorf("its call keeps the precedents %v (%v)", c.Precedents, err)
	}
}

// ids are the precedents' Feedback ids, in order.
func ids(ps []editor.Precedent) []int64 {
	var out []int64
	for _, p := range ps {
		out = append(out, p.FeedbackID)
	}
	return out
}

func TestTasteFadesWithAgeTheNewerUnwantedShownFirstAndTheOlderDroppingOutAtTheCapFirst(t *testing.T) {
	var taste []editor.Precedent
	// Given oldest first: Feedback 1 is the oldest.
	for i := range editor.PrecedentsShown + 1 {
		taste = append(taste, editor.Precedent{FeedbackID: int64(i + 1), Kind: "unwanted", GivenAt: saturday.AddDate(0, 0, i)})
	}

	shown := ids(editor.BriefingPrecedents(taste))

	if len(shown) != editor.PrecedentsShown || shown[0] != int64(editor.PrecedentsShown+1) || slices.Contains(shown, 1) {
		t.Errorf("shown %v, want the newest first, at most %d, and the oldest (1) dropped", shown, editor.PrecedentsShown)
	}
	if two := ids(editor.BriefingPrecedents(taste[:2])); !slices.Equal(two, []int64{2, 1}) {
		t.Errorf("of two unwanted, shown %v, want the newer first", two)
	}
}

func TestTruthDoesNotFadeAnOlderWrongSharingMoreIsShownBeforeNewerAndKeptAtTheCap(t *testing.T) {
	derby := editor.NamedEntity{Name: "Derby", Kind: "place"}
	leeds := editor.NamedEntity{Name: "Leeds", Kind: "place"}
	burnham := editor.NamedEntity{Name: "Andy Burnham", Kind: "person"}
	tr := editor.ToRelate{Spans: []editor.SpanToRelate{{Entities: []editor.NamedEntity{burnham, derby, leeds}}}}
	spans := func(es ...editor.NamedEntity) (editor.OtherSpan, editor.OtherSpan) {
		return editor.OtherSpan{Entities: es[:1]}, editor.OtherSpan{Entities: es[1:]}
	}
	at, to := spans(burnham, derby, leeds)
	wrong := []editor.Precedent{{FeedbackID: 1, Kind: "wrong", Judged: "relation", GivenAt: saturday.AddDate(-1, 0, 0), At: at, To: to}}
	for i := range editor.PrecedentsShown {
		at, to := spans(derby, derby)
		wrong = append(wrong, editor.Precedent{FeedbackID: int64(i + 2), Kind: "wrong", Judged: "relation",
			GivenAt: saturday.AddDate(0, 0, i), At: at, To: to})
	}
	// Wrong between Spans mentioning nothing the case's Spans mention is not
	// relating's precedent.
	at, to = spans(editor.NamedEntity{Name: "Motherwell", Kind: "place"}, editor.NamedEntity{Name: "Motherwell", Kind: "place"})
	wrong = append(wrong, editor.Precedent{FeedbackID: 91, Kind: "wrong", Judged: "relation", GivenAt: saturday, At: at, To: to})

	shown := ids(editor.RelatingPrecedents(wrong, tr))

	if len(shown) != editor.PrecedentsShown || shown[0] != 1 {
		t.Errorf("shown %v, want the year-old wrong sharing three Entities first, %d at most", shown, editor.PrecedentsShown)
	}
	if slices.Contains(shown, 91) {
		t.Errorf("shown %v, want no wrong sharing nothing with the case", shown)
	}
}
