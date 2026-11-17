package editor_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The compatibility check (system design: Each Command is recorded over its
// own frozen inputs). Until a refresh, weaving is replayed over its frozen
// copies of what reading used to write. So reading's current recordings,
// each fixture's first sample as a refresh would copy it, hydrate the
// fixture Items, and each of weaving's runs is woven over them on a fake
// model answering from its request alone: every Item must be hydrated,
// weaving's request must build and show every Item, and the fake's answer
// must pass weaving's checks. A change to what reading writes that weaving
// can't take fails here at once, with no recording made. A change in what
// reading judges is not a failure: a refresh brings it downstream.
func TestReadingsCurrentRecordingsAreTakenByWeaving(t *testing.T) {
	t.Parallel()
	if err := feedWeaving(t, filepath.Join(recordedDir, "reading")); err != nil {
		t.Error(err)
	}
}

// A reading weaving can't be shown fails the check, naming its fixture: one
// whose answer reading's checks now refuse hydrates nothing.
func TestTheCompatibilityCheckFailsOnAReadingWeavingCannotTake(t *testing.T) {
	t.Parallel()
	readings := filepath.Join(t.TempDir(), "reading")
	if err := os.CopyFS(readings, os.DirFS(filepath.Join(recordedDir, "reading"))); err != nil {
		t.Fatal(err)
	}
	name := "pm-speech-in-scotland-16-october-2026"
	path := samplePath(readings, name, 1)
	s, err := sampleAt(path)
	if err != nil {
		t.Fatal(err)
	}
	attempts := s.attempts()
	attempts[len(attempts)-1].Output = json.RawMessage(`{"facts": []}`)
	keepSample(t, path, attempts)

	err = feedWeaving(t, readings)
	if err == nil || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), "not hydrated") {
		t.Fatalf("feedWeaving = %v; want %s not hydrated", err, name)
	}
}

// feedWeaving copies the first sample of each fixture's reading under
// readings, as a refresh would, and weaves each of weaving's runs over the
// fixture Items they hydrate, on a fake model: the first failure.
func feedWeaving(t *testing.T, readings string) error {
	t.Helper()
	frozen := t.TempDir()
	for _, f := range fixtureItems(t) {
		b, err := os.ReadFile(samplePath(readings, f.name, 1))
		if err != nil {
			return fmt.Errorf("%s: reading has no recording: %w", f.name, err)
		}
		if err := os.WriteFile(filepath.Join(frozen, f.name+".json"), b, 0o600); err != nil {
			return err
		}
	}
	store, _ := newRecordAt(t, filepath.Join(t.TempDir(), "go-slow.db"), nil)
	fake := &weavingOnShown{}
	for _, r := range weavingRuns(t) {
		if err := collectCutAndHydrate(t.Context(), store, r, frozen); err != nil {
			return err
		}
		m := recordingSample(t, samplePath(t.TempDir(), r.name, 1), fake, "fake", weavingCommand)
		passed, err := weave(t, store, m, r.at)
		if err != nil {
			return err
		}
		if !passed || len(fake.sent) == 0 {
			return fmt.Errorf("%s: weaving's checks refuse a fake's answer over reading's current recordings", r.name)
		}
		in := fake.sent[len(fake.sent)-1].Input
		for _, f := range r.items {
			if !strings.Contains(in, fmt.Sprintf("Story from %q", f.item.Title)) {
				return fmt.Errorf("%s: weaving was never shown %s", r.name, f.name)
			}
		}
	}
	return nil
}
