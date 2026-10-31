package editor

import "context"

// What the editor_test package tests of the Editor's choice of precedents,
// which no request shows past its cap.
var (
	BriefingPrecedents = briefingPrecedents
	RelatingPrecedents = relatingPrecedents
	PrecedentsShown    = precedentsShown
)

// ToRelate are the Items awaiting relating as relating is shown them, each
// with its candidates.
func (e *Editor) ToRelate(ctx context.Context) ([]ToRelate, error) { return e.toRelate(ctx) }
