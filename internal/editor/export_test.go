package editor

import (
	"context"
	"fmt"
)

// ReadAsKept is Read for a frozen copy of a reading: the recorded answer
// taken as the Reading it was kept as, without the checks that judge a
// reading (whether each Fact and Claim involves every Entity it names). A
// frozen copy is an act's input, not a reading under test: reading's own
// recorded test judges reading's recordings, and a copy is brought under a
// newer standard by its next refresh, never by failing the acts that rest on
// it.
func ReadAsKept(ctx context.Context, m Model, it Item) (Reading, error) {
	req, err := readingRequest(it, nil)
	if err != nil {
		return Reading{}, err
	}
	res, err := m.Complete(ctx, req)
	if err != nil {
		return Reading{}, fmt.Errorf("reading %s: %w", it.EntryID, err)
	}
	return decodeReading(it, nil, res)
}
