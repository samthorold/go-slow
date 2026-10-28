package web

import (
	"net/url"
	"strconv"

	"github.com/samthorold/go-slow/internal/editor"
)

// pill is an Entity as every page shows it: its name and, as its tag, its
// Role where it has one, otherwise its kind; linked where it has a page
// (its Wikidata item).
type pill struct {
	Name, Tag, Href string
}

// pillsInvolved is the Entities f involves, each tagged with the Role
// connecting gave it.
func pillsInvolved(f editor.Fact) []pill {
	out := make([]pill, 0, len(f.Involves))
	for _, r := range f.Involves {
		out = append(out, pill{Name: r.Entity, Tag: r.Role})
	}
	return out
}

// pillMentioned is an Entity an Item mentions, tagged with its kind and
// linked to the Wikidata item it matched, if any.
func pillMentioned(e editor.Entity) pill {
	p := pill{Name: e.Name, Tag: e.Kind}
	if e.WikidataID != "" {
		p.Href = "https://www.wikidata.org/wiki/" + url.PathEscape(e.WikidataID)
	}
	return p
}

// itemCitation is an Item as every page cites it (template "citation"): its
// Source's name, where the page has it, and its title, linking where it is
// read.
type itemCitation struct {
	Source, Title, Href string
}

// made is how a block was made, shown in its margin as a machine label: the
// act and the call it made, worded "<act> · call N" on every page, and
// "<act> · call N · 2,140 tok" where the page counts tokens (the trail).
type made struct {
	// Act is the act, as the label words it ("read").
	Act string
	// Call is the kept call, or 0 if none was kept.
	Call int64
	// Tokens is the call's tokens, worded in the label only if Counted.
	Tokens  int64
	Counted bool
}

// Href is the call's page.
func (m made) Href() string { return callPath(m.Call) }

// Label is the label's wording.
func (m made) Label() string {
	l := m.Act + " · call " + strconv.FormatInt(m.Call, 10)
	if m.Counted {
		l += " · " + grouped(m.Tokens) + " tok"
	}
	return l
}

// grouped is n with its thousands separated by commas ("2,140").
func grouped(n int64) string {
	s := strconv.FormatInt(n, 10)
	sign := ""
	if n < 0 {
		sign, s = "-", s[1:]
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return sign + s
}
