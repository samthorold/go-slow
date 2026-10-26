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

// A reading that gives the Item's mentions once counts them as its distinct
// Entities, as an earlier reading counts those its lists name.
func TestAReadingsMentionsAreCountedAsItsEntities(t *testing.T) {
	before, after := t.TempDir(), t.TempDir()
	write(t, before, "reading/statement.json", `{"act": "reading", "output": {
		"facts": [{"entities": [{"kind": "person", "name": "Ann"}]}], "claims": [{"entities": [{"kind": "person", "name": "Ann"}]}]
	}}`)
	write(t, after, "reading/statement.json", `{"act": "reading", "output": {
		"facts": [{"statement": "a"}], "claims": [],
		"mentions": [{"kind": "person", "name": "Ann"}, {"kind": "place", "name": "Kyiv"}]
	}}`)

	got := yieldOf(t, before, after)

	if want := "| reading/statement | 1 → 1 | 1 → 0 | 1 → 2 |"; !strings.Contains(got, want) {
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
		"| Fixture | Facts | Claims | Entities | Projection-worded Facts |",
		"| reading/statement | 2 → 1 | 1 → 0 | 3 → 1 | 0 → 0 |",
		"| **Total** | 2 → 1 | 1 → 0 | 3 → 1 | 0 → 0 |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report has no line %q; it is:\n%s", want, got)
		}
	}
}

// A Fact worded as a future act (will, would, set to, expected to, is to)
// is counted, to see a re-recording's shift; it is reported, never judged.
// Will is the auxiliary only: the noun ("the islanders' will to remain") is
// followed by to or of, where the auxiliary is followed by a verb.
func TestReadingYieldCountsFactsWordedAsAFutureAct(t *testing.T) {
	before, after := t.TempDir(), t.TempDir()
	write(t, before, "reading/statement.json", `{"act": "reading", "output": {
		"facts": [
			{"statement": "The summit will take place in Brussels"},
			{"statement": "Ukraine would implement the ceasefire"},
			{"statement": "A fleet of trains is set to be built at Derby"},
			{"statement": "Construction is expected to begin in 2027"},
			{"statement": "The headquarters is to be built at Becketwell"},
			{"statement": "The PM raised the islanders' will to remain British"},
			{"statement": "The clear and declared will of the islanders"},
			{"statement": "The purpose is torn up; Willingness and Wills"}
		],
		"claims": [{"statement": "The deals will add 10 billion pounds"}],
		"mentions": []
	}}`)
	write(t, after, "reading/statement.json", `{"act": "reading", "output": {
		"facts": [
			{"statement": "The summit is scheduled for Brussels on 20 November"},
			{"statement": "The UK intends to bring further sanctions"},
			{"statement": "The islanders' will to remain British will be respected"}
		],
		"claims": [],
		"mentions": []
	}}`)

	got := yieldOf(t, before, after)

	if want := "| reading/statement | 8 → 3 | 1 → 0 | 0 → 0 | 5 → 1 |"; !strings.Contains(got, want) {
		t.Errorf("the report has no line %q; it is:\n%s", want, got)
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
		"| Fixture | Reading (Entities) | Reading (mentions) | Connecting (Entities and Roles) | Connecting (Entities) |",
		"| x | – → – | – → – | – → 0.67 | – → 1.00 |",
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

	if want := "| **Mean** | – → – | – → – | – → 0.50 | – → 0.50 |"; !strings.Contains(got, want) {
		t.Errorf("the report has no line %q; it is:\n%s", want, got)
	}
}

// Reading's samples draw their own Facts, so a Fact of one sample is paired
// with the most alike statement of the other (by the words they share, at
// least half), and their Entities compared; a Fact with no such pair is
// left out.
func TestReadingsAgreementPairsEachFactWithTheMostAlikeStatementOfTheOtherSample(t *testing.T) {
	before, after := t.TempDir(), t.TempDir()
	write(t, after, "reading/x/1.json", `{"act": "reading", "output": {"claims": [], "facts": [
		{"statement": "Ann visited Kyiv on Monday.", "entities": [{"kind": "person", "name": "Ann"}, {"kind": "place", "name": "Kyiv"}]},
		{"statement": "Bob resigned.", "entities": [{"kind": "person", "name": "Bob"}]}]}}`)
	write(t, after, "reading/x/2.json", `{"act": "reading", "output": {"claims": [], "facts": [
		{"statement": "Ann visited Kyiv.", "entities": [{"kind": "person", "name": "Ann"}]},
		{"statement": "The weather was poor.", "entities": []}]}}`)
	write(t, after, "connecting/x/1.json", `{"act": "connecting", "output": {"causes": [], "facts": []}}`)

	got := yieldOf(t, before, after)

	// Only the visits pair: {Ann, Kyiv} against {Ann}, 0.50. The samples'
	// mentions, {Ann, Kyiv, Bob} against {Ann}, agree at 0.33.
	if want := "| x | – → 0.50 | – → 0.33 | – → – | – → – |"; !strings.Contains(got, want) {
		t.Errorf("the report has no line %q; it is:\n%s", want, got)
	}
}

// Reading's agreement on mentions is the Jaccard index of two samples' sets
// of the names their Item mentions, averaged over every pair of samples: an
// earlier sample's mentions are the names its lists give. A reading that
// gives the mentions once has no per-Fact lists to agree on.
func TestReadingsAgreementOnMentionsComparesEachPairOfSamplesNames(t *testing.T) {
	before, after := t.TempDir(), t.TempDir()
	write(t, before, "reading/x/1.json", `{"act": "reading", "output": {"claims": [{"statement": "c", "entities": [{"kind": "place", "name": "Kyiv"}]}], "facts": [
		{"statement": "Ann visited Kyiv.", "entities": [{"kind": "person", "name": "Ann"}]}]}}`)
	write(t, before, "reading/x/2.json", `{"act": "reading", "output": {"claims": [], "facts": [
		{"statement": "Ann visited Kyiv.", "entities": [{"kind": "person", "name": "Ann"}, {"kind": "place", "name": "Kyiv"}]}]}}`)
	write(t, after, "reading/x/1.json", `{"act": "reading", "output": {"claims": [], "facts": [{"statement": "Ann visited Kyiv."}],
		"mentions": [{"kind": "person", "name": "Ann"}, {"kind": "place", "name": "Kyiv"}]}}`)
	write(t, after, "reading/x/2.json", `{"act": "reading", "output": {"claims": [], "facts": [{"statement": "Ann visited Kyiv."}],
		"mentions": [{"kind": "person", "name": "Ann"}]}}`)
	write(t, after, "connecting/x/1.json", `{"act": "connecting", "output": {"causes": [], "facts": []}}`)

	got := yieldOf(t, before, after)

	// Before: the lists' Fact pair {Ann} against {Ann, Kyiv}, 0.50; the
	// mentions agree, {Ann, Kyiv} both. After: no lists; mentions 0.50.
	for _, want := range []string{
		"| Fixture | Reading (Entities) | Reading (mentions) | Connecting (Entities and Roles) | Connecting (Entities) |",
		"| x | 0.50 → – | 1.00 → 0.50 | – → – | – → – |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report has no line %q; it is:\n%s", want, got)
		}
	}
}
