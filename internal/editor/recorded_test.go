package editor_test

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/samthorold/go-slow/internal/cut"
	"github.com/samthorold/go-slow/internal/editor"
)

// The recorded runs (system design: The Editor is tested in three tiers).
// Each fixture Item is read by the real model five times, each sample with
// its attempts against the act's checks, and every sample is kept under
// testdata/recorded/reading/<fixture>/; the suite replays them all and
// judges each property by its rule (recorded_samples_test.go), so it never
// makes a model call. Recording is deliberate, and costs five calls per
// fixture, and more for attempts the checks refuse:
//
//	go test -count=1 -timeout 90m ./internal/editor -run TestRecordedReadings -record
//
// with GOSLOW_ANTHROPIC_API_KEY and READING_MODEL set in the environment.
// It keeps the samples kept for the request the act sends now, so it resumes
// where a stopped recording stopped, and draws the rest; fresh samples of a
// fixture are drawn by deleting its directory first.
var rerecordFlag = flag.Bool("record", false, "call the real model on the fixtures and keep its responses")

// How each act's own recorded test is re-recorded.
var (
	readingCommand    = rerecordCmd{"TestRecordedReadings", "READING_MODEL"}
	briefingCommand   = rerecordCmd{"TestRecordedBriefing", "BRIEFING_MODEL"}
	sundayReadCommand = rerecordCmd{"TestRecordedSundayRead", "SUNDAY_READ_MODEL"}
)

// recording is one kept response, with the hash of the request it answered
// (editor.RequestHash, with the model asked for): a replay is valid only for
// that request.
type recording struct {
	Act         string          `json:"act"`
	Model       string          `json:"model"`
	RequestHash string          `json:"request_hash,omitempty"`
	RecordedAt  time.Time       `json:"recorded_at"`
	Usage       editor.Usage    `json:"usage"`
	Output      json.RawMessage `json:"output"`
}

// replay answers with a kept response, as the model did when it was kept,
// whatever it is asked: for an act's frozen copies of the recordings it
// rests on, and for fakes. An act's own recorded test replays its samples
// through sampleModel, which checks each request.
type replay struct{ rec recording }

func (r replay) Complete(context.Context, editor.Request) (editor.Response, error) {
	return editor.Response{Output: r.rec.Output, Model: r.rec.Model, Usage: r.rec.Usage}, nil
}

// rerecordCmd is how a recorded test is re-recorded: the test, and the
// variable naming the model its act runs on.
type rerecordCmd struct{ test, modelVar string }

func (c rerecordCmd) String() string {
	return fmt.Sprintf("go test -count=1 ./internal/editor -run %s -record (GOSLOW_ANTHROPIC_API_KEY and %s set)", c.test, c.modelVar)
}

// stale is nil when a recording, keeping the hash kept, answered the request
// hashing to current; otherwise the failure naming the fixture and the
// command that re-records it.
func stale(name, kept, current string, cmd rerecordCmd) error {
	switch {
	case kept == "":
		return fmt.Errorf("%s: stale: re-record with %s; the recording keeps no request hash, so it is not known to answer the request the act builds now", name, cmd)
	case kept != current:
		return fmt.Errorf("%s: stale: re-record with %s; the act builds a request the recording did not answer (request hash %s, recorded %s)", name, cmd, current, kept)
	}
	return nil
}

// pinnedModel is the model the variable modelVar pins an act to, in
// .env.example, which lists the model each act runs on: changing a pin is a
// re-record, like changing the act's instructions.
func pinnedModel(t *testing.T, modelVar string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", ".env.example"))
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.Lines(string(b)) {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), modelVar+"="); ok {
			return v
		}
	}
	t.Fatalf(".env.example pins no %s", modelVar)
	return ""
}

// contentItem is the part of a GOV.UK Content API document an Item carries.
type contentItem struct {
	BasePath         string    `json:"base_path"`
	Title            string    `json:"title"`
	Description      string    `json:"description"`
	FirstPublishedAt time.Time `json:"first_published_at"`
	Details          struct {
		Body string `json:"body"`
	} `json:"details"`
}

// fixture loads a GOV.UK document as the Item Collection would make of it.
func fixture(t *testing.T, path string) (editor.Item, string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var c contentItem
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return editor.Item{
		SourceID:  "govuk-number-10",
		EntryID:   "tag:www.gov.uk,2005:" + c.BasePath,
		Title:     c.Title,
		Link:      "https://www.gov.uk" + c.BasePath,
		Summary:   c.Description,
		Content:   c.Details.Body,
		Published: c.FirstPublishedAt,
	}, c.Details.Body
}

// fixtureToRead is a fixture Item as reading is given it in use: with the
// Cut the sentence cutter makes of its text.
func fixtureToRead(it editor.Item) editor.ToRead {
	return editor.ToRead{Item: it, Cut: cut.Cut{ID: 1, SourceID: it.SourceID, EntryID: it.EntryID,
		Judge: cut.SentenceCutter, Spans: cut.Sentences(it.Content)}}
}

func TestRecordedReadings(t *testing.T) {
	paths, err := filepath.Glob("testdata/govuk/*.json")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	for _, path := range paths {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".json"), func(t *testing.T) {
			for _, err := range readingVerdicts(t, recordedDir, path) {
				t.Error(err)
			}
		})
	}
}

// readingVerdicts replays (or records) the samples of reading the fixture
// Item at path, kept under root, and judges its properties: each failure.
func readingVerdicts(t *testing.T, root, path string) []error {
	t.Helper()
	name := strings.TrimSuffix(filepath.Base(path), ".json")
	it, _ := fixture(t, path)
	tr := fixtureToRead(it)
	spans := make([]string, len(tr.Cut.Spans))
	for i, s := range tr.Cut.Spans {
		spans[i] = cut.Words(it.Content, s)
	}
	casesPresent(t, name, spans)
	return sampled(t, filepath.Join(root, "reading"), name, dailyRule, readingCommand, func(m *sampleModel) sampleResult {
		var r editor.Reading
		passed, err := runSample(m, func(model editor.Model) (bool, error) {
			var err error
			r, err = editor.Read(t.Context(), model, m.model, tr)
			return err == nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if !passed {
			return sampleResult{}
		}
		p := properties{}
		read := readingOf{it: it, r: r, spans: tr.Cut.Spans}
		bareAttributionsRejected(p, read, spans)
		for _, q := range quotations[name] {
			quotationSpoken(p, read, q)
		}
		for _, ref := range references[name] {
			referenceFollowed(p, read, ref)
		}
		if name == "pm-meeting-with-president-zelenskyy-of-ukraine-14-october-2026" {
			// The readout names the Prime Minister only by office: the name
			// carries the office, which Wikidata resolves by date.
			p.holds("a person is named with the office of Prime Minister", slices.ContainsFunc(r.Names, func(n editor.Name) bool {
				return n.Kind == "person" && strings.Contains(n.Office, "Prime Minister")
			}), "none is: %+v", r.Names)
		}
		return sampleResult{passed: true, held: p}
	})
}

// readingOf is a sample's reading of a fixture, with the fixture's Spans.
type readingOf struct {
	it    editor.Item
	r     editor.Reading
	spans []cut.Span
}

// within is whether the address a is inside Span n (from 1).
func (o readingOf) within(a editor.Address, n int) bool {
	s := o.spans[n-1]
	return a.Span.Start >= s.Start && a.Span.End <= s.End
}

// bareAttribution is a Span that only says who speaks the quotation after
// it: "Prime Minister Andy Burnham said:", a few words ending in "said:".
var bareAttribution = regexp.MustCompile(`^(\S+\s+){1,7}said:$`)

// bareAttributionsRejected asserts each bare attribution in the fixture is
// Rejected: it says nothing of its own, and who it names is the speaker of
// the quotation after it.
func bareAttributionsRejected(p properties, o readingOf, spans []string) {
	for i, w := range spans {
		if !bareAttribution.MatchString(w) {
			continue
		}
		p.holds("a bare “X said:” Span is Rejected", slices.ContainsFunc(o.r.Rejected, func(a editor.Address) bool {
			return a.Span == o.spans[i]
		}), "S%d %q is not", i+1, w)
	}
}

// quotation is a fixture's quotation: Spans from to to, the words of whoever
// the Span before them names, by words in that Span.
type quotation struct {
	from, to int
	who      string
}

// quotations are each fixture's quotations, by fixture.
var quotations = map[string][]quotation{
	"great-british-railways-takes-shape-as-hq-and-apprenticeship-scheme-confirmed": {
		{13, 19, "Andy Burnham"}, {21, 24, "Heidi Alexander"}, {31, 33, "Pat McFadden"}, {35, 40, "Claire Ward"},
	},
	"prime-minister-makes-offer-to-holyrood-to-work-together-to-empower-scottish-regions": {
		{6, 9, "Andy Burnham"}, {11, 14, "Douglas Alexander"},
	},
	"president-zelenskyy-to-visit-downing-street": {
		{10, 13, "Andy Burnham"}, {19, 21, "John Healey"},
	},
	"uk-government-response-to-agreement-between-us-and-russia-9-october": {
		{2, 7, "spokesperson"},
	},
}

// quotationSpoken asserts every Span of a quotation is given as its speaker
// who the Item says spoke it, and no one else.
func quotationSpoken(p properties, o readingOf, q quotation) {
	spoken := p.property("a quoted Span's speaker is who the Item says spoke it")
	for n := q.from; n <= q.to; n++ {
		var by []string
		for _, s := range o.r.Speakers {
			if o.within(s.Of, n) {
				by = append(by, o.r.Words(o.it.Content, s.By))
			}
		}
		if len(by) == 0 {
			spoken("S%d, %s's, has no speaker", n, q.who)
		}
		for _, b := range by {
			if !strings.Contains(strings.ToLower(b), strings.ToLower(q.who)) {
				spoken("S%d, %s's, is given as %q's", n, q.who, b)
			}
		}
	}
}

// reference is a fixture's stretch, words opening Span n, that names in
// other words what words containing to name.
type reference struct {
	n         int
	words, to string
}

// references are each fixture's references in other words, by fixture.
var references = map[string][]reference{
	"appointment-of-lord-lieutenant-of-county-tyrone-15-october-2026": {
		{4, "He", "Baxter"}, {5, "He", "Baxter"}, {7, "He", "Baxter"}, {9, "He", "Baxter"},
	},
	"great-british-railways-takes-shape-as-hq-and-apprenticeship-scheme-confirmed": {
		{9, "the city", "Derby"},
	},
}

// referenceFollowed asserts the reference refers to its antecedent: the
// stretch reads, followed through what it refers to, as words naming it.
func referenceFollowed(p properties, o readingOf, ref reference) {
	name := "a pronoun refers to its antecedent"
	if ref.words != "He" {
		name = "a description refers to what it describes"
	}
	followed := p.property(name)
	var at *editor.Address
	for _, r := range o.r.RefersTo {
		if o.within(r.From, ref.n) && cut.Words(o.it.Content, r.From.Span) == ref.words {
			at = &r.From
			break
		}
	}
	if at == nil {
		followed("%q in S%d refers to nothing", ref.words, ref.n)
		return
	}
	if w := o.r.Words(o.it.Content, *at); !strings.Contains(w, ref.to) {
		followed("%q in S%d refers to %q, not %s", ref.words, ref.n, w, ref.to)
	}
}

// casesPresent fails unless the fixture's Spans carry each case its
// properties rest on, so a changed fixture or cutter can't leave them
// testing nothing.
func casesPresent(t *testing.T, name string, spans []string) {
	t.Helper()
	for _, q := range quotations[name] {
		if q.to > len(spans) || !strings.HasSuffix(spans[q.from-2], "said:") ||
			!strings.Contains(strings.ToLower(spans[q.from-2]), strings.ToLower(q.who)) {
			t.Fatalf("%s no longer carries %s's quotation after S%d", name, q.who, q.from-1)
		}
	}
	for _, ref := range references[name] {
		if ref.n > len(spans) || !strings.Contains(spans[ref.n-1], ref.words) {
			t.Fatalf("%s's S%d no longer has %q", name, ref.n, ref.words)
		}
	}
}

func TestABareAttributionIsAFewWordsEndingInSaid(t *testing.T) {
	for s, want := range map[string]bool{
		"Prime Minister Andy Burnham said:":                                         true,
		"A UK government spokesperson said:":                                        true,
		"Mayor of the East Midlands, Claire Ward, said:":                            true,
		"Ahead of the two leaders meeting today, Prime Minister Andy Burnham said:": false,
		"Please don’t tell anyone in Manchester that I said that.":                  false,
		"said:": false,
	} {
		if got := bareAttribution.MatchString(s); got != want {
			t.Errorf("%q is a bare attribution: %v, want %v", s, got, want)
		}
	}
}

var tag = regexp.MustCompile(`<[^>]*>`)

// stripTags is HTML as plain text: written here, apart from the act's own,
// so the assertions don't take the act's word for what the Item says.
func stripTags(s string) string { return html.UnescapeString(tag.ReplaceAllString(s, " ")) }

// normal is s as lowercase words separated by single spaces.
func normal(s string) string {
	f := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	return " " + strings.Join(f, " ") + " "
}

// recorder passes a call to the real model and keeps what it answered, with
// the hash of the request, asked of the model named asked.
type recorder struct {
	model editor.Model
	rec   *recording
	asked string
}

func (r recorder) Complete(ctx context.Context, req editor.Request) (editor.Response, error) {
	res, err := r.model.Complete(ctx, req)
	if err != nil {
		return res, err
	}
	*r.rec = recording{Act: req.Act, Model: res.Model, RequestHash: editor.RequestHash(req, r.asked),
		RecordedAt: time.Now().UTC(), Usage: res.Usage, Output: res.Output}
	return res, nil
}
