package register

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"time"
)

// GitLogFormat is the git log the commits admin command reads:
//
//	git log --format='commit %H %cI' --name-only
//
// each commit's line followed by the paths it changed.
const GitLogFormat = "commit %H %cI"

// editorPaths are the paths whose change is a change to the Editor: its
// acts, the model adapter they call, and how Feedback is applied (the
// Feedback commands, the storage that keeps Feedback as events, which says
// what each view reads under it, and the storage that selects what a judge
// is shown as precedent). internal/sqlite/feedback.go held the old design's
// Feedback effects; it stays listed so the commits that changed it still
// read as changes to the Editor when the whole log is recorded.
//
// Only what under them is built into the app counts (changesEditor): a
// change to the Editor is a change to the app that runs, and tests, recorded
// runs and the dev tools beside them are not (system design: Shared reading
// rule for the three parts that should fall).
var editorPaths = []string{"internal/editor/", "internal/claude/", "internal/feedback/", "internal/sqlite/feedback.go",
	"internal/sqlite/pieces.go", "internal/sqlite/precedents.go"}

// devTools are the dev tools under editorPaths, which the app binary
// doesn't import.
var devTools = []string{"internal/editor/yield/"}

// ParseGitLog reads commits from a git log in GitLogFormat with
// --name-only, each marked as changing the Editor if it touches any of its
// paths.
func ParseGitLog(r io.Reader) ([]Commit, error) {
	var out []Commit
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "":
		case strings.HasPrefix(line, "commit "):
			fields := strings.Fields(line)
			if len(fields) != 3 {
				return nil, fmt.Errorf("line %d: want %q, got %q", n, GitLogFormat, line)
			}
			at, err := time.Parse(time.RFC3339, fields[2])
			if err != nil {
				return nil, fmt.Errorf("line %d: commit time: %w", n, err)
			}
			out = append(out, Commit{Hash: fields[1], At: at.UTC()})
		case len(out) == 0:
			return nil, fmt.Errorf("line %d: a path before any commit: %q", n, line)
		default:
			if changesEditor(line) {
				out[len(out)-1].Editor = true
			}
		}
	}
	return out, sc.Err()
}

// changesEditor says whether a change to path is a change to the Editor: a
// path under editorPaths built into the app, so not a test, not anything
// under a testdata directory and not a dev tool.
func changesEditor(path string) bool {
	if strings.HasSuffix(path, "_test.go") || strings.Contains("/"+path, "/testdata/") {
		return false
	}
	for _, p := range devTools {
		if strings.HasPrefix(path, p) {
			return false
		}
	}
	for _, p := range editorPaths {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}
