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

func TestTheBriefingIsCountedByItsStatementsTheSpansTheyCiteAndTheStoriesItGathered(t *testing.T) {
	before, after := t.TempDir(), t.TempDir()
	write(t, before, "briefing/saturday.json", `{"act": "briefing", "output": {"statements": [
		{"spans": ["S1"], "stories": [], "text": "a"}
	]}}`)
	write(t, after, "briefing/saturday.json", `{"act": "briefing", "output": {"statements": [
		{"spans": ["S1", "S2"], "stories": [{"span": "S1", "story": ["E1"]}], "text": "a"},
		{"spans": ["S2", "S3"], "stories": [], "text": "c"}
	]}}`)

	got := yieldOf(t, before, after)

	for _, want := range []string{
		"### briefing",
		"| Fixture | Statements | Spans cited | Stories gathered |",
		"| briefing/saturday | 1 → 2 | 1 → 3 | 0 → 1 |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report has no line %q; it is:\n%s", want, got)
		}
	}
}

func TestRelatingYieldCountsRelationsByKindAndASampleWithNoCallAsNone(t *testing.T) {
	before, after := t.TempDir(), t.TempDir()
	write(t, after, "relating/speech/1.json", `{"act": "relating", "output": {"relations": [
		{"span": "S1", "to": "C1", "verdict": "follows"}, {"span": "S2", "to": "C1", "verdict": "follows"},
		{"span": "S3", "to": "C2", "verdict": "same_as"}
	]}}`)
	write(t, after, "relating/speech/2.json", `{"act": "relating", "output": {"relations": [
		{"span": "S1", "to": "C1", "verdict": "follows"}, {"span": "S4", "to": "C3", "verdict": "contradicts"}
	]}}`)
	// The first Item read had nothing read before it: related with no call.
	write(t, after, "relating/first/1.json", `{"act": "", "model": "", "output": null}`)

	got := yieldOf(t, before, after)

	for _, want := range []string{
		"### relating",
		"| Fixture | Follows | Same as | Contradicts |",
		"| relating/first (added) | – → 0 | – → 0 | – → 0 |",
		"| relating/speech (added) | – → 1–2 | – → 0–1 | – → 0–1 |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("yield lacks %q:\n%s", want, got)
		}
	}
}

func TestFrozenInputsAndOtherFilesAreLeftOut(t *testing.T) {
	before, after := t.TempDir(), t.TempDir()
	for _, dir := range []string{before, after} {
		write(t, dir, "relating/x.json", `{"act": "relating", "output": {"relations": []}}`)
		write(t, dir, "sunday/inputs/reading/x.json", `{"act": "reading", "output": {"names": [{}], "refers_to": [], "rejected": [], "speakers": []}}`)
		write(t, dir, "relating/inputs/reading/x.json", `{"act": "reading", "output": {"names": [], "refers_to": [], "rejected": [], "speakers": []}}`)
		write(t, dir, "sunday/conversation.json", `{"act": "sunday_read", "turns": []}`)
		write(t, dir, "sunday/wikidata/search.json", `[{"id": "Q1"}]`)
	}

	got := yieldOf(t, before, after)

	if !strings.Contains(got, "| relating/x | 0 → 0 |") {
		t.Errorf("the report leaves out the relating recording; it is:\n%s", got)
	}
	for _, unwanted := range []string{"inputs", "### reading", "sunday"} {
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
	follows := `{"span": "S1", "to": "C1", "verdict": "follows"}`
	write(t, before, "relating/x.json", `{"act": "relating", "output": {"relations": [`+follows+`]}}`)
	write(t, before, "relating/y.json", `{"act": "relating", "output": {"relations": []}}`)
	write(t, after, "relating/x/1.json", `{"act": "relating", "output": {"relations": [`+follows+`, `+follows+`]},
		"earlier_attempts": [{"act": "relating", "output": {"relations": [`+follows+`, `+follows+`, `+follows+`]}}]}`)
	write(t, after, "relating/x/2.json", `{"act": "relating", "output": {"relations": []}}`)
	write(t, after, "relating/x/3.json", `{"act": "relating", "output": {"relations": [`+follows+`]}}`)
	write(t, after, "relating/y/1.json", `{"act": "relating", "output": {"relations": [`+follows+`]}}`)
	write(t, after, "relating/y/2.json", `{"act": "relating", "output": {"relations": [`+follows+`]}}`)

	got := yieldOf(t, before, after)

	for _, want := range []string{
		"| relating/x | 1 → 0–2 |",
		"| relating/y | 0 → 1 |",
		"| **Total** | 1 → 1–3 |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report has no line %q; it is:\n%s", want, got)
		}
	}
}
