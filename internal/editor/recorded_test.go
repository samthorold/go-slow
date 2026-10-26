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
//	go test -count=1 ./internal/editor -run TestRecordedReadings -record
//
// with GOSLOW_ANTHROPIC_API_KEY and READING_MODEL set in the environment.
// It keeps the samples kept for the request the act sends now, so it resumes
// where a stopped recording stopped, and draws the rest; fresh samples of a
// fixture are drawn by deleting its directory first.
var rerecordFlag = flag.Bool("record", false, "call the real model on the fixtures and keep its responses")

// How each act's own recorded test is re-recorded.
var (
	readingCommand          = rerecordCmd{"TestRecordedReadings", "READING_MODEL"}
	readingPrecedentCommand = rerecordCmd{"TestRecordedReadingPrecedent", "READING_MODEL"}
	placingPrecedentCommand = rerecordCmd{"TestRecordedPlacingPrecedent", "PLACING_MODEL"}
	briefingCommand         = rerecordCmd{"TestRecordedBriefing", "BRIEFING_MODEL"}
	sundayReadCommand       = rerecordCmd{"TestRecordedSundayRead", "SUNDAY_READ_MODEL"}
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
	it, body := fixture(t, path)
	text := normal(stripTags(it.Title + " " + it.Summary + " " + body))
	if name == commonsStatement {
		commonsCasesPresent(t, text)
	}
	if name == reactionFixture {
		reactionCasesPresent(t, text)
	}
	return sampled(t, filepath.Join(root, "reading"), name, dailyRule, readingCommand, func(m *sampleModel) sampleResult {
		var r editor.Reading
		passed, err := runSample(m, func(model editor.Model) (bool, error) {
			var err error
			r, err = editor.Read(t.Context(), model, it)
			return err == nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if !passed {
			return sampleResult{}
		}
		p := properties{}
		traces := p.property("every Fact traces to its Item, as at a date, on words in it")
		for _, f := range r.Facts {
			if f.SourceID != it.SourceID || f.EntryID != it.EntryID {
				traces("Fact %q traces to %s %s", f.Statement, f.SourceID, f.EntryID)
			}
			if f.AsAt.IsZero() {
				traces("Fact %q holds as at no date", f.Statement)
			}
			if !strings.Contains(text, normal(f.Quote)) {
				traces("Fact %q rests on words not in the Item: %q", f.Statement, f.Quote)
			}
		}
		attributed := p.property("Claims keep their attribution")
		for _, c := range r.Claims {
			if strings.TrimSpace(c.MadeBy) == "" {
				attributed("Claim %q has no attribution", c.Statement)
			}
		}
		if name == "pm-meeting-with-president-zelenskyy-of-ukraine-14-october-2026" {
			// The readout names the Prime Minister only by office: the
			// Entity carries the role, which Wikidata resolves by date.
			p.holds("a person is given the role of Prime Minister", slices.ContainsFunc(r.Entities, func(e editor.Entity) bool {
				return e.Kind == "person" && strings.Contains(e.Role, "Prime Minister")
			}), "none is: %+v", r.Entities)
		}
		if name == "pm-speech-in-scotland-16-october-2026" {
			speechFacts(p, r, normal(stripTags(body)))
		}
		if name == commonsStatement {
			commonsStatementFacts(p, r)
		}
		if name == reactionFixture {
			reactionFacts(p, r)
		}
		return sampleResult{passed: true, held: p}
	})
}

// commonsStatement is the Prime Minister's statement to the House of
// Commons, which carries an arrangement (the UK-EU summit), a declared
// intention (further sanctions on Iran) and an estimate (what the UK-EU
// deals will add to the economy).
const commonsStatement = "pm-statement-to-the-house-of-commons-12-october-2026"

// commonsCasesPresent fails unless the statement's text carries each case
// its properties rest on, so a changed fixture can't leave them testing
// nothing.
func commonsCasesPresent(t *testing.T, text string) {
	t.Helper()
	for _, c := range []string{
		"i can confirm to the house today that it will take place in brussels on the 20th november",
		"we will bring further sanctions to restrict the capacity of iran and the irgc to do us harm",
		"these deals will add the equivalent of over 10 billion pounds per year to the uk economy by 2040",
	} {
		if !strings.Contains(text, normal(c)) {
			t.Fatalf("the statement no longer carries the case %q", c)
		}
	}
}

var (
	// summitDate is the summit's date as a statement gives it: in words
	// ("20 November", "20th November", "November 20") or as a date
	// (2026-11-20).
	summitDate   = regexp.MustCompile(`(?i)\b(20(th)? november|november 20(th)?|2026-11-20)\b`)
	takesPlace   = regexp.MustCompile(`(?i)\bwill take place\b`)
	tenBillion   = regexp.MustCompile(`(?i)\b10\s*(billion|bn)\b`)
	sanctionWord = regexp.MustCompile(`(?i)\bsanction`)
	iranOrIRGC   = regexp.MustCompile(`(?i)\b(iran|irgc)\b`)
)

func TestTheSummitsDateIsMatchedInWordsOrAsADate(t *testing.T) {
	for s, want := range map[string]bool{
		"The UK-EU summit is scheduled for 20 November in Brussels":    true,
		"The UK-EU summit is scheduled for Brussels on 20th November":  true,
		"The UK-EU summit is scheduled for Brussels on November 20":    true,
		"The UK-EU summit is scheduled for Brussels on 2026-11-20":     true,
		"The UK-EU summit is scheduled for Brussels on 2026-11-21":     false,
		"The UK-EU summit is scheduled for Brussels on 120 November":   false,
		"The UK-EU summit is scheduled for Brussels in late November.": false,
	} {
		if got := summitDate.MatchString(s); got != want {
			t.Errorf("summitDate matches %q: %v, want %v", s, got, want)
		}
	}
}

// commonsStatementFacts asserts the statement's arrangement and declared
// intention are drawn as Facts, as what they are, and its estimate as a
// Claim (system design: An arrangement is a Fact; A declared intention is
// a Fact, as an intention).
func commonsStatementFacts(p properties, r editor.Reading) {
	summit := func(s string) bool { return strings.Contains(strings.ToLower(s), "summit") }
	p.holds("a Fact states the UK-EU summit's arrangement", slices.ContainsFunc(r.Facts, func(f editor.Fact) bool {
		s := f.Statement
		return summit(s) && strings.Contains(strings.ToLower(s), "brussels") && summitDate.MatchString(s) && !takesPlace.MatchString(s)
	}), "none does: %s", factStatements(r))
	event := p.property("no Fact says the UK-EU summit will take place")
	for _, f := range r.Facts {
		if summit(f.Statement) && takesPlace.MatchString(f.Statement) {
			event("Fact %q", f.Statement)
		}
	}
	// Which statements are drawn is not this property's: the intention,
	// when drawn, is classified as a Fact.
	intention := p.property("the further-sanctions intention on Iran, when drawn, is a Fact, never a Claim")
	for _, c := range r.Claims {
		if sanctionWord.MatchString(c.Statement) && iranOrIRGC.MatchString(c.Statement) {
			intention("Claim %q", c.Statement)
		}
	}
	estimate := p.property("the £10 billion estimate is a Claim, not a Fact")
	for _, f := range r.Facts {
		if tenBillion.MatchString(f.Statement) {
			estimate("Fact %q", f.Statement)
		}
	}
	if !slices.ContainsFunc(r.Claims, func(c editor.Claim) bool { return tenBillion.MatchString(c.Statement) }) {
		estimate("no Claim states it")
	}
}

// reactionCasesPresent fails unless the reaction's text carries the event
// it responds to (the agreement between the US and Russia) and the saying
// (a government spokesperson's), so a changed fixture can't leave its
// properties testing nothing.
func reactionCasesPresent(t *testing.T, text string) {
	t.Helper()
	for _, c := range []string{
		"response to agreement between us and russia",
		"a uk government spokesperson said",
	} {
		if !strings.Contains(text, normal(c)) {
			t.Fatalf("the reaction no longer carries the case %q", c)
		}
	}
}

var (
	agreementWord = regexp.MustCompile(`(?i)\bagree`)
	russiaWord    = regexp.MustCompile(`(?i)\brussia`)
	// usWord is the United States, never the pronoun "us".
	usWord       = regexp.MustCompile(`\b(US|USA|U\.S\.)\b|(?i:\bunited states\b|\bamerica)`)
	respondsWord = regexp.MustCompile(`(?i)\b(respon|statement)`)
	speakerWord  = regexp.MustCompile(`(?i)\b(government|spokesperson)\b`)
)

// reactionFacts asserts the reaction's saying and the event it responds
// to are drawn as Facts, beside what was said (system design: A declared
// intention is a Fact, as an intention).
func reactionFacts(p properties, r editor.Reading) {
	p.holds("a Fact states the agreement between the US and Russia", slices.ContainsFunc(r.Facts, func(f editor.Fact) bool {
		s := f.Statement
		return agreementWord.MatchString(s) && russiaWord.MatchString(s) && usWord.MatchString(s)
	}), "none does: %s", factStatements(r))
	p.holds("a Fact states the government's response", slices.ContainsFunc(r.Facts, func(f editor.Fact) bool {
		return respondsWord.MatchString(f.Statement) && speakerWord.MatchString(f.Statement)
	}), "none does: %s", factStatements(r))
}

func TestTheReactionsCasesAreMatchedOnTheEventAndTheSaying(t *testing.T) {
	for s, want := range map[string][2]bool{
		"An agreement between the US and Russia was reached on 9 October.":                    {true, false},
		"The United States and Russia agreed a deal.":                                         {true, false},
		"A UK government spokesperson responded to the agreement between the US and Russia.":  {true, true},
		"The UK Government made a statement on the US-Russia agreement.":                      {true, true},
		"The UK fully supports US-led efforts to secure a just and lasting peace in Ukraine.": {false, false},
		"The UK government intends to maintain pressure on Russia through sanctions.":         {false, false},
		"The UK government continues to monitor developments closely.":                        {false, false},
		"Russia agreed to talks that would do us harm.":                                       {false, false},
	} {
		agreement := agreementWord.MatchString(s) && russiaWord.MatchString(s) && usWord.MatchString(s)
		response := respondsWord.MatchString(s) && speakerWord.MatchString(s)
		if got := [2]bool{agreement, response}; got != want {
			t.Errorf("%q: (agreement, response) = %v, want %v", s, got, want)
		}
	}
}

// speechFacts asserts a speech's Facts come from its primary text: the
// speech as GOV.UK published it, not its title or summary.
func speechFacts(p properties, r editor.Reading, body string) {
	primary := p.property("a speech's Facts come from its primary text")
	if len(r.Facts) == 0 {
		primary("the speech yields no Facts")
	}
	for _, f := range r.Facts {
		if !strings.Contains(body, normal(f.Quote)) {
			primary("speech Fact %q does not come from the speech's text: %q", f.Statement, f.Quote)
		}
	}
	p.holds("a Fact places the speech in Motherwell", strings.Contains(strings.ToLower(joinFacts(r)), "motherwell"),
		"none does: %s", joinFacts(r))
}

// factStatements is a reading's Facts, one statement a line.
func factStatements(r editor.Reading) string {
	var b strings.Builder
	for _, f := range r.Facts {
		b.WriteString(f.Statement + "\n")
	}
	return b.String()
}

func joinFacts(r editor.Reading) string {
	var b strings.Builder
	for _, f := range r.Facts {
		b.WriteString(f.Statement + " | " + strings.Join(f.Entities, ", ") + "\n")
	}
	return b.String()
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
