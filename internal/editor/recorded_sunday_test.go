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

// maxSamplesFlag bounds the samples recording draws of the Sunday read, to
// probe a changed act on one before paying for the rule's: a recording with
// fewer samples than the rule fails, as any does.
var maxSamplesFlag = flag.Int("max-samples", 0, "recording the Sunday read, the most samples drawn (0: the rule's)")

// The recorded runs of the Sunday read. Its own frozen copies of the
// readings, relatings and the Briefing (testdata/recorded/sunday/inputs)
// are replayed into a Record: the fixtures read and related, and Saturday's
// Briefing written over them; and the agent writes the Sunday read over it,
// the day after, from what that Briefing told, querying the Record and
// Wikidata. It is sampled three times (the sundayReadRule), each sample run
// as in use: up to SundayReadTries tries, each a whole conversation over a
// fresh copy of that Record. Each try is kept, written turn by turn as it
// runs, at testdata/recorded/sunday/<k>/try-<n>/conversation.json, with
// Wikidata's answers to its lookups beside it (wikidata/), and replayed with
// no network call; every turn keeps the hash of its request. Re-recording
// runs the agent on the real model, which costs about a dollar on Claude
// Opus 5.5:
//
//	go test -count=1 -timeout 90m ./internal/editor -run '^TestRecordedSundayRead$' -record
//
// with GOSLOW_ANTHROPIC_API_KEY and SUNDAY_READ_MODEL set in the
// environment; -max-tries bounds the tries each sample is given. It keeps
// the tries already kept that replay as the act runs now, so it resumes a
// stopped recording; fresh samples are drawn by deleting them first. Before
// re-recording it as a refresh, copy each fixture's first sample of reading
// and relating, and the Briefing's first sample, over its inputs, all
// together. Re-record it after the act's instructions, tools or checks
// change, or to bring it onto the earlier acts' recordings, and only on the
// Owner's say.
func TestRecordedSundayRead(t *testing.T) { recordedSundayRead(t, recordedDir) }

// recordedSundayRead replays (or records) the Sunday read from the
// recordings under root.
func recordedSundayRead(t *testing.T, root string) {
	dir := filepath.Join(root, "sunday")
	frozen := toldRecordFile(t, filepath.Join(dir, "inputs"))
	casesPresentForSundayRead(t, copyOfRecord(t, frozen))
	fresh := func() *sqlite.Store { return copyOfRecord(t, frozen) }
	var live *sundayLive
	if *rerecordFlag {
		key, name := os.Getenv("GOSLOW_ANTHROPIC_API_KEY"), os.Getenv(sundayReadCommand.modelVar)
		if key == "" || name == "" {
			t.Fatalf("recording needs GOSLOW_ANTHROPIC_API_KEY and %s", sundayReadCommand.modelVar)
		}
		live = &sundayLive{conv: claude.New(claude.Options{APIKey: key, Model: name}), model: name, maxTries: *maxTriesFlag,
			maxSamples: *maxSamplesFlag}
	}
	for _, err := range sundaySampled(t, dir, fresh, live, func(err error) { t.Error(err) }) {
		t.Error(err)
	}
}

// toldRecordFile is the file of a closed Record holding the fixture Items,
// with the readings and relatings under dir replayed into it and Saturday's
// Briefing written over them by the one kept under dir/briefing: the
// Sunday read's frozen copies.
func toldRecordFile(t *testing.T, dir string) string {
	t.Helper()
	path := spanRecordFile(t, dir)
	store, err := sqlite.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := sampleAt(filepath.Join(dir, "briefing", "saturday.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !briefingSample(t, store, &sampleModel{attempts: s.attempts(), unchecked: true}) {
		t.Fatal("the frozen Briefing fails the act's checks")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// casesPresentForSundayRead fails unless what the week's Briefing told
// carries what the read's tools work on: a Span told with relations (a
// Story to look into) and a Span told naming an Entity (Spans to find by
// it).
func casesPresentForSundayRead(t *testing.T, store *sqlite.Store) {
	t.Helper()
	told, err := store.BriefingsWritten(t.Context(), sundayAt.Add(-7*24*time.Hour), sundayAt)
	if err != nil || len(told) != 1 || len(told[0].Statements) == 0 {
		t.Fatalf("the week told nothing: %+v %v", told, err)
	}
	related, naming := false, false
	for _, st := range told[0].Statements {
		for _, c := range st.Cites {
			rels, err := store.RelationsOf(t.Context(), c.At)
			if err != nil {
				t.Fatal(err)
			}
			related = related || len(rels) > 0
			naming = naming || len(c.Entities) > 0
		}
	}
	if !related || !naming {
		t.Fatalf("no Span told has relations (%v) or names an Entity (%v)", related, naming)
	}
}

// sundayLive is the model the Sunday read is recorded on: the conversation,
// the model it is asked for, Wikidata as fetched (the network, if nil), and
// the most tries a sample is given (0: the act's own) and the most samples
// drawn (0: the rule's).
type sundayLive struct {
	conv       editor.Conversation
	model      string
	wikidata   http.RoundTripper
	maxTries   int
	maxSamples int
}

// sundayAt is when the recorded Sunday read is written: the morning after
// the recorded Briefing.
var sundayAt = briefingAt.AddDate(0, 0, 1)

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
	tries, samples, model := editor.SundayReadTries, sundayReadRule.samples, ""
	if live != nil {
		model = live.model
		if live.maxTries > 0 {
			tries = min(tries, live.maxTries)
		}
		if live.maxSamples > 0 {
			samples = min(samples, live.maxSamples)
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
	for k := 1; k <= samples; k++ {
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
// A replay has no one to be polite to, so it never waits: a kept answer that
// asked the client to back off (a maxlag error, whose Retry-After header is
// not kept) would otherwise wait the default backoff in real time.
func replayedWikidata(t *testing.T, dir string) editor.Wikidata {
	t.Helper()
	srv := httptest.NewServer(wikidatatest.Handler(filepath.Join(dir, "wikidata")))
	t.Cleanup(srv.Close)
	return wikidata.New(wikidata.Options{
		HTTP: &http.Client{Timeout: 30 * time.Second}, Endpoint: srv.URL, UserAgent: polite.UserAgent(testContact),
		Now: time.Now, Sleep: func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
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
	// Every statement cites things that exist: Spans within their Items'
	// text that reading did not reject, Assessments resting on Spans, and
	// the Wikidata revisions its lookups saw.
	cites := p.property("every statement cites what exists, traced to its source")
	spanExists := func(o editor.OtherSpan) string {
		if o.Item.Title == "" || o.At.Span.End > len(o.Item.Content) {
			return "a Span outside its Item's text"
		}
		rd, read, err := store.NewestReading(ctx, o.At.SourceID, o.At.EntryID)
		if err != nil {
			t.Fatal(err)
		}
		if read && slices.Contains(rd.Rejected, o.At) {
			return "a Span reading rejected"
		}
		return ""
	}
	for _, st := range r.Statements {
		if len(st.Cites) == 0 {
			cites("an uncited statement: %q", st.Text)
		}
		for _, c := range st.Cites {
			switch c.Kind {
			case editor.CitesSpan:
				if why := spanExists(c.Span); why != "" {
					cites("%q cites %s: %+v", st.Text, why, c.Span.At)
				}
			case editor.CitesAssessment:
				if c.Assessment.ID == 0 || len(c.Assessment.Spans) == 0 {
					cites("%q cites an Assessment resting on no Span: %+v", st.Text, c.Assessment)
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
	assessments := p.property("every Assessment rests on Spans that exist, as at the read's day")
	for _, a := range r.Assessments {
		if len(a.Spans) == 0 || a.AsAt.Format(time.DateOnly) != r.Day {
			assessments("an Assessment not resting on Spans as at the read's day: %+v", a)
		}
		for _, o := range a.Spans {
			if why := spanExists(o); why != "" {
				assessments("%q rests on %s: %+v", a.Statement, why, o.At)
			}
		}
		t.Logf("Assessment: %s (%d Spans)", a.Statement, len(a.Spans))
	}
	for _, g := range r.Gathered {
		t.Logf("Story gathered from %+v: %d Spans", g.From, len(g.Story))
	}
	return sampleResult{passed: true, held: p}
}

// plus is the usage of a and b together.
func plus(a, b editor.Usage) editor.Usage {
	return editor.Usage{InputTokens: a.InputTokens + b.InputTokens, OutputTokens: a.OutputTokens + b.OutputTokens,
		CacheReadTokens: a.CacheReadTokens + b.CacheReadTokens}
}
