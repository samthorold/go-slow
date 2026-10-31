package editor

import (
	"context"
	"fmt"
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

// Lookup is one consultation of a Reference source, which a Sunday read's
// statement citing it traces to.
type Lookup struct {
	// ID is the lookup's own, once the Record keeps it.
	ID int64
	// Reference names the Reference source.
	Reference string
	// ItemID and Revision are the Wikidata item and the revision of it the
	// lookup saw.
	ItemID   string
	Revision int64
	At       time.Time
	// Licence is what the Reference source licenses its data under.
	Licence string
}

// WikidataLicence is the licence of Wikidata's structured data, checked
// against Wikidata:Licensing: the main, Property and Lexeme namespaces are
// released into the public domain under CC0, so it may be stored and needs
// no attribution (sources research, Reference sources).
const WikidataLicence = "CC0 1.0"

// URL is where the revision the lookup saw can be read.
func (l Lookup) URL() string {
	return fmt.Sprintf("https://www.wikidata.org/w/index.php?oldid=%d", l.Revision)
}
