package collect

import "fmt"

// Format is how a Source's feed or API endpoint is written, which decides
// how its entries are read as Items.
type Format int

const (
	// Atom is an Atom (RFC 4287) feed, such as GOV.UK's.
	Atom Format = iota
	// ONSReleases is the ONS API's release search (/v1/search/releases)
	// asked for published releases.
	ONSReleases
	// WrittenStatements is Parliament's Written Questions Service API list
	// of written statements (/api/writtenstatements/statements).
	WrittenStatements
	// ONSReleaseCalendar is the same ONS release search asked for upcoming
	// releases: each entry is a scheduled event, due on its release date.
	ONSReleaseCalendar
)

// parse reads a feed of this Format as Items (without Source or collection
// time).
func (f Format) parse(body []byte) ([]Item, error) {
	switch f {
	case Atom:
		return parseAtom(body)
	case ONSReleases:
		return parseONSReleases(body)
	case ONSReleaseCalendar:
		return parseONSCalendar(body)
	case WrittenStatements:
		return parseWrittenStatements(body)
	default:
		return nil, fmt.Errorf("unknown feed format %d", f)
	}
}
