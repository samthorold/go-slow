package editor_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/claude"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/polite"
	"github.com/samthorold/go-slow/internal/wikidata"
	"github.com/samthorold/go-slow/internal/wikidata/wikidatatest"
)

// conversationRecording is a recorded run of an act with tools: every turn
// the model took, in order.
type conversationRecording struct {
	Act        string       `json:"act"`
	Model      string       `json:"model"`
	RecordedAt time.Time    `json:"recorded_at"`
	Usage      editor.Usage `json:"usage"`
	Turns      []keptTurn   `json:"turns"`
}

// keptTurn is one turn as kept: what the model said and called, and the
// hash of the request it answered. The provider's own form of it is not
// kept; replay needs only what the act reads.
type keptTurn struct {
	Text        string            `json:"text,omitempty"`
	Calls       []editor.ToolCall `json:"calls,omitempty"`
	Model       string            `json:"model"`
	RequestHash string            `json:"request_hash,omitempty"`
	Usage       editor.Usage      `json:"usage"`
}

// conversationRecorder passes each turn to the real model and keeps it, with
// the hash of its request, asked of the model named asked.
type conversationRecorder struct {
	model editor.Conversation
	rec   *conversationRecording
	asked string
}

func (r conversationRecorder) Turn(ctx context.Context, req editor.ConversationRequest) (editor.Turn, error) {
	t, err := r.model.Turn(ctx, req)
	if err != nil {
		return t, err
	}
	r.rec.Act, r.rec.Model, r.rec.RecordedAt = req.Act, t.Model, time.Now().UTC()
	r.rec.Usage = editor.Usage{
		InputTokens: r.rec.Usage.InputTokens + t.Usage.InputTokens, OutputTokens: r.rec.Usage.OutputTokens + t.Usage.OutputTokens,
		CacheReadTokens: r.rec.Usage.CacheReadTokens + t.Usage.CacheReadTokens,
	}
	r.rec.Turns = append(r.rec.Turns, keptTurn{Text: t.Text, Calls: t.Calls, Model: t.Model,
		RequestHash: editor.ConversationRequestHash(asReplayed(req), r.asked), Usage: t.Usage})
	return t, nil
}

// asReplayed is req as a replay of it is sent: the model's earlier turns
// without their provider's own form, which a recording does not keep. A
// recorded turn's hash is of this form, the one its replay can rebuild.
func asReplayed(req editor.ConversationRequest) editor.ConversationRequest {
	req.Messages = slices.Clone(req.Messages)
	for i, m := range req.Messages {
		if m.Assistant != nil && m.Assistant.Raw != nil {
			t := *m.Assistant
			t.Raw = nil
			req.Messages[i].Assistant = &t
		}
	}
	return req
}

// conversationReplay answers each turn with the next kept one, so long as
// the turn's request is the one it answered, hashed with the model the act
// is pinned to; otherwise it reports the recording stale and answers with
// the error. A recording with no hashes at all (unhashed) is replayed
// unchecked.
type conversationReplay struct {
	rec    conversationRecording
	next   *int
	model  string
	cmd    rerecordCmd
	report func(error)
}

func (r conversationReplay) Turn(_ context.Context, req editor.ConversationRequest) (editor.Turn, error) {
	if *r.next >= len(r.rec.Turns) {
		return editor.Turn{}, errors.New("the recording has no more turns")
	}
	k := r.rec.Turns[*r.next]
	*r.next++
	if !unhashed(r.rec) {
		name := fmt.Sprintf("turn %d", *r.next)
		if err := stale(name, k.RequestHash, editor.ConversationRequestHash(asReplayed(req), r.model), r.cmd); err != nil {
			r.report(err)
			return editor.Turn{}, err
		}
	}
	return editor.Turn{Text: k.Text, Calls: k.Calls, Model: k.Model, Usage: k.Usage}, nil
}

// unhashed is whether rec was recorded before turns kept their request's
// hash: none of its turns has one.
func unhashed(rec conversationRecording) bool {
	return !slices.ContainsFunc(rec.Turns, func(k keptTurn) bool { return k.RequestHash != "" })
}

// recordingTransport passes Wikidata's answers through and keeps each
// under the name wikidatatest replays it from.
type recordingTransport struct {
	t   *testing.T
	dir string
}

func (rt recordingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	res, err := http.DefaultTransport.RoundTrip(r)
	if err != nil || res.StatusCode != http.StatusOK {
		return res, err
	}
	b, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(rt.dir, 0o750); err != nil {
		rt.t.Error(err)
	}
	if err := os.WriteFile(filepath.Join(rt.dir, wikidatatest.Name(r)), b, 0o600); err != nil {
		rt.t.Error(err)
	}
	res.Body = io.NopCloser(bytes.NewReader(b))
	return res, nil
}

// testContact is the placeholder contact a recording names in its
// User-Agent: never the Owner's real details.
const testContact = "go-slow-tests@example.org"

// The recorded run of the Sunday read. The fixtures' recorded readings,
// placings and connectings are replayed into a Record with the Briefing's
// Topics, and the agent writes the Sunday read over it, the day after,
// querying the Record and Wikidata. The whole conversation is kept under
// testdata/recorded/sunday, with Wikidata's answers to its lookups, and
// replayed with no network call. The readings, placings and connectings it
// rests on are its own copies, under testdata/recorded/sunday/inputs, as
// they were when it was recorded: its turns cite what those showed, so
// re-recording an earlier act leaves it valid. (The readings there were
// recorded with a separate list of Entities, and are rewritten with each
// Fact's and Claim's Entities inline, as the act gives them now.) Re-recording runs the agent on the real
// model, which costs a few dollars on Claude Opus 5.5:
//
//	go test -count=1 ./internal/editor -run TestRecordedSundayRead -record
//
// with GOSLOW_ANTHROPIC_API_KEY and SUNDAY_READ_MODEL set in the
// environment. Before re-recording it, copy the current recordings of
// reading, placing and connecting (testdata/recorded/{reading,placing,
// connecting}) over its inputs, all three together. Re-record it after the
// act's instructions, tools or checks change, or to bring it onto the
// earlier acts' recordings, and only on the Owner's say.
func TestRecordedSundayRead(t *testing.T) { recordedSundayRead(t, recordedDir) }

// recordedSundayRead replays (or records) the Sunday read from the
// recordings under root.
func recordedSundayRead(t *testing.T, root string) {
	ctx := t.Context()
	dir := filepath.Join(root, "sunday")
	path := filepath.Join(dir, "conversation.json")
	wikidataDir := filepath.Join(dir, "wikidata")
	at := saturday.AddDate(0, 0, 1).Add(time.Hour)

	var model editor.Conversation
	var rec conversationRecording
	wdHTTP := &http.Client{Timeout: 30 * time.Second}
	endpoint, gap := wikidata.Endpoint, wikidata.Gap
	if *rerecordFlag {
		key, name := os.Getenv("GOSLOW_ANTHROPIC_API_KEY"), os.Getenv("SUNDAY_READ_MODEL")
		if key == "" || name == "" {
			t.Fatal("recording needs GOSLOW_ANTHROPIC_API_KEY and SUNDAY_READ_MODEL")
		}
		if err := os.RemoveAll(wikidataDir); err != nil {
			t.Fatal(err)
		}
		model = conversationRecorder{claude.New(claude.Options{APIKey: key, Model: name}), &rec, name}
		wdHTTP.Transport = recordingTransport{t, wikidataDir}
	} else {
		b, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			t.Skipf("the Sunday read has not been recorded yet; record it with: %s", sundayReadCommand)
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &rec); err != nil {
			t.Fatal(err)
		}
		if unhashed(rec) {
			// Recorded before turns kept their request's hash, and not
			// re-recorded since (it costs dollars): whether the act still
			// builds the requests it answered is not known, so this replay
			// checks the act's properties on it, not that it is current.
			t.Logf("the Sunday read's recording keeps no request hashes, so it is not known to answer "+
				"the requests the act builds now; re-record it, on the Owner's say, with: %s", sundayReadCommand)
		}
		model = conversationReplay{rec: rec, next: new(int), model: pinnedModel(t, sundayReadCommand.modelVar),
			cmd: sundayReadCommand, report: func(err error) { t.Error(err) }}
		srv := httptest.NewServer(wikidatatest.Handler(wikidataDir))
		t.Cleanup(srv.Close)
		endpoint, gap = srv.URL, 0
	}
	wd := wikidata.New(wikidata.Options{
		HTTP: wdHTTP, Endpoint: endpoint, UserAgent: polite.UserAgent(testContact), Gap: gap,
		Now: time.Now, Sleep: polite.Sleep,
	})

	store := frozenRecord(t, filepath.Join(dir, "inputs"))
	var logs bytes.Buffer
	ed := editor.New(editor.Deps{Store: store, SundayReads: store, SundayReadModel: model, Wikidata: wd,
		Log: slog.New(slog.NewTextHandler(&logs, nil))})
	err := ed.SundayRead(ctx, at)
	if *rerecordFlag && len(rec.Turns) > 0 {
		b, merr := json.MarshalIndent(rec, "", "  ")
		if merr != nil {
			t.Fatal(merr)
		}
		if merr := os.MkdirAll(dir, 0o750); merr != nil {
			t.Fatal(merr)
		}
		if werr := os.WriteFile(path, append(b, '\n'), 0o600); werr != nil {
			t.Fatal(werr)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("recorded Sunday read: %d turns; %d input, %d cache-read input, %d output tokens",
		len(rec.Turns), rec.Usage.InputTokens, rec.Usage.CacheReadTokens, rec.Usage.OutputTokens)
	if strings.Contains(logs.String(), "writing the Sunday read failed") {
		t.Fatalf("the recorded Sunday read fails the act's checks:\n%s", logs.String())
	}

	r, found, err := store.SundayRead(ctx, at.Format(time.DateOnly))
	if err != nil || !found {
		t.Fatalf("no Sunday read written: %v %v\n%s", found, err, logs.String())
	}
	if len(r.Statements) == 0 {
		t.Fatal("the Sunday read says nothing")
	}
	// Every statement cites things that exist: Facts and Developments
	// traced to their Items, Assessments the read made with the Facts they
	// rest on, and the Wikidata revisions its lookups saw.
	for _, st := range r.Statements {
		if len(st.Cites) == 0 {
			t.Errorf("an uncited statement: %q", st.Text)
		}
		for _, c := range st.Cites {
			switch c.Kind {
			case editor.CitesFact, editor.CitesDevelopment:
				if c.Fact.ID == 0 || c.Fact.EntryID == "" || c.Fact.ItemTitle == "" {
					t.Errorf("%q cites a Fact that does not trace to an Item: %+v", st.Text, c.Fact)
				}
				if c.Kind == editor.CitesDevelopment {
					s, found, err := store.Story(ctx, c.Story.ID)
					if err != nil || !found || !containsFact(s.Timeline, c.Fact.ID) {
						t.Errorf("%q cites a Development not on its Story's Timeline: %+v", st.Text, c)
					}
				}
			case editor.CitesAssessment:
				if c.Assessment.ID == 0 || len(c.Assessment.Facts) == 0 {
					t.Errorf("%q cites an Assessment resting on no Fact: %+v", st.Text, c.Assessment)
				}
			case editor.CitesLookup:
				if c.Lookup.ID == 0 || c.Lookup.ItemID == "" || c.Lookup.Revision == 0 || c.Lookup.Licence != editor.WikidataLicence {
					t.Errorf("%q cites a lookup that names no revision: %+v", st.Text, c.Lookup)
				}
			default:
				t.Errorf("%q cites a %q", st.Text, c.Kind)
			}
		}
		t.Logf("%s (%d cited)", st.Text, len(st.Cites))
	}
	for _, a := range r.Assessments {
		if len(a.Facts) == 0 || a.AsAt.Format(time.DateOnly) != r.Day {
			t.Errorf("an Assessment not resting on Facts as at the read's day: %+v", a)
		}
		t.Logf("Assessment: %s (%d Facts)", a.Statement, len(a.Facts))
	}
	for _, s := range r.Stories {
		t.Logf("Story started: %s (%d Developments)", s.Title, len(s.Timeline))
	}
}

func containsFact(timeline []editor.Development, id int64) bool {
	for _, d := range timeline {
		if d.ID == id {
			return true
		}
	}
	return false
}
