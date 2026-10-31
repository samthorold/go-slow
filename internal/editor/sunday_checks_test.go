package editor_test

import (
	"log/slog"
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

// assessesTheTold makes an Assessment resting on the Span the week told,
// and cites it.
const assessesTheTold = `{"assessments": [{"ref": "N1", "statement": "The Prime Minister is courting Scotland.",
		"spans": ["P1"], "assessments": [], "supersedes": ""}],
	"statements": [{"text": "The Prime Minister's week was Scotland's.", "cites": ["N1", "P1"], "stories": []}]}`

// assessedWeek is the told week with a read written on sundayMorning making
// one Assessment, A and its id, and a week told after it.
func assessedWeek(t *testing.T) (*sqlite.Store, editor.Assessment) {
	t.Helper()
	store := toldWeek(t)
	ed, logs := reader(store, &fakeAgent{turns: []editor.Turn{submits(assessesTheTold)}})
	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatal(err)
	}
	as, err := store.Assessments(t.Context(), 10)
	if err != nil || len(as) != 1 {
		t.Fatalf("Assessments = %+v, %v\n%s", as, err, logs)
	}
	toldAgain(t, store)
	return store, as[0]
}

// nextSunday is a week after sundayMorning.
var nextSunday = sundayMorning.AddDate(0, 0, 7)

// toldAgain collects a visit in the week before nextSunday and writes the
// Saturday's Briefing telling it, not yet read, so the next read has
// something told: P1 in it.
func toldAgain(t *testing.T, store *sqlite.Store) {
	t.Helper()
	ctx := t.Context()
	saturday := nextSunday.Add(-24 * time.Hour)
	if _, err := store.AddItems(ctx, []collect.Item{{SourceID: number10.ID, EntryID: "visit", Title: "PM visits Glasgow",
		Link: "https://www.gov.uk/visit", Content: "<p>The Prime Minister visited Glasgow.</p>",
		Published: saturday.Add(-48 * time.Hour), Attribution: collect.OGL, CollectedAt: saturday.Add(-time.Hour)}}); err != nil {
		t.Fatal(err)
	}
	if err := cut.New(store).CutNew(ctx, saturday.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	ed := editor.New(editor.Deps{Store: store, Briefings: store, Log: slog.New(slog.DiscardHandler),
		BriefingModel: teller{text: "The Prime Minister visited Glasgow.", tell: "visited Glasgow"}})
	if err := ed.Brief(ctx, saturday); err != nil {
		t.Fatal(err)
	}
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

func TestAnAssessmentIsKeptWithTheSpansItRestsOnAndALaterOneSupersedesIt(t *testing.T) {
	store, a := assessedWeek(t)
	// Kept with the Span it rests on, and dated: as at the Sunday it was made.
	if a.Statement != "The Prime Minister is courting Scotland." || a.AsAt.Format(time.DateOnly) != "2026-10-25" ||
		len(a.Spans) != 1 || a.Spans[0].At != backInScotland(t) || a.Day != "2026-10-25" {
		t.Errorf("Assessment = %+v", a)
	}
	r, _, _ := store.SundayRead(t.Context(), "2026-10-25")
	if c := r.Statements[0].Cites[0]; c.Kind != editor.CitesAssessment || c.Assessment.ID != a.ID {
		t.Errorf("the statement cites %+v, want the Assessment", c)
	}

	// The next read is shown it, by its ref, and supersedes it.
	ref := "A" + itoa(a.ID)
	agent := &fakeAgent{turns: []editor.Turn{
		calls("assessments", `{}`),
		submits(`{"assessments": [{"ref": "N1", "statement": "The Prime Minister is courting Scotland and its regions.",
			"spans": ["P1"], "assessments": ["` + ref + `"], "supersedes": "` + ref + `"}],
			"statements": [{"text": "The courtship goes on.", "cites": ["N1", "` + ref + `"], "stories": []}]}`),
	}}
	ed, logs := reader(store, agent)
	if err := ed.SundayRead(t.Context(), nextSunday); err != nil {
		t.Fatal(err)
	}
	shown := toolResult(t, agent, 1, 0).Content
	for _, want := range []string{ref + " (as at 2026-10-25): The Prime Minister is courting Scotland.", "rests on P2 ("} {
		if !strings.Contains(shown, want) {
			t.Errorf("the earlier Assessment is not shown with %q:\n%s", want, shown)
		}
	}
	all, err := store.Assessments(t.Context(), 10)
	if err != nil || len(all) != 2 {
		t.Fatalf("Assessments = %+v, %v\n%s", all, err, logs)
	}
	later, earlier := all[0], all[1]
	// The earlier is kept as it was, superseded, never rewritten.
	if earlier.ID != a.ID || earlier.Statement != a.Statement || earlier.SupersededBy != later.ID {
		t.Errorf("earlier = %+v, want it as it was, superseded by %d", earlier, later.ID)
	}
	if later.Supersedes != a.ID || later.AsAt.Format(time.DateOnly) != "2026-11-01" || len(later.Spans) != 1 ||
		len(later.Earlier) != 1 || later.Earlier[0].ID != a.ID {
		t.Errorf("later = %+v", later)
	}
}

func TestWrongOnAnAssessmentWithdrawsItFromLaterReads(t *testing.T) {
	store, a := assessedWeek(t)
	if _, errs, err := feedback.NewPieces(store).Give(t.Context(), feedback.Ask{Kind: "wrong", Judgement: itoa(a.ID)}, sundayMorning); err != nil || errs != nil {
		t.Fatalf("give Feedback: %v %v", errs, err)
	}
	agent := &fakeAgent{turns: []editor.Turn{calls("assessments", `{}`)}}
	ed, _ := reader(store, agent)

	if err := ed.SundayRead(t.Context(), nextSunday); err != nil {
		t.Fatal(err)
	}

	if got := toolResult(t, agent, 1, 0).Content; strings.Contains(got, "courting") {
		t.Errorf("a withdrawn Assessment is shown:\n%s", got)
	}
	// The read that made it keeps it, withdrawn, and leaves out the
	// statement citing it.
	r, _, _ := store.SundayRead(t.Context(), "2026-10-25")
	if !r.Assessments[0].Withdrawn || len(r.Statements[0].TakenOutBy) != 1 {
		t.Errorf("read = %+v", r)
	}
}

func TestASubmissionFailingTheChecksIsRefusedAndToldBackToTheAgent(t *testing.T) {
	store, a := assessedWeek(t)
	ref := "A" + itoa(a.ID)
	supersedes := func(earlier string) string {
		return `{"assessments": [{"ref": "N1", "statement": "Courting.", "spans": ["P1"], "assessments": [], "supersedes": "` + earlier + `"}],
			"statements": [{"text": "x", "cites": ["N1"], "stories": []}]}`
	}
	// A1 is shown, then superseded by a read, so the third case's earlier
	// Assessment is already superseded.
	for name, tc := range map[string]struct {
		read, says string
		looks      []string
	}{
		"a statement citing nothing": {read: uncited, says: "uncited"},
		"a ref not shown":            {read: citesWhatItWasNotShown, says: "P99"},
		"an Assessment citing no Span shown": {read: `{"assessments": [{"ref": "N1", "statement": "Courting.", "spans": ["P42"],
			"assessments": [], "supersedes": ""}], "statements": [{"text": "x", "cites": ["N1"], "stories": []}]}`, says: "P42"},
		"an Assessment citing no Span": {read: `{"assessments": [{"ref": "N1", "statement": "Courting.", "spans": [],
			"assessments": [], "supersedes": ""}], "statements": [{"text": "x", "cites": ["N1"], "stories": []}]}`, says: "cites no Span"},
		"superseding one not shown": {read: supersedes(ref), says: ref},
		"superseding one twice": {read: `{"assessments": [
			{"ref": "N1", "statement": "Courting.", "spans": ["P1"], "assessments": [], "supersedes": "` + ref + `"},
			{"ref": "N2", "statement": "Wooing.", "spans": ["P1"], "assessments": [], "supersedes": "` + ref + `"}],
			"statements": [{"text": "x", "cites": ["N1"], "stories": []}]}`, says: "already superseded", looks: []string{"assessments", `{}`}},
		"a Story holding a Span its relations do not reach": {read: `{"assessments": [], "statements": [{"text": "x",
			"cites": ["P1"], "stories": [{"span": "P1", "story": ["P1"]}]}]}`, says: "none of its relations reaches"},
	} {
		t.Run(name, func(t *testing.T) {
			var turns []editor.Turn
			if tc.looks != nil {
				turns = append(turns, calls(tc.looks...))
			}
			agent := &fakeAgent{turns: append(turns, submits(tc.read))}
			ed, _ := reader(store, agent)

			if err := ed.SundayRead(t.Context(), nextSunday); err != nil {
				t.Fatal(err)
			}

			if _, found, _ := store.SundayRead(t.Context(), "2026-11-01"); found {
				t.Fatal("a read that fails its checks was published")
			}
			told := toolResult(t, agent, len(turns)+1, 0)
			if !told.IsError || !strings.Contains(told.Content, tc.says) {
				t.Errorf("the agent was told %+v, want an error saying %q", told, tc.says)
			}
		})
	}
}

func TestARejectedReadCanBeMendedAndResubmitted(t *testing.T) {
	store := toldWeek(t)
	agent := &fakeAgent{turns: []editor.Turn{submits(uncited), submits(citesTheTold)}}
	ed, _ := reader(store, agent)

	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatal(err)
	}

	r, found, err := store.SundayRead(t.Context(), "2026-10-25")
	if err != nil || !found || len(r.Statements) != 1 || r.Turns != 2 {
		t.Errorf("read = %+v, %v, %v; want the mended one, in two turns", r, found, err)
	}
}

func TestTheReadGathersTheStoriesItTiesTogether(t *testing.T) {
	store := toldWeek(t)
	agent := &fakeAgent{turns: []editor.Turn{
		calls("story", `{"span": "P1"}`),
		submits(`{"assessments": [], "statements": [{"text": "The offer and the visit are one story.", "cites": ["P1", "P2"],
			"stories": [{"span": "P1", "story": ["P2"]}, {"span": "P2", "story": ["P1"]}]}]}`),
	}}
	ed, logs := reader(store, agent)

	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatal(err)
	}

	r, found, err := store.SundayRead(t.Context(), "2026-10-25")
	if err != nil || !found || len(r.Gathered) != 2 {
		t.Fatalf("read = %+v, %v, %v\n%s", r, found, err, logs)
	}
	// The read's is now the Span's latest gathering.
	g, gathered, err := store.LatestGathering(t.Context(), centralised(t))
	if err != nil || !gathered || !g.ByRead || g.Day != "2026-10-25" || len(g.Spans) != 1 || g.Spans[0].At != backInScotland(t) ||
		g.Conversation != r.ConversationID {
		t.Errorf("latest gathering = %+v, %v, %v", g, gathered, err)
	}
}

func TestAWeekWithNothingToldIsWrittenEmptyWithNoCall(t *testing.T) {
	store := record(t)
	agent := &fakeAgent{}
	ed, _ := reader(store, agent)

	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatal(err)
	}

	if len(agent.sent) != 0 {
		t.Errorf("the agent was called %d times with nothing told", len(agent.sent))
	}
	r, found, err := store.SundayRead(t.Context(), "2026-10-25")
	if err != nil || !found || len(r.Statements) != 0 || r.ConversationID != 0 {
		t.Errorf("read = %+v, %v, %v; want one written empty", r, found, err)
	}
}

func TestAStatementTheWeeksBriefingHadTakenOutIsNotShown(t *testing.T) {
	store := toldWeek(t)
	b, _, _ := store.Briefing(t.Context(), "2026-10-24")
	if _, errs, err := feedback.NewPieces(store).Give(t.Context(), feedback.Ask{Kind: "wrong", Judgement: itoa(b.Statements[0].ID)}, saturday.Add(2*time.Hour)); err != nil || errs != nil {
		t.Fatalf("give Feedback: %v %v", errs, err)
	}
	agent := &fakeAgent{}
	ed, _ := reader(store, agent)

	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatal(err)
	}

	// All the week told was taken out: the read is written empty.
	if len(agent.sent) != 0 {
		t.Errorf("the agent was shown a statement taken out:\n%s", agent.sent[0].Messages[0].User)
	}
}

func TestTheAgentsLoopIsBoundedInTurnsAndTokens(t *testing.T) {
	looking := calls("story", `{"span": "P1"}`)
	expensive := looking
	expensive.Usage = editor.Usage{InputTokens: editor.SundayReadTokenBudget / 3, OutputTokens: 1000}
	for name, tc := range map[string]struct {
		turn  editor.Turn
		turns int
	}{
		"turns":  {looking, editor.SundayReadMaxTurns},
		"tokens": {expensive, 3},
	} {
		t.Run(name, func(t *testing.T) {
			store := toldWeek(t)
			agent := &fakeAgent{then: &tc.turn}
			ed, _ := reader(store, agent)

			if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
				t.Fatal(err)
			}

			if len(agent.sent) != tc.turns {
				t.Errorf("the agent took %d turns, want it stopped after %d", len(agent.sent), tc.turns)
			}
			fs, err := store.SundayReadFailures(t.Context(), "2026-10-25")
			if err != nil || len(fs) != 1 || !strings.Contains(fs[0].Reason, name) {
				t.Errorf("failures = %+v, %v; want one naming the %s bound", fs, err, name)
			}
		})
	}
}

func TestAnAgentThatStopsWithoutSubmittingIsAskedToSubmit(t *testing.T) {
	store := toldWeek(t)
	agent := &fakeAgent{turns: []editor.Turn{{Text: "I have read the week."}, submits(citesTheTold)}}
	ed, _ := reader(store, agent)

	if err := ed.SundayRead(t.Context(), sundayMorning); err != nil {
		t.Fatal(err)
	}

	msgs := agent.sent[1].Messages
	if last := msgs[len(msgs)-1]; !strings.Contains(last.User, "submit_read") {
		t.Errorf("the agent was told %+v, want to be asked to submit", last)
	}
	if _, found, _ := store.SundayRead(t.Context(), "2026-10-25"); !found {
		t.Error("the read submitted once asked was not written")
	}
}
