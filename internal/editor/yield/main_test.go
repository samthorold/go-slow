package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// write puts a hand-made recording at rel under dir.
func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func yieldOf(t *testing.T, before, after string) string {
	t.Helper()
	var out strings.Builder
	if err := run([]string{before, after}, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	return out.String()
}

// Recordings of acts the tool doesn't count (relating and the Briefing are
// retired) get no table: the report names them as acts it doesn't count.
func TestRetiredActsAreReportedAsNotCounted(t *testing.T) {
	before, after := t.TempDir(), t.TempDir()
	write(t, before, "briefing/saturday.json", `{"act": "briefing", "output": {"statements": [
		{"spans": ["S1"], "stories": [], "text": "a"}
	]}}`)
	write(t, after, "relating/speech/1.json", `{"act": "relating", "output": {"relations": [
		{"span": "S1", "to": "C1", "verdict": "follows"}
	]}}`)
	write(t, after, "relating/first/1.json", `{"act": "", "model": "", "output": null}`)
	write(t, after, "weaving/saturday/1.json", `{"act": "weaving", "output": {"stories": []}}`)

	got := yieldOf(t, before, after)

	if !strings.Contains(got, "Not counted: briefing, relating\n") {
		t.Errorf("the report doesn't name the acts it doesn't count; it is:\n%s", got)
	}
	for _, unwanted := range []string{"### briefing", "### relating", "Follows", "Statements"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("the report has %q, which it should leave out; it is:\n%s", unwanted, got)
		}
	}
	if !strings.Contains(got, "### weaving") {
		t.Errorf("the report leaves out weaving; it is:\n%s", got)
	}
}

// Weaving is counted by the Stories it made, their Editor's Spans and the
// distinct Spans they cite. With no recordings before it, its fixtures show
// as added.
func TestWeavingIsCountedByItsStoriesTheirEditorsSpansAndTheSpansTheyCite(t *testing.T) {
	before, after := t.TempDir(), t.TempDir()
	write(t, after, "weaving/saturday/1.json", `{"act": "weaving", "output": {"stories": [
		{"editor_spans": [{"body": "a", "cites": [1, 2]}, {"body": "b", "cites": [2, 3]}]},
		{"editor_spans": [{"body": "c", "cites": [4]}]}
	]}}`)
	write(t, after, "weaving/saturday/2.json", `{"act": "weaving", "output": {"stories": []}}`)

	got := yieldOf(t, before, after)

	for _, want := range []string{
		"### weaving",
		"| Fixture | Stories | Editor's Spans | Spans cited |",
		"| weaving/saturday (added) | – → 0–2 | – → 0–3 | – → 0–4 |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report has no line %q; it is:\n%s", want, got)
		}
	}
}

// Naming is counted by the words of the Name it gave each Story, by the
// last attempt of each sample: none for a sample whose last attempt gave no
// Name.
func TestNamingIsCountedByTheWordsOfItsNames(t *testing.T) {
	before, after := t.TempDir(), t.TempDir()
	write(t, before, "naming/saturday-story-1/1.json", `{"act": "naming", "output": {"name": "Britain's war of services"}}`)
	write(t, after, "naming/saturday-story-1/1.json", `{"act": "naming", "output": {"name": "Britain's war"}}`)
	write(t, after, "naming/saturday-story-1/2.json", `{"act": "naming", "output": {"name": "Britain fights a war by denying services"}}`)
	write(t, after, "naming/saturday-story-2/1.json", `{"act": "naming", "output": null, "error": "refused"}`)

	got := yieldOf(t, before, after)

	for _, want := range []string{
		"### naming",
		"| Fixture | Words |",
		"| naming/saturday-story-1 | 4 → 2–7 |",
		"| naming/saturday-story-2 (added) | – → 0 |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report has no line %q; it is:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Not counted") {
		t.Errorf("the report names naming as not counted; it is:\n%s", got)
	}
}

func TestFrozenInputsAndOtherFilesAreLeftOut(t *testing.T) {
	before, after := t.TempDir(), t.TempDir()
	for _, dir := range []string{before, after} {
		write(t, dir, "weaving/x.json", `{"act": "weaving", "output": {"stories": []}}`)
		write(t, dir, "weaving/inputs/reading/x.json", `{"act": "reading", "output": {"names": [{}], "refers_to": [], "rejected": [], "speakers": []}}`)
		write(t, dir, "naming/inputs/weaving/x.json", `{"act": "weaving", "output": {"stories": [{"editor_spans": []}]}}`)
		write(t, dir, "weaving/wikidata/search.json", `[{"id": "Q1"}]`)
	}

	got := yieldOf(t, before, after)

	if !strings.Contains(got, "| weaving/x | 0 → 0 |") {
		t.Errorf("the report leaves out the weaving recording; it is:\n%s", got)
	}
	for _, unwanted := range []string{"inputs", "### reading", "wikidata", "naming", "Not counted"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("the report has %q, which it should leave out; it is:\n%s", unwanted, got)
		}
	}
}

func TestItNeedsTwoDirectoriesThatExist(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{{dir}, {dir, filepath.Join(dir, "missing")}} {
		if err := run(args, &strings.Builder{}); err == nil {
			t.Errorf("run(%q) succeeded; want an error", args)
		}
	}
}

// Reading is counted by what it judged of the fixture's Spans, the Spans
// being those the sentence cutter makes of the fixture Item beside the
// recordings.
func TestReadingYieldShowsTheSpansRejectedNamesSpeakersAndReferences(t *testing.T) {
	root := t.TempDir()
	before, after := filepath.Join(root, "old", "recorded"), filepath.Join(root, "repo", "recorded")
	write(t, root, "repo/govuk/statement.json", `{"details": {"body": "<p>Ann said:</p><p>Kyiv is safe. She went there.</p>"}}`)
	write(t, before, "reading/statement.json", `{"act": "reading", "output": {"names": [], "refers_to": [], "rejected": [], "speakers": []}}`)
	write(t, after, "reading/statement/1.json", `{"act": "reading", "output": {
		"names": [{}, {}], "refers_to": [{}], "rejected": [1], "speakers": [{}, {}]}}`)
	write(t, after, "reading/statement/2.json", `{"act": "reading", "output": {
		"names": [{}], "refers_to": [], "rejected": [], "speakers": [{}, {}]}}`)
	write(t, after, "reading/gone/1.json", `{"act": "reading", "output": {"names": [], "refers_to": [], "rejected": [], "speakers": []}}`)

	got := yieldOf(t, before, after)

	for _, want := range []string{
		"| Fixture | Spans | Rejected | Names | Speakers | Refers to |",
		"| reading/statement | 3 → 3 | 0 → 0–1 | 0 → 1–2 | 0 → 2 | 0 → 0–1 |",
		"| reading/gone (added) | – → 0 | – → 0 | – → 0 | – → 0 | – → 0 |",
		"| **Total** | 3 → 3 | 0 → 0–1 | 0 → 1–2 | 0 → 2 | 0 → 0–1 |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report has no line %q; it is:\n%s", want, got)
		}
	}
}

// A fixture recorded as samples (<fixture>/<k>.json) is one row, showing the
// range its samples drew, each by its last attempt; the total is the range
// from every fixture's least to its most.
func TestAFixturesSamplesShowAsTheRangeTheyDrew(t *testing.T) {
	before, after := t.TempDir(), t.TempDir()
	story := `{"editor_spans": []}`
	stories := func(n int) string {
		s := make([]string, n)
		for i := range s {
			s[i] = story
		}
		return `{"act": "weaving", "output": {"stories": [` + strings.Join(s, ", ") + `]}`
	}
	write(t, before, "weaving/x.json", stories(1)+`}`)
	write(t, before, "weaving/y.json", stories(0)+`}`)
	write(t, before, "weaving/gone.json", stories(1)+`}`)
	write(t, after, "weaving/x/1.json", stories(2)+`, "earlier_attempts": [`+stories(3)+`}]}`)
	write(t, after, "weaving/x/2.json", stories(0)+`}`)
	write(t, after, "weaving/x/3.json", stories(1)+`}`)
	write(t, after, "weaving/y/1.json", stories(1)+`}`)
	write(t, after, "weaving/y/2.json", stories(1)+`}`)

	got := yieldOf(t, before, after)

	for _, want := range []string{
		"| weaving/x | 1 → 0–2 |",
		"| weaving/y | 0 → 1 |",
		"| weaving/gone (removed) | 1 → – |",
		"| **Total** | 2 → 1–3 |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report has no line %q; it is:\n%s", want, got)
		}
	}
}
