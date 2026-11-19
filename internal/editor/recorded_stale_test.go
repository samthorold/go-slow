package editor_test

import (
	"context"
	"strings"
	"testing"

	"github.com/samthorold/go-slow/internal/editor"
)

// A request as an act builds it, and the same act with its instructions
// changed: what a change to the act's code sends instead.
var (
	asRecorded = editor.Request{Act: "hydration", Instructions: "Read the Item.", Input: "Item: one",
		Schema: map[string]any{"type": "object"}}
	changed = editor.Request{Act: "hydration", Instructions: "Read the Item closely.", Input: "Item: one",
		Schema: map[string]any{"type": "object"}}
)

// recordedOn is what recording req on the model named model keeps.
func recordedOn(t *testing.T, req editor.Request, model string) recording {
	t.Helper()
	var rec recording
	if _, err := (recorder{&fakeModel{output: `{"facts": []}`}, &rec, model}).Complete(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	return rec
}

// Recording keeps the hash of the request the act sent, with the model it
// asked for: the one function a kept call's hash is.
func TestARecordingKeepsTheHashOfTheRequestItAnswered(t *testing.T) {
	rec := recordedOn(t, asRecorded, "claude-haiku-5-5")
	if want := editor.RequestHash(asRecorded, "claude-haiku-5-5"); rec.RequestHash != want {
		t.Errorf("request hash = %q, want %q", rec.RequestHash, want)
	}
}

// replayOf is a replay of rec, kept as the one attempt of sample 1 of the
// fixture "one", hashing with model, and what it reports.
func replayOf(t *testing.T, rec recording, model string) (*sampleModel, *[]error) {
	t.Helper()
	path := samplePath(t.TempDir(), "one", 1)
	keepSample(t, path, []recording{rec})
	var reported []error
	return replayingSample(t, path, model, hydrationCommand, func(err error) { reported = append(reported, err) }), &reported
}

func TestAReplayAnswersTheRequestItWasRecordedFor(t *testing.T) {
	m, reported := replayOf(t, recordedOn(t, asRecorded, "claude-haiku-5-5"), "claude-haiku-5-5")
	res, err := m.Complete(t.Context(), asRecorded)
	if err != nil || strings.Join(strings.Fields(string(res.Output)), "") != `{"facts":[]}` || len(*reported) != 0 {
		t.Errorf("replay = %s, %v (reported %v); want the recorded output", res.Output, err, *reported)
	}
}

// After the act's instructions change, its old recording answers a request
// the act no longer sends: the replay fails, naming the fixture and the
// command that re-records it, rather than passing on a prompt that no
// longer exists.
func TestAReplayFailsAsStaleWhenTheActsRequestHasChanged(t *testing.T) {
	rec := recordedOn(t, asRecorded, "claude-haiku-5-5")
	for name, tc := range map[string]struct {
		req   editor.Request
		model string
	}{
		"the instructions changed": {changed, "claude-haiku-5-5"},
		"the model changed":        {asRecorded, "claude-opus-5-5"},
	} {
		t.Run(name, func(t *testing.T) {
			m, reported := replayOf(t, rec, tc.model)
			_, err := m.Complete(t.Context(), tc.req)
			if err == nil {
				t.Fatal("a stale recording replayed")
			}
			for _, want := range []string{"one", "stale: re-record with", "go test -count=1 ./internal/editor -run TestRecordedHydration -record", "HYDRATION_MODEL"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not say %q", err, want)
				}
			}
			if len(*reported) != 1 {
				t.Errorf("reported %v, want the staleness once", *reported)
			}
		})
	}
}

// A recording made before recordings kept their request's hash can't show
// it answers today's request: it is stale until it is re-recorded.
func TestAReplayOfARecordingWithNoHashFailsAsStale(t *testing.T) {
	rec := recordedOn(t, asRecorded, "claude-haiku-5-5")
	rec.RequestHash = ""
	m, _ := replayOf(t, rec, "claude-haiku-5-5")
	if _, err := m.Complete(context.Background(), asRecorded); err == nil || !strings.Contains(err.Error(), "stale: re-record with") {
		t.Errorf("replay = %v; want it stale", err)
	}
}

// The model a replay hashes with is the one the act is pinned to, so
// changing a pin makes its recordings stale, as .env.example says.
func TestTheModelAReplayHashesWithIsTheActsPin(t *testing.T) {
	if got := pinnedModel(t, "HYDRATION_MODEL"); got != "claude-haiku-5-5" {
		t.Errorf("HYDRATION_MODEL is pinned to %q, want claude-haiku-5-5", got)
	}
	if got := pinnedModel(t, "WEAVING_MODEL"); got != "claude-opus-5-5" {
		t.Errorf("WEAVING_MODEL is pinned to %q, want claude-opus-5-5", got)
	}
	if got := pinnedModel(t, "NAMING_MODEL"); got != "claude-haiku-5-5" {
		t.Errorf("NAMING_MODEL is pinned to %q, want claude-haiku-5-5", got)
	}
}
