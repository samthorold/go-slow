package editor

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode"
)

// LookupKey is what a name is looked up in Wikidata by: the name, its kind,
// and for a person named with an office, the office and the Item's date
// (system design: Entities are looked up by name, not by Span). "Derby" in a
// hundred Spans is one key, and one look-up.
type LookupKey struct {
	Name, Kind, Office string
	// On is the Item's date, 2006-01-02 in UTC, where Office is set: an
	// office is held by different people on different days. It is empty
	// otherwise, and where the Item gives no date.
	On string
}

// KeyOf is the key the name n, read in an Item published on published, is
// looked up by.
func KeyOf(n Name, published time.Time) LookupKey {
	k := LookupKey{Name: n.Name, Kind: n.Kind, Office: n.Office}
	if k.Office != "" && !published.IsZero() {
		k.On = published.UTC().Format(time.DateOnly)
	}
	return k
}

// LookupJudge is what made a look-up: plain code, by its name and version.
// A new version is a new judge, and looks up what arrives from then on.
type LookupJudge struct {
	Name    string
	Version int
}

// Looker is the look-up code this build runs: matching names against
// Wikidata's answers (match).
var Looker = LookupJudge{Name: "wikidata", Version: 1}

// NameLookup is the judgement of what a key names in Wikidata: the item it
// matched, or none, with its Judge and when.
type NameLookup struct {
	// ID is the look-up's in the Record, once it is kept there.
	ID    int64
	Key   LookupKey
	Judge LookupJudge
	At    time.Time
	// WikidataID, Label and Revision are the item it matched, its English
	// label and the revision the look-up saw; WikidataID is empty for no
	// match.
	WikidataID string
	Label      string
	Revision   int64
}

// LookUp looks the key k up in Wikidata at now: the item it matches, or
// none, with no certain match (system design: The model names what a Span
// names; plain code finds it in Wikidata). An error is Wikidata's: the
// look-up is not made, and is tried again later.
func LookUp(ctx context.Context, wd Wikidata, k LookupKey, now time.Time) (NameLookup, error) {
	l := NameLookup{Key: k, Judge: Looker, At: now}
	m, err := match(ctx, wd, k)
	if err != nil {
		return NameLookup{}, err
	}
	if m != nil {
		l.WikidataID, l.Label, l.Revision = m.ID, m.Label, m.Revision
	}
	return l, nil
}

// LookupStore is the part of the Record the look-up step reads and writes.
type LookupStore interface {
	// NamesToLookUp are the keys of the names readings judged that no
	// look-up has looked up yet, each once.
	NamesToLookUp(ctx context.Context) ([]LookupKey, error)
	// KeepLookup keeps a look-up as an event. With no match, it makes the
	// Editor's own Entity for the name, if there is none; with a match, it
	// merges into the item each Editor's Entity of its kind made for the
	// key's name or the item's label.
	KeepLookup(ctx context.Context, l NameLookup) error
}

// LookupDeps are the look-up step's dependencies.
type LookupDeps struct {
	Store    LookupStore
	Wikidata Wikidata
	Log      *slog.Logger
}

// Lookups is the look-up step: plain code, run after reading, finding in
// Wikidata what the Spans name, once per key (system design: Entities are
// looked up by name, not by Span).
type Lookups struct {
	LookupDeps
}

// NewLookups builds the look-up step.
func NewLookups(d LookupDeps) *Lookups { return &Lookups{LookupDeps: d} }

// LookUpWaiting looks up, at now, every key not yet looked up. A look-up
// Wikidata refuses or fails, after the adapter's own backoff, is logged and
// kept nowhere, so it is tried again on the next run; it stops no other,
// and holds up no reading, which is kept already. Only a failing Record, or
// ctx ending, is an error.
func (l *Lookups) LookUpWaiting(ctx context.Context, now time.Time) error {
	keys, err := l.Store.NamesToLookUp(ctx)
	if err != nil {
		return err
	}
	for _, k := range keys {
		found, err := LookUp(ctx, l.Wikidata, k, now)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			l.Log.ErrorContext(ctx, "look-up failed", "name", k.Name, "kind", k.Kind, "office", k.Office, "on", k.On, "error", err)
			continue
		}
		if err := l.Store.KeepLookup(ctx, found); err != nil {
			return err
		}
		l.Log.InfoContext(ctx, "looked up", "name", k.Name, "kind", k.Kind, "office", k.Office, "on", k.On,
			"wikidata_id", found.WikidataID)
	}
	return nil
}

// wikidataHuman is the class every person is an instance of (Q5).
const wikidataHuman = "Q5"

// heldOn is whether the tenure is known to cover the day d.
func (t Tenure) heldOn(d time.Time) bool {
	return !t.Start.IsZero() && !t.Start.After(d) && (t.End.IsZero() || !t.End.Before(d))
}

// match is the Wikidata item the key k names, or nil. Wikidata's own search
// ranks the candidates; match only rules them in or out, so it never
// guesses between candidates Wikidata ranks no differently.
//
// A person given an office is first sought among the office's holders on
// the Item's date: the one holder, when the Item names them only by the
// office ("the Prime Minister"), or the holder the Item's name is part of
// ("Burnham"). Otherwise, and for everything else, the match is the best
// ranked item of the right kind whose label or an alias is the name.
func match(ctx context.Context, wd Wikidata, k LookupKey) (*WikidataItem, error) {
	name := words(strings.TrimPrefix(strings.ToLower(k.Name), "the "))
	if k.Kind == "person" && k.Office != "" && k.On != "" {
		on, err := time.Parse(time.DateOnly, k.On)
		if err != nil {
			return nil, err
		}
		holders, err := holdersOn(ctx, wd, k.Office, on)
		if err != nil {
			return nil, err
		}
		byOffice := strings.Contains(words(k.Office), name)
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
		case byOffice && len(holders) == 1:
			return &holders[0], nil
		case byOffice:
			return nil, nil
		}
	}
	ids, err := wd.Search(ctx, k.Name)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	items, err := wd.Items(ctx, ids)
	if err != nil {
		return nil, err
	}
	name = words(k.Name)
	for _, it := range items {
		if (k.Kind == "person") != slices.Contains(it.InstanceOf, wikidataHuman) {
			continue
		}
		if slices.ContainsFunc(append([]string{it.Label}, it.Aliases...), func(n string) bool { return words(n) == name }) {
			return &it, nil
		}
	}
	return nil, nil
}

// holdersOn are the people who held the office on the day on: the office is
// the best ranked item Wikidata finds for its name that has holders.
func holdersOn(ctx context.Context, wd Wikidata, office string, on time.Time) ([]WikidataItem, error) {
	ids, err := wd.Search(ctx, office)
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

// words lowercases s and turns every run of characters that are not letters
// or digits into one space, with a space at each end, so that one name is
// part of another, whatever its punctuation or spacing, exactly when
// words(part) is a substring of words(whole).
func words(s string) string {
	f := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	return " " + strings.Join(f, " ") + " "
}
