package editor_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/sqlite"
)

// The Sunday read's samples, recorded and replayed on a fake conversation
// over a small Record: the harness around the act, with no model call.

// placedRecord is a fresh Record for each try: the speech read and placed,
// so citesTheSpeech is a read the act's checks take.
func placedRecord(t *testing.T) func() *sqlite.Store {
	return func() *sqlite.Store {
		store := record(t)
		readAndPlace(t, store, &fakeModel{output: startsAStory})
		return store
	}
}

// tries is a fake conversation that runs one script per try, starting the
// next script whenever it is sent a conversation's opening request, and
// keeps how many tries it was asked.
type tries struct {
	scripts [][]editor.Turn
	asked   int
	agent   *fakeAgent
}

func (f *tries) Turn(ctx context.Context, r editor.ConversationRequest) (editor.Turn, error) {
	if len(r.Messages) == 1 {
		script := f.scripts[min(f.asked, len(f.scripts)-1)]
		f.asked++
		f.agent = &fakeAgent{turns: script}
	}
	return f.agent.Turn(ctx, r)
}

// withRaw is t with a provider form of its own.
func withRaw(t editor.Turn, raw string) editor.Turn {
	t.Raw = json.RawMessage(raw)
	return t
}

// passingTry looks at a Story, then submits a read the checks take.
var passingTry = []editor.Turn{
	withRaw(calls("story", `{"story": "S1"}`), `{"content":[{"type":"thinking","signature":"a"}]}`),
	withRaw(submits(citesTheSpeech), `{"content":[{"type":"thinking","signature":"b"}]}`),
}

// failingTry submits a read the checks refuse, then stops.
var failingTry = []editor.Turn{withRaw(submits(uncited), `{"content":[]}`)}

// noNetwork is Wikidata reached live in a test: it answers nothing.
type noNetwork struct{}

func (noNetwork) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("no network in a test")
}

// recordSundaySamples records the Sunday read's samples under dir on live.
func recordSundaySamples(t *testing.T, dir string, live editor.Conversation, maxTries int) []error {
	t.Helper()
	return sundaySampled(t, dir, placedRecord(t), &sundayLive{conv: live, model: "claude-opus-5-5", wikidata: noNetwork{}, maxTries: maxTries}, nil)
}

// replaySundaySamples replays the Sunday read's samples under dir, a stale
// turn failing the test.
func replaySundaySamples(t *testing.T, dir string) []error {
	t.Helper()
	return sundaySampled(t, dir, placedRecord(t), nil, func(err error) { t.Error(err) })
}

// The Sunday read's samples are recorded a conversation at a time, each kept
// whole under its sample and try, and replayed with no call: three samples
// passing their checks on their first try are every property supported.
func TestTheSundayReadIsRecordedAndReplayedAsSamples(t *testing.T) {
	dir := t.TempDir()
	live := &tries{scripts: [][]editor.Turn{passingTry}}
	if errs := recordSundaySamples(t, dir, live, 0); len(errs) != 0 {
		t.Fatalf("recorded: %v", errs)
	}
	if live.asked != sundayReadRule.samples {
		t.Errorf("%d conversations recorded, want %d", live.asked, sundayReadRule.samples)
	}
	for _, k := range []string{"1", "2", "3"} {
		b, err := os.ReadFile(filepath.Join(dir, k, "try-1", "conversation.json"))
		if err != nil {
			t.Fatal(err)
		}
		var rec conversationRecording
		if err := json.Unmarshal(b, &rec); err != nil || len(rec.Turns) != 2 || rec.Outcome != editor.Applied {
			t.Errorf("sample %s kept %+v, %v", k, rec, err)
		}
		for i, turn := range rec.Turns {
			if turn.RequestHash == "" || turn.Raw == nil {
				t.Errorf("sample %s turn %d keeps no hash or no provider form: %+v", k, i+1, turn)
			}
		}
	}
	if errs := replaySundaySamples(t, dir); len(errs) != 0 {
		t.Errorf("replayed: %v", errs)
	}
}

// A try the checks refuse is tried again, as in use, and every try is kept:
// the replay passes on the same try the recording did.
func TestASundayReadSampleIsTriedAgainAfterAFailedTry(t *testing.T) {
	dir := t.TempDir()
	live := &tries{scripts: [][]editor.Turn{failingTry, passingTry}}
	if errs := recordSundaySamples(t, dir, live, 0); len(errs) != 0 {
		t.Fatalf("recorded: %v", errs)
	}
	if live.asked != sundayReadRule.samples+1 {
		t.Errorf("%d conversations recorded, want one failed and three passing", live.asked)
	}
	b, err := os.ReadFile(filepath.Join(dir, "1", "try-1", "conversation.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rec conversationRecording
	if err := json.Unmarshal(b, &rec); err != nil || rec.Outcome != editor.Failed {
		t.Errorf("the failed try kept %+v, %v", rec, err)
	}
	if errs := replaySundaySamples(t, dir); len(errs) != 0 {
		t.Errorf("replayed: %v", errs)
	}
}

// Recording gives a sample no more tries than it is allowed, the act's own or
// fewer: samples failing every try are refuted.
func TestASundayReadSampleFailingEveryTryIsRefuted(t *testing.T) {
	dir := t.TempDir()
	live := &tries{scripts: [][]editor.Turn{failingTry}}
	errs := recordSundaySamples(t, dir, live, 2)
	if live.asked != 2*sundayReadRule.samples {
		t.Errorf("%d conversations recorded, want two per sample", live.asked)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), checksProperty) || !strings.Contains(errs[0].Error(), "refuted") ||
		!strings.Contains(errs[0].Error(), "0 of 3") {
		t.Errorf("judged %v; want passing its checks refuted at 0 of 3", errs)
	}
}

// A Sunday read with fewer samples kept than its rule fails, naming the
// command that records them.
func TestTheSundayReadWithTooFewSamplesFails(t *testing.T) {
	dir := t.TempDir()
	if errs := recordSundaySamples(t, dir, &tries{scripts: [][]editor.Turn{passingTry}}, 0); len(errs) != 0 {
		t.Fatal(errs)
	}
	if err := os.RemoveAll(filepath.Join(dir, "3")); err != nil {
		t.Fatal(err)
	}
	errs := replaySundaySamples(t, dir)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "2 of 3 samples") || !strings.Contains(errs[0].Error(), "TestRecordedSundayRead") {
		t.Errorf("judged %v; want too few samples", errs)
	}
}

// keptTry is the conversation kept for try 1 of sample k under dir.
func keptTry(t *testing.T, dir, k string) (string, conversationRecording) {
	t.Helper()
	path := filepath.Join(dir, k, "try-1", "conversation.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rec conversationRecording
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	return path, rec
}

// keepTry writes rec as the conversation at path.
func keepTry(t *testing.T, path string, rec conversationRecording) {
	t.Helper()
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// Replaying a try whose turn the act now asks something else fails as stale;
// recording again keeps the current tries, and draws afresh the stale one and
// one left part way.
func TestAStaleOrUnfinishedSundayReadTryIsRedrawn(t *testing.T) {
	dir := t.TempDir()
	if errs := recordSundaySamples(t, dir, &tries{scripts: [][]editor.Turn{passingTry}}, 0); len(errs) != 0 {
		t.Fatal(errs)
	}
	path, rec := keptTry(t, dir, "1")
	rec.Turns[1].RequestHash = "not the request"
	keepTry(t, path, rec)
	path, rec = keptTry(t, dir, "2")
	rec.Outcome = ""
	keepTry(t, path, rec)

	var reported []error
	errs := sundaySampled(t, dir, placedRecord(t), nil, func(err error) { reported = append(reported, err) })
	if len(reported) == 0 || !strings.Contains(reported[0].Error(), "stale: re-record with") {
		t.Errorf("reported %v; want the first sample stale", reported)
	}
	if len(errs) == 0 {
		t.Error("a stale sample was judged supported")
	}

	live := &tries{scripts: [][]editor.Turn{passingTry}}
	if errs := recordSundaySamples(t, dir, live, 0); len(errs) != 0 {
		t.Fatal(errs)
	}
	if live.asked != 2 {
		t.Errorf("%d conversations recorded, want the stale one and the unfinished one", live.asked)
	}
}
