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

// What connecting leaves out is counted, never judged: a Fact it gives no
// Roles links nothing.
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

func TestConnectingYieldCountsFactsInvolvingNoEntity(t *testing.T) {
	before, after := t.TempDir(), t.TempDir()
	write(t, before, "connecting/x.json", `{"act": "connecting", "output": {"causes": [], "facts": [
		{"fact": "F1", "roles": [{"entity": "Ann", "role": "actor"}]},
		{"fact": "F2", "roles": []},
		{"fact": "F3", "roles": []}]}}`)
	write(t, after, "connecting/x/1.json", `{"act": "connecting", "output": {"causes": [], "facts": [
		{"fact": "F1", "roles": [{"entity": "Ann", "role": "actor"}]},
		{"fact": "F2", "roles": []},
		{"fact": "F3", "roles": [{"entity": "Ann", "role": "actor"}]}]}}`)
	write(t, after, "connecting/x/2.json", `{"act": "connecting", "output": {"causes": [], "facts": [
		{"fact": "F1", "roles": [{"entity": "Ann", "role": "actor"}]},
		{"fact": "F2", "roles": [{"entity": "Ann", "role": "actor"}]},
		{"fact": "F3", "roles": [{"entity": "Ann", "role": "actor"}]}]}}`)

	got := yieldOf(t, before, after)

	for _, want := range []string{
		"| Fixture | Connections | Mentions involved in no Fact | Facts involving no Entity |",
		"| connecting/x | 0 → 0 | 0 → 0 | 2 → 0–1 |",
		"| **Total** | 0 → 0 | 0 → 0 | 2 → 0–1 |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report has no line %q; it is:\n%s", want, got)
		}
	}
}

// A mention connecting gives no Role in any Fact is counted, never judged.
// The mentions are those the act was shown: its frozen copy of the
// fixture's reading, on each side.
func TestConnectingYieldCountsMentionsInvolvedInNoFact(t *testing.T) {
	before, after := t.TempDir(), t.TempDir()
	write(t, before, "connecting/inputs/reading/x.json", `{"act": "reading", "output": {"facts": [], "claims": [],
		"mentions": [{"kind": "person", "name": "Ann"}, {"kind": "place", "name": "Kyiv"}]}}`)
	write(t, before, "connecting/x.json", `{"act": "connecting", "output": {"causes": [], "facts": [
		{"fact": "F1", "roles": [{"entity": "Ann", "role": "actor"}, {"entity": "Kyiv", "role": "place"}]}]}}`)
	write(t, after, "connecting/inputs/reading/x.json", `{"act": "reading", "output": {"facts": [], "claims": [],
		"mentions": [{"kind": "person", "name": "Ann"}, {"kind": "place", "name": "Kyiv"}, {"kind": "person", "name": "John Swinney"}]}}`)
	write(t, after, "connecting/x/1.json", `{"act": "connecting", "output": {"causes": [], "facts": [
		{"fact": "F1", "roles": [{"entity": "Ann", "role": "actor"}]},
		{"fact": "F2", "roles": [{"entity": "Ann", "role": "undergoer"}]}]}}`)
	write(t, after, "connecting/x/2.json", `{"act": "connecting", "output": {"causes": [], "facts": [
		{"fact": "F1", "roles": [{"entity": "Ann", "role": "actor"}, {"entity": "Kyiv", "role": "place"}]}]}}`)

	got := yieldOf(t, before, after)

	for _, want := range []string{
		"| connecting/x | 0 → 0 | 0 → 1–2 | 0 → 0 |",
		"| **Total** | 0 → 0 | 0 → 1–2 | 0 → 0 |",
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

// Reading is counted by what it judged of the fixture's Spans, the Spans
// being those the sentence cutter makes of the fixture Item beside the
// recordings. A reading by the earlier act, which drew Facts, judged none.
func TestReadingYieldShowsTheSpansRejectedNamesSpeakersAndReferences(t *testing.T) {
	root := t.TempDir()
	before, after := filepath.Join(root, "old", "recorded"), filepath.Join(root, "repo", "recorded")
	write(t, root, "repo/govuk/statement.json", `{"details": {"body": "<p>Ann said:</p><p>Kyiv is safe. She went there.</p>"}}`)
	write(t, before, "reading/statement.json", `{"act": "reading", "output": {"facts": [{}], "claims": [], "mentions": []}}`)
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
	write(t, before, "connecting/x.json", `{"act": "connecting", "output": {"causes": [{}]}}`)
	write(t, before, "connecting/y.json", `{"act": "connecting", "output": {"causes": []}}`)
	write(t, after, "connecting/x/1.json", `{"act": "connecting", "output": {"causes": [{}, {}]},
		"earlier_attempts": [{"act": "connecting", "output": {"causes": [{}, {}, {}, {}, {}]}}]}`)
	write(t, after, "connecting/x/2.json", `{"act": "connecting", "output": {"causes": []}}`)
	write(t, after, "connecting/x/3.json", `{"act": "connecting", "output": {"causes": [{}]}}`)
	write(t, after, "connecting/y/1.json", `{"act": "connecting", "output": {"causes": [{}]}}`)
	write(t, after, "connecting/y/2.json", `{"act": "connecting", "output": {"causes": [{}]}}`)

	got := yieldOf(t, before, after)

	for _, want := range []string{
		"| connecting/x | 1 → 0–2 |",
		"| connecting/y | 0 → 1 |",
		"| **Total** | 1 → 1–3 |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report has no line %q; it is:\n%s", want, got)
		}
	}
}

// Agreement is how alike a fixture's samples are in what each Fact involves:
// for each Fact, the Jaccard index of two samples' sets, averaged over every
// pair of samples and every Fact. Connecting's samples share their Facts
// (F1, F2...), and its sets are of Entity and Role.
func TestConnectingsAgreementIsTheMeanPairwiseJaccardOfEachFactsEntitiesAndRoles(t *testing.T) {
	before, after := t.TempDir(), t.TempDir()
	write(t, after, "connecting/x/1.json", `{"act": "connecting", "output": {"causes": [], "facts": [
		{"fact": "F1", "roles": [{"entity": "Ann", "role": "actor"}, {"entity": "Kyiv", "role": "place"}]},
		{"fact": "F2", "roles": []}]}}`)
	write(t, after, "connecting/x/2.json", `{"act": "connecting", "output": {"causes": [], "facts": [
		{"fact": "F1", "roles": [{"entity": "Ann", "role": "actor"}, {"entity": "Kyiv", "role": "undergoer"}]},
		{"fact": "F2", "roles": []}]}}`)

	got := yieldOf(t, before, after)

	// F1: {Ann actor} shared of 3 = 1/3; F2: both none = 1. Mean 0.67.
	// By Entity alone, F1 agrees fully: 1.00.
	for _, want := range []string{
		"### agreement",
		"| Fixture | Connecting (Entities and Roles) | Connecting (Entities) |",
		"| x | – → 0.67 | – → 1.00 |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report has no line %q; it is:\n%s", want, got)
		}
	}
}

// The agreement's last row is each column's mean over the fixtures that have
// one.
func TestTheAgreementsMeanIsOverTheFixturesThatHaveOne(t *testing.T) {
	before, after := t.TempDir(), t.TempDir()
	for _, f := range []string{"x", "y"} {
		write(t, after, "connecting/"+f+"/1.json", `{"act": "connecting", "output": {"causes": [], "facts": [
			{"fact": "F1", "roles": [{"entity": "Ann", "role": "actor"}]}]}}`)
	}
	write(t, after, "connecting/x/2.json", `{"act": "connecting", "output": {"causes": [], "facts": [
		{"fact": "F1", "roles": [{"entity": "Ann", "role": "actor"}]}]}}`)
	write(t, after, "connecting/y/2.json", `{"act": "connecting", "output": {"causes": [], "facts": [
		{"fact": "F1", "roles": [{"entity": "Bob", "role": "actor"}]}]}}`)

	got := yieldOf(t, before, after)

	if want := "| **Mean** | – → 0.50 | – → 0.50 |"; !strings.Contains(got, want) {
		t.Errorf("the report has no line %q; it is:\n%s", want, got)
	}
}
