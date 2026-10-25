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
	}
	if !slices.EqualFunc(got, want, func(a, b register.Commit) bool {
		return a.Hash == b.Hash && a.At.Equal(b.At) && a.Editor == b.Editor
	}) {
		t.Errorf("commits\n%+v\nwant\n%+v", got, want)
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
