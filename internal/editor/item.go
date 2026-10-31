package editor

import (
	"errors"
	"slices"
	"time"
)

// Item is what the acts look at of a collected Item.
type Item struct {
	SourceID  string
	EntryID   string
	Title     string
	Link      string
	Summary   string
	Content   string
	Published time.Time
}

// ErrFailedChecks is the error for a model result that fails the act's
// checks. Nothing of it is written to the Record.
var ErrFailedChecks = errors.New("the result fails the act's checks")

func str() map[string]any { return map[string]any{"type": "string"} }

func integer() map[string]any { return map[string]any{"type": "integer"} }

func array(items map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": items}
}

func object(props map[string]any) map[string]any {
	required := make([]string, 0, len(props))
	for k := range props {
		required = append(required, k)
	}
	slices.Sort(required)
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}
