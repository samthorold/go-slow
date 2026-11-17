package editor

import (
	"context"
	"time"
)

// Wikidata is the port to Wikidata, the Reference source Entities come from
// first (system design: Entities come from Wikidata first, otherwise from
// the Editor). Its structured data is CC0, so what it answers may be kept.
type Wikidata interface {
	// Search is the items whose label or alias matches text, best first.
	Search(ctx context.Context, text string) ([]string, error)
	// Items are the items with these ids, in the order asked.
	Items(ctx context.Context, ids []string) ([]WikidataItem, error)
	// Labels are the English labels of these items, by id.
	Labels(ctx context.Context, ids []string) (map[string]string, error)
}

// WikidataItem is what resolving an Entity looks at of a Wikidata item.
type WikidataItem struct {
	ID      string
	Label   string
	Aliases []string
	// InstanceOf are the classes it is an instance of (P31).
	InstanceOf []string
	// Positions are the positions a person has held (P39).
	Positions []Tenure
	// Holders are the people who have held an office (P1308).
	Holders []Tenure
	// Revision is the item's revision the lookup saw.
	Revision int64
}

// Tenure is a person holding an office, from a date until a date. A zero
// Start is not known; a zero End is not over.
type Tenure struct {
	Holder string
	Office string
	Start  time.Time
	End    time.Time
	// StartToTheDay is whether Wikidata gives Start to the day, not only
	// the month or year.
	StartToTheDay bool
}
