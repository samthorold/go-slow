package web_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// sunday is the Sunday after saturday, past the Briefing's time.
var sunday = saturday.AddDate(0, 0, 1)

// submitter is an agent that submits read at once.
type submitter struct{ read string }

func (s submitter) Turn(context.Context, editor.ConversationRequest) (editor.Turn, error) {
	return editor.Turn{Model: "fake-agent", Calls: []editor.ToolCall{{ID: "1", Name: "submit_read", Input: json.RawMessage(s.read)}}}, nil
}

// writeSundayRead writes the Sunday read at at, as an agent submitting read.
func writeSundayRead(t *testing.T, store *sqlite.Store, at time.Time, read string) {
	t.Helper()
	ed := editor.New(editor.Deps{Store: store, SundayReads: store, SundayReadModel: submitter{read},
		Wikidata: noWikidata{}, Log: slog.New(slog.DiscardHandler)})
	if err := ed.SundayRead(t.Context(), at); err != nil {
		t.Fatal(err)
	}
}

const assessed = `{"statements": [
	{"text": "The Prime Minister is <b hx-get=\"/evil\">courting</b> Scotland.", "cites": ["N1", "S1:F1"]}],
	"assessments": [{"ref": "N1", "statement": "The Prime Minister is courting Scottish voters.", "facts": ["F1"], "supersedes": ""}],
	"stories": []}`

func TestTheSundayReadHasItsOwnPageLinkedFromThatDaysBriefing(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	writeBriefing(t, store, sunday, `{}`)
	writeSundayRead(t, store, sunday, assessed)
	app, _ := newAppWithLog(t, sunday, store)

	briefing := get(t, app, "/")
	if !strings.Contains(briefing.body, `href="/sunday-reads/2026-10-25"`) {
		t.Fatalf("Sunday's Briefing does not link the Sunday read:\n%s", briefing.body)
	}
	if res := get(t, app, "/briefings/2026-10-24"); strings.Contains(res.body, "/sunday-reads/") {
		t.Errorf("Saturday's Briefing links a Sunday read:\n%s", res.body)
	}

	res := get(t, app, "/sunday-reads/2026-10-25")
	if res.status != 200 || !strings.Contains(res.body, "<!doctype html>") {
		t.Fatalf("status %d, not a full page:\n%s", res.status, res.body)
	}
	// The statement is the Editor's words: escaped and inert.
	if strings.Contains(res.body, "<b hx-get") || !regexp.MustCompile(`hx-disable>The Prime Minister is &lt;b`).MatchString(res.body) {
		t.Errorf("the statement is not escaped inside hx-disable:\n%s", res.body)
	}
	// Each citation traces: the Assessment, at its own anchor with the Facts
	// it rests on, and the Development on its Story's Timeline.
	for _, want := range []string{
		`href="#assessment-1"`, `id="assessment-1"`, "The Prime Minister is courting Scottish voters.",
		`href="/stories/1#development-1"`, "Andy Burnham spoke in Motherwell.",
	} {
		if !strings.Contains(res.body, want) {
			t.Errorf("the read's page does not show %q:\n%s", want, res.body)
		}
	}
}

func TestASundayWithNoReadSaysSoAndOtherDaysHaveNone(t *testing.T) {
	app := newApp(t, sunday)

	if res := get(t, app, "/sunday-reads/2026-10-25"); res.status != 200 || !strings.Contains(res.body, "There is no Sunday read yet.") {
		t.Errorf("status %d:\n%s", res.status, res.body)
	}
	for _, path := range []string{"/sunday-reads/2026-10-24", "/sunday-reads/2026-11-01", "/sunday-reads/soon"} {
		if res := get(t, app, path); res.status != 404 {
			t.Errorf("%s: status = %d, want 404", path, res.status)
		}
	}
}
