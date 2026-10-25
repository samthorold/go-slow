package editor_test

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"html"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/samthorold/go-slow/internal/claude"
	"github.com/samthorold/go-slow/internal/editor"
)

// The recorded runs (system design: The Editor is tested in three tiers).
// Each fixture Item is read once by the real model, and its response is kept
// under testdata/recorded; the suite replays the responses, so it never makes
// a model call. Re-recording is deliberate, and costs a call per fixture:
//
//	go test ./internal/editor -run TestRecordedReadings -record
//
// with GOSLOW_ANTHROPIC_API_KEY and READING_MODEL set in the environment.
var rerecordFlag = flag.Bool("record", false, "call the real model on the fixtures and keep its responses")

// recording is one kept response.
type recording struct {
	Act        string          `json:"act"`
	Model      string          `json:"model"`
	RecordedAt time.Time       `json:"recorded_at"`
	Usage      editor.Usage    `json:"usage"`
	Output     json.RawMessage `json:"output"`
}

// replay answers with a kept response, as the model did when it was kept.
type replay struct{ rec recording }

func (r replay) Complete(context.Context, editor.Request) (editor.Response, error) {
	return editor.Response{Output: r.rec.Output, Model: r.rec.Model, Usage: r.rec.Usage}, nil
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
	var total editor.Usage
	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".json")
		kept := filepath.Join("testdata", "recorded", "reading", name+".json")
		t.Run(name, func(t *testing.T) {
			it, body := fixture(t, path)
			if *rerecordFlag {
				rerecord(t, it, kept)
			}
			b, err := os.ReadFile(kept)
			if errors.Is(err, fs.ErrNotExist) {
				// Recording needs an API key, and none was available when the
				// harness landed; the fixture is replayed once it is recorded.
				t.Skipf("%s has not been recorded yet; record it with: "+
					"go test -count=1 ./internal/editor -run TestRecordedReadings -record "+
					"(GOSLOW_ANTHROPIC_API_KEY and READING_MODEL set)", name)
			}
			if err != nil {
				t.Fatal(err)
			}
			var rec recording
			if err := json.Unmarshal(b, &rec); err != nil {
				t.Fatal(err)
			}
			total.InputTokens += rec.Usage.InputTokens
			total.OutputTokens += rec.Usage.OutputTokens

			r, err := editor.Read(t.Context(), replay{rec}, it)
			if err != nil {
				t.Fatalf("the recorded reading fails the act's checks: %v", err)
			}

			text := normal(stripTags(it.Title + " " + it.Summary + " " + body))
			for _, f := range r.Facts {
				// Every Fact traces to its Item.
				if f.SourceID != it.SourceID || f.EntryID != it.EntryID {
					t.Errorf("Fact %q traces to %s %s", f.Statement, f.SourceID, f.EntryID)
				}
				if f.AsAt.IsZero() {
					t.Errorf("Fact %q holds as at no date", f.Statement)
				}
				if !strings.Contains(text, normal(f.Quote)) {
					t.Errorf("Fact %q rests on words not in the Item: %q", f.Statement, f.Quote)
				}
			}
			// Claims keep their attribution.
			for _, c := range r.Claims {
				if strings.TrimSpace(c.MadeBy) == "" {
					t.Errorf("Claim %q has no attribution", c.Statement)
				}
			}
			if name == "pm-meeting-with-president-zelenskyy-of-ukraine-14-october-2026" {
				// The readout names the Prime Minister only by office: the
				// Entity carries the role, which Wikidata resolves by date.
				if !slices.ContainsFunc(r.Entities, func(e editor.Entity) bool {
					return e.Kind == "person" && strings.Contains(e.Role, "Prime Minister")
				}) {
					t.Errorf("no person is given the role of Prime Minister: %+v", r.Entities)
				}
			}
			if name == "pm-speech-in-scotland-16-october-2026" {
				speechFacts(t, r, normal(stripTags(body)))
			}
		})
	}
	t.Logf("recorded usage over %d fixtures: %d input, %d output tokens", len(paths), total.InputTokens, total.OutputTokens)
}

// speechFacts asserts a speech's Facts come from its primary text: the
// speech as GOV.UK published it, not its title or summary.
func speechFacts(t *testing.T, r editor.Reading, body string) {
	t.Helper()
	if len(r.Facts) == 0 {
		t.Fatal("the speech yields no Facts")
	}
	for _, f := range r.Facts {
		if !strings.Contains(body, normal(f.Quote)) {
			t.Errorf("speech Fact %q does not come from the speech's text: %q", f.Statement, f.Quote)
		}
	}
	if !strings.Contains(strings.ToLower(joinFacts(r)), "motherwell") {
		t.Errorf("no Fact places the speech in Motherwell: %s", joinFacts(r))
	}
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

// rerecord reads the Item on the real model and keeps the response, whether
// or not it passes the act's checks: the replay shows if it doesn't.
func rerecord(t *testing.T, it editor.Item, kept string) {
	t.Helper()
	key, model := os.Getenv("GOSLOW_ANTHROPIC_API_KEY"), os.Getenv("READING_MODEL")
	if key == "" || model == "" {
		t.Fatal("recording needs GOSLOW_ANTHROPIC_API_KEY and READING_MODEL")
	}
	var rec recording
	m := recorder{claude.New(claude.Options{APIKey: key, Model: model}), &rec}
	if _, err := editor.Read(t.Context(), m, it); err != nil && rec.Output == nil {
		t.Fatalf("recording: %v", err)
	}
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(kept), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kept, append(b, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

// recorder passes a call to the real model and keeps what it answered.
type recorder struct {
	model editor.Model
	rec   *recording
}

func (r recorder) Complete(ctx context.Context, req editor.Request) (editor.Response, error) {
	res, err := r.model.Complete(ctx, req)
	if err != nil {
		return res, err
	}
	*r.rec = recording{Act: req.Act, Model: res.Model, RecordedAt: time.Now().UTC(), Usage: res.Usage, Output: res.Output}
	return res, nil
}
