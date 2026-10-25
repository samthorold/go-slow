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

func TestEntitiesNamedAsPlainStringsAreCountedToo(t *testing.T) {
	// Readings recorded before Entities carried a kind named them as strings.
	before, after := t.TempDir(), t.TempDir()
	write(t, before, "reading/statement.json", `{"act": "reading", "output": {
		"facts": [{"entities": ["Ann", "Kyiv"]}], "claims": [{"entities": ["Ann"]}]
	}}`)
	write(t, after, "reading/statement.json", `{"act": "reading", "output": {
		"facts": [{"entities": [{"kind": "person", "name": "Ann"}]}], "claims": []
	}}`)

	got := yieldOf(t, before, after)

	if want := "| reading/statement | 1 → 1 | 1 → 0 | 2 → 1 |"; !strings.Contains(got, want) {
		t.Errorf("the report has no line %q; it is:\n%s", want, got)
	}
}

func TestEachLaterActIsCountedByWhatItDraws(t *testing.T) {
	before, after := t.TempDir(), t.TempDir()
	write(t, before, "placing/statement.json", `{"act": "placing", "output": {
		"facts": [{"fact": "F1", "developments_in": ["N1", "S2"]}, {"fact": "F2", "developments_in": []}],
		"new_stories": [{"ref": "N1", "title": "t"}]
	}}`)
	write(t, after, "placing/statement.json", `{"act": "placing", "output": {
		"facts": [{"fact": "F1", "developments_in": ["S2"]}], "new_stories": []
	}}`)
	write(t, before, "connecting/statement.json", `{"act": "connecting", "output": {
		"facts": [], "causes": [{"cause": "F1", "effect": "F2"}, {"cause": "F2", "effect": "F3"}]
	}}`)
	write(t, after, "connecting/statement.json", `{"act": "connecting", "output": {"facts": [], "causes": []}}`)
	write(t, before, "briefing/saturday.json", `{"act": "briefing", "output": {"stories": [
		{"story": "S1", "statements": [{"text": "a"}, {"text": "b"}]}
	]}}`)
	write(t, after, "briefing/saturday.json", `{"act": "briefing", "output": {"stories": [
		{"story": "S1", "statements": [{"text": "a"}]}, {"story": "S2", "statements": [{"text": "c"}]}
	]}}`)

	got := yieldOf(t, before, after)

	for _, want := range []string{
		"### placing",
		"| Fixture | Developments | New Stories |",
		"| placing/statement | 2 → 1 | 1 → 0 |",
		"### connecting",
		"| Fixture | Connections |",
		"| connecting/statement | 2 → 0 |",
		"### briefing",
		"| Fixture | Statements | Stories |",
		"| briefing/saturday | 2 → 2 | 1 → 2 |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report has no line %q; it is:\n%s", want, got)
		}
	}
}

func TestFrozenInputsAndOtherFilesAreLeftOut(t *testing.T) {
	before, after := t.TempDir(), t.TempDir()
	for _, dir := range []string{before, after} {
		write(t, dir, "connecting/x.json", `{"act": "connecting", "output": {"causes": []}}`)
		write(t, dir, "sunday/inputs/reading/x.json", `{"act": "reading", "output": {"facts": [{}], "claims": []}}`)
		write(t, dir, "connecting/inputs/placing/x.json", `{"act": "placing", "output": {"facts": [], "new_stories": [{}]}}`)
		write(t, dir, "sunday/conversation.json", `{"act": "sunday_read", "turns": []}`)
		write(t, dir, "sunday/wikidata/search.json", `[{"id": "Q1"}]`)
	}

	got := yieldOf(t, before, after)

	if !strings.Contains(got, "| connecting/x | 0 → 0 |") {
		t.Errorf("the report leaves out the connecting recording; it is:\n%s", got)
	}
	for _, unwanted := range []string{"inputs", "### reading", "### placing", "sunday"} {
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

func TestAddedAndRemovedFixturesShowAsSuch(t *testing.T) {
	before, after := t.TempDir(), t.TempDir()
	write(t, before, "reading/gone.json", `{"act": "reading", "output": {"facts": [{}, {}], "claims": []}}`)
	write(t, after, "reading/new.json", `{"act": "reading", "output": {"facts": [{}], "claims": [{}]}}`)

	got := yieldOf(t, before, after)

	for _, want := range []string{
		"| reading/gone (removed) | 2 → – | 0 → – | 0 → – |",
		"| reading/new (added) | – → 1 | – → 1 | – → 0 |",
		"| **Total** | 2 → 1 | 0 → 1 | 0 → 0 |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report has no line %q; it is:\n%s", want, got)
		}
	}
}

func TestReadingYieldShowsFactsClaimsAndEntitiesBeforeAndAfter(t *testing.T) {
	before, after := t.TempDir(), t.TempDir()
	write(t, before, "reading/statement.json", `{"act": "reading", "output": {
		"facts": [
			{"statement": "a", "entities": [{"kind": "person", "name": "Ann"}, {"kind": "place", "name": "Kyiv"}]},
			{"statement": "b", "entities": [{"kind": "person", "name": "Ann"}]}
		],
		"claims": [{"statement": "c", "entities": [{"kind": "person", "name": "Bob"}]}]
	}}`)
	write(t, after, "reading/statement.json", `{"act": "reading", "output": {
		"facts": [{"statement": "a", "entities": [{"kind": "person", "name": "Ann"}]}],
		"claims": []
	}}`)

	got := yieldOf(t, before, after)

	for _, want := range []string{
		"| Fixture | Facts | Claims | Entities |",
		"| reading/statement | 2 → 1 | 1 → 0 | 3 → 1 |",
		"| **Total** | 2 → 1 | 1 → 0 | 3 → 1 |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report has no line %q; it is:\n%s", want, got)
		}
	}
}
