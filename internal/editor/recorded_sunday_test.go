package editor_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/claude"
	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/polite"
	"github.com/samthorold/go-slow/internal/sqlite"
	"github.com/samthorold/go-slow/internal/wikidata"
	"github.com/samthorold/go-slow/internal/wikidata/wikidatatest"
)

// conversationRecording is a recorded run of an act with tools, one try at
// it: every turn the model took, in order, and how the try ended.
type conversationRecording struct {
	Act        string       `json:"act"`
	Model      string       `json:"model"`
	RecordedAt time.Time    `json:"recorded_at"`
	Usage      editor.Usage `json:"usage"`
	Turns      []keptTurn   `json:"turns"`
	// Outcome is applied when the try's read was written, failed when it
	// was not, and empty while the try is being recorded: a try left with no
	// outcome was stopped part way, and is drawn afresh.
	Outcome editor.Outcome `json:"outcome,omitempty"`
}

// keptTurn is one turn as kept: what the model said and called, the turn in
// the provider's own form, and the hash of the request it answered. The
// provider's form is kept because the act passes each turn back as it came,
// so it is part of every later turn's request, and of its hash. A turn the
// model answered with an error (a refusal, a turn cut off) keeps it.
type keptTurn struct {
	Text        string            `json:"text,omitempty"`
	Calls       []editor.ToolCall `json:"calls,omitempty"`
	Model       string            `json:"model"`
	StopReason  string            `json:"stop_reason,omitempty"`
	Raw         json.RawMessage   `json:"raw,omitempty"`
	Err         string            `json:"error,omitempty"`
	RequestHash string            `json:"request_hash"`
	Usage       editor.Usage      `json:"usage"`
}

// turn is the kept turn as the model gave it.
func (k keptTurn) turn() (editor.Turn, error) {
	t := editor.Turn{Text: k.Text, Calls: k.Calls, Model: k.Model, StopReason: k.StopReason, Raw: k.Raw, Usage: k.Usage}
	if k.Err != "" {
		return t, errors.New(k.Err)
	}
	return t, nil
}

// conversationRecorder passes each turn to the real model and keeps it, with
// the hash of its request, asked of the model named asked, writing the
// recording to path (if any) as each turn arrives.
type conversationRecorder struct {
	model editor.Conversation
	rec   *conversationRecording
	asked string
	path  string
}

func (r conversationRecorder) Turn(ctx context.Context, req editor.ConversationRequest) (editor.Turn, error) {
	hash := editor.ConversationRequestHash(req, r.asked)
	t, err := r.model.Turn(ctx, req)
	r.rec.Act, r.rec.RecordedAt = req.Act, time.Now().UTC()
	if t.Model != "" {
		r.rec.Model = t.Model
	}
	r.rec.Usage = plus(r.rec.Usage, t.Usage)
	k := keptTurn{Text: t.Text, Calls: t.Calls, Model: t.Model, StopReason: t.StopReason, Raw: t.Raw, RequestHash: hash, Usage: t.Usage}
	if err != nil {
		k.Err = err.Error()
	}
	r.rec.Turns = append(r.rec.Turns, k)
	if r.path != "" {
		if werr := writeConversation(r.path, *r.rec); werr != nil {
			return t, errors.Join(err, werr)
		}
	}
	return t, err
}

// writeConversation writes rec to path.
func writeConversation(path string, rec conversationRecording) error {
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

// conversationReplay answers each turn with the next kept one, in the
// provider's own form as it came, so long as the turn's request is the one
// it answered, hashed with the model the act is pinned to; otherwise it
// reports the recording stale and answers with the error. A turn kept with
// no hash is stale.
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
	name := fmt.Sprintf("turn %d", *r.next)
	if err := stale(name, k.RequestHash, editor.ConversationRequestHash(req, r.model), r.cmd); err != nil {
		r.report(err)
		return editor.Turn{}, err
	}
	return k.turn()
}

// recordingTransport passes Wikidata's answers through base (the network,
// if nil) and keeps each under the name wikidatatest replays it from.
type recordingTransport struct {
	t    *testing.T
	dir  string
	base http.RoundTripper
}

func (rt recordingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	base := rt.base
	if base == nil {
		base = http.DefaultTransport
	}
	res, err := base.RoundTrip(r)
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
	// Kept ending in a newline, as the repository's hooks keep every file;
	// the answer read back parses the same.
	kept := b
	if !bytes.HasSuffix(kept, []byte("\n")) {
		kept = append(slices.Clone(b), '\n')
	}
	if err := os.WriteFile(filepath.Join(rt.dir, wikidatatest.Name(r)), kept, 0o600); err != nil {
		rt.t.Error(err)
	}
	res.Body = io.NopCloser(bytes.NewReader(b))
	return res, nil
}

// testContact is the placeholder contact a recording names in its
// User-Agent: never the Owner's real details.
const testContact = "go-slow-tests@example.org"

// maxTriesFlag bounds the tries recording gives each sample of the Sunday
// read, below the act's own SundayReadTries, to keep a paid recording within
// the retries its brief allows.
var maxTriesFlag = flag.Int("max-tries", 0, "recording the Sunday read, the most tries each sample is given (0: the act's own)")

// The recorded runs of the Sunday read. The fixtures' recorded readings,
// placings and connectings are replayed into a Record with the Briefing's
// Topics, and the agent writes the Sunday read over it, the day after,
// querying the Record and Wikidata. It is sampled three times (the
// sundayReadRule), each sample run as in use: up to SundayReadTries tries,
// each a whole conversation over a fresh copy of that Record. Each try is
// kept, written turn by turn as it runs, at
// testdata/recorded/sunday/<k>/try-<n>/conversation.json, with Wikidata's
// answers to its lookups beside it (wikidata/), and replayed with no network
// call; every turn keeps the hash of its request. The readings, placings and
// connectings it rests on are its own copies, under
// testdata/recorded/sunday/inputs, so re-recording an earlier act leaves it
// valid. Re-recording runs the agent on the real model, which costs a few
// dollars on Claude Opus 5.5:
//
//	go test -count=1 -timeout 90m ./internal/editor -run TestRecordedSundayRead -record
//
// with GOSLOW_ANTHROPIC_API_KEY and SUNDAY_READ_MODEL set in the
// environment; -max-tries bounds the tries each sample is given. It keeps
// the tries already kept that replay as the act runs now, so it resumes a
// stopped recording; fresh samples are drawn by deleting them first. Before
// re-recording it as a refresh, copy each fixture's first sample of reading,
// placing and connecting over its inputs, all three together. Re-record it
// after the act's instructions, tools or checks change, or to bring it onto
// the earlier acts' recordings, and only on the Owner's say.
func TestRecordedSundayRead(t *testing.T) { recordedSundayRead(t, recordedDir) }

// recordedSundayRead replays (or records) the Sunday read from the
// recordings under root.
func recordedSundayRead(t *testing.T, root string) {
	dir := filepath.Join(root, "sunday")
	frozen := frozenRecordFile(t, filepath.Join(dir, "inputs"))
	fresh := func() *sqlite.Store { return copyOfRecord(t, frozen) }
	var live *sundayLive
	if *rerecordFlag {
		key, name := os.Getenv("GOSLOW_ANTHROPIC_API_KEY"), os.Getenv(sundayReadCommand.modelVar)
		if key == "" || name == "" {
			t.Fatalf("recording needs GOSLOW_ANTHROPIC_API_KEY and %s", sundayReadCommand.modelVar)
		}
		live = &sundayLive{conv: claude.New(claude.Options{APIKey: key, Model: name}), model: name, maxTries: *maxTriesFlag}
	}
	for _, err := range sundaySampled(t, dir, fresh, live, func(err error) { t.Error(err) }) {
		t.Error(err)
	}
}

// sundayLive is the model the Sunday read is recorded on: the conversation,
// the model it is asked for, Wikidata as fetched (the network, if nil), and
// the most tries a sample is given (0: the act's own).
type sundayLive struct {
	conv     editor.Conversation
	model    string
	wikidata http.RoundTripper
	maxTries int
}

// sundayAt is when the recorded Sunday read is written: the morning after
// the recorded Briefing.
var sundayAt = saturday.AddDate(0, 0, 1).Add(time.Hour)

// tryPath is where try n of sample k is kept under dir.
func tryPath(dir string, k, n int) string {
	return filepath.Join(dir, strconv.Itoa(k), fmt.Sprintf("try-%d", n), "conversation.json")
}

// sundaySampled replays (or, with live, records) every sample of the Sunday
// read under dir, each try over a fresh Record, and judges its properties by
// the Sunday read's rule. Replaying, a stale turn is reported; recording, a
// try kept that is stale or unfinished is drawn afresh.
func sundaySampled(t *testing.T, dir string, fresh func() *sqlite.Store, live *sundayLive, report func(error)) []error {
	t.Helper()
	tries, model := editor.SundayReadTries, ""
	if live != nil {
		model = live.model
		if live.maxTries > 0 {
			tries = min(tries, live.maxTries)
		}
	} else {
		model = pinnedModel(t, sundayReadCommand.modelVar)
		kept := 0
		for _, err := os.Stat(tryPath(dir, kept+1, 1)); err == nil; _, err = os.Stat(tryPath(dir, kept+1, 1)) {
			kept++
		}
		if kept != sundayReadRule.samples {
			return []error{fmt.Errorf("the Sunday read: %d of %d samples recorded; its properties are judged on %d: record them with %s",
				kept, sundayReadRule.samples, sundayReadRule.samples, sundayReadCommand)}
		}
	}
	results := make([]sampleResult, 0, sundayReadRule.samples)
	var total editor.Usage
	conversations := 0
	for k := 1; k <= sundayReadRule.samples; k++ {
		var res sampleResult
		for n := 1; n <= tries; n++ {
			path := tryPath(dir, k, n)
			rec, kept := keptConversation(path)
			store := fresh()
			var passed bool
			if kept {
				var staleness error
				replayed := conversationReplay{rec: rec, next: new(int), model: model, cmd: sundayReadCommand, report: func(err error) {
					staleness = err
					if live == nil {
						report(err)
					}
				}}
				passed = sundayTry(t, store, replayed, replayedWikidata(t, filepath.Dir(path)))
				if staleness != nil && live != nil {
					kept, store = false, fresh()
				}
			}
			if !kept {
				if live == nil {
					break
				}
				if err := os.RemoveAll(filepath.Dir(path)); err != nil {
					t.Fatal(err)
				}
				rec = conversationRecording{}
				passed = sundayTry(t, store, conversationRecorder{live.conv, &rec, model, path},
					liveWikidata(t, filepath.Dir(path), live.wikidata))
				rec.Outcome = editor.Failed
				if passed {
					rec.Outcome = editor.Applied
				}
				if err := writeConversation(path, rec); err != nil {
					t.Fatal(err)
				}
				t.Logf("sample %d, try %d: %s, %d turns; %d input, %d cache-read input, %d output tokens",
					k, n, rec.Outcome, len(rec.Turns), rec.Usage.InputTokens, rec.Usage.CacheReadTokens, rec.Usage.OutputTokens)
			}
			total = plus(total, rec.Usage)
			conversations++
			if passed {
				res = sundayProperties(t, store)
				if live != nil {
					// Tries after the one that passed are not part of the sample.
					for m := n + 1; m <= editor.SundayReadTries; m++ {
						if err := os.RemoveAll(filepath.Dir(tryPath(dir, k, m))); err != nil {
							t.Fatal(err)
						}
					}
				}
				break
			}
		}
		results = append(results, res)
	}
	t.Logf("the Sunday read: %d samples, %d conversations; %d input, %d cache-read input, %d output tokens",
		sundayReadRule.samples, conversations, total.InputTokens, total.CacheReadTokens, total.OutputTokens)
	return judge("the Sunday read", sundayReadRule, sundayReadCommand, results)
}

// keptConversation is the try kept at path, and whether it was kept whole.
func keptConversation(path string) (conversationRecording, bool) {
	b, err := os.ReadFile(path) //nolint:gosec // a test's own recordings
	if err != nil {
		return conversationRecording{}, false
	}
	var rec conversationRecording
	if err := json.Unmarshal(b, &rec); err != nil || rec.Outcome == "" {
		return conversationRecording{}, false
	}
	return rec, true
}

// replayedWikidata is Wikidata answering from the answers kept under dir.
func replayedWikidata(t *testing.T, dir string) editor.Wikidata {
	t.Helper()
	srv := httptest.NewServer(wikidatatest.Handler(filepath.Join(dir, "wikidata")))
	t.Cleanup(srv.Close)
	return wikidata.New(wikidata.Options{
		HTTP: &http.Client{Timeout: 30 * time.Second}, Endpoint: srv.URL, UserAgent: polite.UserAgent(testContact),
		Now: time.Now, Sleep: polite.Sleep,
	})
}

// liveWikidata is Wikidata reached through base (the network, if nil), each
// answer kept under dir.
func liveWikidata(t *testing.T, dir string, base http.RoundTripper) editor.Wikidata {
	t.Helper()
	return wikidata.New(wikidata.Options{
		HTTP:     &http.Client{Timeout: 30 * time.Second, Transport: recordingTransport{t, filepath.Join(dir, "wikidata"), base}},
		Endpoint: wikidata.Endpoint, UserAgent: polite.UserAgent(testContact), Gap: wikidata.Gap,
		Now: time.Now, Sleep: polite.Sleep,
	})
}

// sundayTry runs one try at the Sunday read on conv into store, and is
// whether it passed the act's checks: whether the read was written.
func sundayTry(t *testing.T, store *sqlite.Store, conv editor.Conversation, wd editor.Wikidata) bool {
	t.Helper()
	var logs bytes.Buffer
	ed := editor.New(editor.Deps{Store: store, SundayReads: store, SundayReadModel: conv, Wikidata: wd,
		Log: slog.New(slog.NewTextHandler(&logs, nil))})
	if err := ed.SundayRead(t.Context(), sundayAt); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), "writing the Sunday read failed") {
		t.Logf("a try failed the act's checks:\n%s", logs.String())
		return false
	}
	return true
}

// sundayProperties are the test-only properties of the Sunday read written
// into store.
func sundayProperties(t *testing.T, store *sqlite.Store) sampleResult {
	t.Helper()
	ctx := t.Context()
	r, found, err := store.SundayRead(ctx, sundayAt.Format(time.DateOnly))
	if err != nil || !found {
		t.Fatalf("no Sunday read written: %v %v", found, err)
	}
	p := properties{}
	p.holds("the read says something", len(r.Statements) > 0, "the Sunday read says nothing")
	// Every statement cites things that exist: Facts and Developments
	// traced to their Items, Assessments the read made with the Facts they
	// rest on, and the Wikidata revisions its lookups saw.
	cites := p.property("every statement cites what exists, traced to its source")
	for _, st := range r.Statements {
		if len(st.Cites) == 0 {
			cites("an uncited statement: %q", st.Text)
		}
		for _, c := range st.Cites {
			switch c.Kind {
			case editor.CitesFact, editor.CitesDevelopment:
				if c.Fact.ID == 0 || c.Fact.EntryID == "" || c.Fact.ItemTitle == "" {
					cites("%q cites a Fact that does not trace to an Item: %+v", st.Text, c.Fact)
				}
				if c.Kind == editor.CitesDevelopment {
					s, found, err := store.Story(ctx, c.Story.ID)
					if err != nil || !found || !containsFact(s.Timeline, c.Fact.ID) {
						cites("%q cites a Development not on its Story's Timeline: %+v", st.Text, c)
					}
				}
			case editor.CitesAssessment:
				if c.Assessment.ID == 0 || len(c.Assessment.Facts) == 0 {
					cites("%q cites an Assessment resting on no Fact: %+v", st.Text, c.Assessment)
				}
			case editor.CitesLookup:
				if c.Lookup.ID == 0 || c.Lookup.ItemID == "" || c.Lookup.Revision == 0 || c.Lookup.Licence != editor.WikidataLicence {
					cites("%q cites a lookup that names no revision: %+v", st.Text, c.Lookup)
				}
			default:
				cites("%q cites a %q", st.Text, c.Kind)
			}
		}
		t.Logf("%s (%d cited)", st.Text, len(st.Cites))
	}
	assessments := p.property("every Assessment rests on Facts, as at the read's day")
	for _, a := range r.Assessments {
		if len(a.Facts) == 0 || a.AsAt.Format(time.DateOnly) != r.Day {
			assessments("an Assessment not resting on Facts as at the read's day: %+v", a)
		}
		t.Logf("Assessment: %s (%d Facts)", a.Statement, len(a.Facts))
	}
	for _, s := range r.Stories {
		t.Logf("Story started: %s (%d Developments)", s.Title, len(s.Timeline))
	}
	return sampleResult{passed: true, held: p}
}

// plus is the usage of a and b together.
func plus(a, b editor.Usage) editor.Usage {
	return editor.Usage{InputTokens: a.InputTokens + b.InputTokens, OutputTokens: a.OutputTokens + b.OutputTokens,
		CacheReadTokens: a.CacheReadTokens + b.CacheReadTokens}
}

func containsFact(timeline []editor.Development, id int64) bool {
	for _, d := range timeline {
		if d.ID == id {
			return true
		}
	}
	return false
}
