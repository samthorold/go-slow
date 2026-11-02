package register_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/register"
)

func TestCommitsAreReadFromGitLogWithWhetherTheyChangeTheEditor(t *testing.T) {
	log := `commit 1111 2026-10-25T01:57:11+01:00

internal/editor/briefing.go
internal/web/briefing.go
commit 2222 2026-10-24T23:00:00Z

docs/system-design/README.md
commit 3333 2026-10-24T22:00:00Z

internal/feedback/feedback.go

commit 4444 2026-10-24T21:00:00Z
commit 5555 2026-10-24T20:00:00Z

internal/sqlite/feedback.go
commit 6666 2026-10-24T19:00:00Z

internal/sqlite/pieces.go
commit 7777 2026-10-24T18:00:00Z

internal/sqlite/precedents.go
`
	got, err := register.ParseGitLog(strings.NewReader(log))
	if err != nil {
		t.Fatal(err)
	}

	want := []register.Commit{
		{Hash: "1111", At: time.Date(2026, time.October, 25, 0, 57, 11, 0, time.UTC), Editor: true},
		{Hash: "2222", At: time.Date(2026, time.October, 24, 23, 0, 0, 0, time.UTC)},
		{Hash: "3333", At: time.Date(2026, time.October, 24, 22, 0, 0, 0, time.UTC), Editor: true},
		{Hash: "4444", At: time.Date(2026, time.October, 24, 21, 0, 0, 0, time.UTC)},
		{Hash: "5555", At: time.Date(2026, time.October, 24, 20, 0, 0, 0, time.UTC), Editor: true},
		// How views read the Feedback standing is how Feedback is applied.
		{Hash: "6666", At: time.Date(2026, time.October, 24, 19, 0, 0, 0, time.UTC), Editor: true},
		// Which Feedback a judge is shown as precedent is too.
		{Hash: "7777", At: time.Date(2026, time.October, 24, 18, 0, 0, 0, time.UTC), Editor: true},
	}
	if !slices.EqualFunc(got, want, func(a, b register.Commit) bool {
		return a.Hash == b.Hash && a.At.Equal(b.At) && a.Editor == b.Editor
	}) {
		t.Errorf("commits\n%+v\nwant\n%+v", got, want)
	}
}

// A change to the Editor is a change to the app that runs (system design:
// Shared reading rule for the three parts that should fall): tests, recorded
// runs and the dev tools beside them are not.
func TestOnlyWhatIsBuiltIntoTheAppChangesTheEditor(t *testing.T) {
	for _, c := range []struct {
		name   string
		paths  []string
		editor bool
	}{
		{"tests only", []string{"internal/editor/reading_test.go", "internal/claude/turn_test.go"}, false},
		{"recordings only", []string{
			"internal/editor/testdata/recorded/reading/derby/1.json",
			"internal/editor/testdata/govuk/derby.json",
			"internal/feedback/testdata/case.json",
		}, false},
		{"the yield dev tool only", []string{"internal/editor/yield/main.go"}, false},
		// An act's instructions changed together with its re-recording.
		{"an act and its recordings", []string{
			"internal/editor/reading.go",
			"internal/editor/reading_test.go",
			"internal/editor/testdata/recorded/reading/derby/1.json",
		}, true},
		{"a test outside the Editor's paths", []string{"internal/sqlite/pieces_test.go"}, false},
		{"testdata outside the Editor's paths", []string{"internal/wikidata/testdata/search.json"}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			log := "commit 1111 2026-10-25T00:00:00Z\n\n" + strings.Join(c.paths, "\n") + "\n"
			got, err := register.ParseGitLog(strings.NewReader(log))
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].Editor != c.editor {
				t.Errorf("%v read as %+v, want Editor %v", c.paths, got, c.editor)
			}
		})
	}
}

func TestALogNotInTheExpectedFormatIsRefused(t *testing.T) {
	for _, log := range []string{
		"internal/editor/briefing.go\n",
		"commit 1111 yesterday\n",
		"commit 1111\n",
	} {
		if _, err := register.ParseGitLog(strings.NewReader(log)); err == nil {
			t.Errorf("%q was read without error", log)
		}
	}
}
