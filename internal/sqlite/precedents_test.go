package sqlite

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/feedback"
)

func TestWrongOnARelationIsPrecedentWhileItStandsAndARelatingKeepsThePrecedentsItWasShown(t *testing.T) {
	s := relatingRecord(t)
	tc := collected(t, s, "tuesday", tuesdayText, tuesday)
	read(t, s, "tuesday", tuesdayText, tc, []named{scotland})
	mc := collected(t, s, "monday", mondayText, monday)
	mr := read(t, s, "monday", mondayText, mc, []named{scotland})
	tue := editor.Address{SourceID: "number-10", EntryID: "tuesday", Span: tc.Spans[0]}
	mon := editor.Address{SourceID: "number-10", EntryID: "monday", Span: mc.Spans[0]}
	judge := editor.Judge{Act: "relating", Version: 1, Model: "claude-test", RequestHash: "rel-hash"}
	if err := s.KeepRelating(t.Context(), editor.Relating{SourceID: "number-10", EntryID: "monday", ReadingID: mr.ID,
		At: cutAt.Add(2 * time.Hour), Judge: judge, Relations: []editor.Relation{{Kind: editor.Follows, From: tue, To: mon}}}); err != nil {
		t.Fatal(err)
	}
	rels, err := s.RelationsOf(t.Context(), tue)
	if err != nil || len(rels) != 1 {
		t.Fatalf("relations %+v, %v", rels, err)
	}
	pieces := feedback.NewPieces(s)
	given := cutAt.Add(3 * time.Hour)
	p, errs, err := pieces.Give(t.Context(), feedback.Ask{Kind: "wrong", Judgement: strconv.FormatInt(rels[0].ID, 10)}, given)
	if err != nil || errs != nil {
		t.Fatalf("give: %v %v", errs, err)
	}

	ps, err := s.StandingWrong(t.Context(), []string{"relation"})
	if err != nil || len(ps) != 1 {
		t.Fatalf("standing wrong %+v, %v", ps, err)
	}
	got := ps[0]
	if got.FeedbackID != p.ID || got.Kind != "wrong" || got.Judged != "relation" || got.Relation != editor.Follows ||
		!got.GivenAt.Equal(given) || got.At.At != tue || got.To.At != mon ||
		!strings.HasPrefix(got.To.Words, "Scotland is one") || got.At.Item.Title == "" {
		t.Errorf("precedent %+v", got)
	}
	if !slices.ContainsFunc(got.At.Entities, func(e editor.NamedEntity) bool { return e.Name == "Scotland" }) {
		t.Errorf("its Span mentions %+v, want Scotland", got.At.Entities)
	}
	if ps, _ := s.StandingWrong(t.Context(), []string{"refers_to", "speaker"}); len(ps) != 0 {
		t.Errorf("wrong on a relation is precedent for reading: %+v", ps)
	}

	r := editor.Relating{SourceID: "number-10", EntryID: "monday", ReadingID: mr.ID, At: given.Add(time.Hour), Judge: judge,
		Precedents: []int64{p.ID}}
	if err := s.KeepRelating(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := s.db.QueryRowContext(t.Context(), `SELECT max(event_id) FROM relatings`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if shown, err := s.shownWith(t.Context(), id); err != nil || !slices.Equal(shown, []int64{p.ID}) {
		t.Errorf("the relating keeps the precedents %v (%v), want [%d]", shown, err, p.ID)
	}

	if _, err := pieces.Undo(t.Context(), p.ID, given.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if ps, err := s.StandingWrong(t.Context(), []string{"relation"}); err != nil || len(ps) != 0 {
		t.Errorf("undone wrong is precedent: %+v, %v", ps, err)
	}
}
