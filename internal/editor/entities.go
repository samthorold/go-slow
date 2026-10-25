package editor

import (
	"context"
	"fmt"
	"slices"
	"strings"
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

// heldOn is whether the tenure is known to cover the day d.
func (t Tenure) heldOn(d time.Time) bool {
	return !t.Start.IsZero() && !t.Start.After(d) && (t.End.IsZero() || !t.End.Before(d))
}

// wikidataHuman is the class every person is an instance of (Q5).
const wikidataHuman = "Q5"

// Lookup is one consultation of a Reference source, which a Fact drawn from
// it traces to.
type Lookup struct {
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

// resolve looks each Entity of a reading up in Wikidata, as at on, the
// Item's date, and keeps a match on it. An Entity with no match stays the
// Editor's own. The positions a matched person holds on that date become
// Facts traced to the lookup, made at now.
func resolve(ctx context.Context, wd Wikidata, r Reading, on, now time.Time) (Reading, error) {
	on = on.UTC().Truncate(24 * time.Hour)
	for i, e := range r.Entities {
		m, err := match(ctx, wd, e, on)
		if err == nil && m != nil {
			r.Entities[i].WikidataID, r.Entities[i].Label = m.ID, m.Label
			err = positionFacts(ctx, wd, &r, e.Name, *m, on, now)
		}
		if err != nil {
			return Reading{}, fmt.Errorf("look up %q in Wikidata: %w", e.Name, err)
		}
	}
	return r, nil
}

// positionFacts adds to r a Fact for each position the person m holds on
// the day on, where Wikidata dates its start to the day: a Fact holds as at
// a date, and one known only to the year would claim a day no source
// gives.
func positionFacts(ctx context.Context, wd Wikidata, r *Reading, name string, m WikidataItem, on, now time.Time) error {
	var held []Tenure
	var offices []string
	for _, t := range m.Positions {
		if t.StartToTheDay && t.heldOn(on) {
			held = append(held, t)
			offices = append(offices, t.Office)
		}
	}
	if len(held) == 0 {
		return nil
	}
	labels, err := wd.Labels(ctx, offices)
	if err != nil {
		return err
	}
	lookup := &Lookup{Reference: "Wikidata", ItemID: m.ID, Revision: m.Revision, At: now, Licence: WikidataLicence}
	for _, t := range held {
		office, ok := labels[t.Office]
		if !ok {
			continue
		}
		r.ReferenceFacts = append(r.ReferenceFacts, Fact{
			Statement: fmt.Sprintf("%s took office as %s on %s.", m.Label, office, t.Start.Format("2 January 2006")),
			AsAt:      t.Start,
			Quote:     fmt.Sprintf("%s position held (P39) %s, start time (P580) %s", m.ID, t.Office, t.Start.Format(time.DateOnly)),
			Entities:  []string{name},
			Lookup:    lookup,
		})
	}
	return nil
}

// match is the Wikidata item an Entity is, as at on, or nil. Wikidata's own
// search ranks the candidates; match only rules them in or out, so it never
// guesses between candidates Wikidata ranks no differently.
//
// A person given a role is first sought among the role's holders on the
// Item's date: the one holder, when the Item names them only by the role
// ("the Prime Minister"), or the holder the Item's name is part of
// ("Burnham"). Otherwise, and for everything else, the match is the best
// ranked item of the right kind whose label or an alias is the name.
func match(ctx context.Context, wd Wikidata, e Entity, on time.Time) (*WikidataItem, error) {
	name := words(strings.TrimPrefix(strings.ToLower(e.Name), "the "))
	if e.Kind == "person" && e.Role != "" {
		holders, err := holdersOn(ctx, wd, e.Role, on)
		if err != nil {
			return nil, err
		}
		byRole := strings.Contains(words(e.Role), name)
		var named []WikidataItem
		for _, h := range holders {
			if slices.ContainsFunc(append([]string{h.Label}, h.Aliases...), func(n string) bool {
				return strings.Contains(words(n), name)
			}) {
				named = append(named, h)
			}
		}
		switch {
		case len(named) == 1:
			return &named[0], nil
		case byRole && len(holders) == 1:
			return &holders[0], nil
		case byRole:
			return nil, nil
		}
	}
	ids, err := wd.Search(ctx, e.Name)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	items, err := wd.Items(ctx, ids)
	if err != nil {
		return nil, err
	}
	name = words(e.Name)
	for _, it := range items {
		if (e.Kind == "person") != slices.Contains(it.InstanceOf, wikidataHuman) {
			continue
		}
		if slices.ContainsFunc(append([]string{it.Label}, it.Aliases...), func(n string) bool { return words(n) == name }) {
			return &it, nil
		}
	}
	return nil, nil
}

// holdersOn are the people who held the office role names on the day on:
// the office is the best ranked item Wikidata finds for role that has
// holders.
func holdersOn(ctx context.Context, wd Wikidata, role string, on time.Time) ([]WikidataItem, error) {
	ids, err := wd.Search(ctx, role)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	offices, err := wd.Items(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, o := range offices {
		if len(o.Holders) == 0 {
			continue
		}
		var held []string
		for _, t := range o.Holders {
			if t.heldOn(on) && !slices.Contains(held, t.Holder) {
				held = append(held, t.Holder)
			}
		}
		if len(held) == 0 {
			return nil, nil
		}
		return wd.Items(ctx, held)
	}
	return nil, nil
}
