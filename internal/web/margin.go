package web

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/samthorold/go-slow/internal/editor"
)

// pill is an Entity as every page shows it: its name and, as its tag, a
// person's office where the name is read so, otherwise its kind; linked
// where it has a page (its Wikidata item).
type pill struct {
	Name, Tag, Href string
}

// judged is a judgement as a margin line names it: whose words a Span is
// ("spoken by …"), or what a stretch of it refers to.
type judged struct {
	// Words are what the line names: the speaker, or the stretch.
	Words string
	// To is what the stretch refers to, for a refers to.
	To string
}

// pillOf is an Entity as the Record's look-ups have it, tagged with its kind
// and linked to its Wikidata item, if it is one.
func pillOf(e editor.NamedEntity) pill {
	p := pill{Name: e.Name, Tag: e.Kind}
	if e.WikidataID != "" {
		p.Href = "https://www.wikidata.org/wiki/" + url.PathEscape(e.WikidataID)
	}
	return p
}

// itemCitation is an Item as every page cites it (template "citation"): its
// Source's short name, who is speaking, and its title, linking where it is
// read.
type itemCitation struct {
	Source, Title, Href string
}

// cite is the citation of an Item from the Source sourceID, titled title,
// linking href: the Source named by its short name from the Sources list
// (none if the list doesn't hold it). The short name is the pages' alone;
// no act is shown it.
func (a *app) cite(sourceID, title, href string) itemCitation {
	return itemCitation{Source: a.shortNames[sourceID], Title: title, Href: href}
}

// made is how a block was made, shown in its margin as a machine label: the
// act and the call it made, worded "<act> · call N" on every page.
type made struct {
	// Act is the act, as the label words it ("read").
	Act string
	// Call is the kept call, or 0 if none was kept.
	Call int64
}

// Href is the call's page.
func (m made) Href() string { return callPath(m.Call) }

// Label is the label's wording.
func (m made) Label() string { return m.Act + " · call " + strconv.FormatInt(m.Call, 10) }

// labelLength is the most of a Span's words, in runes, that names it where
// a page lists it by its opening words: enough to tell them apart.
const labelLength = 60

// shorten is s, cut at the last word that fits within labelLength and marked
// with an ellipsis if it is longer.
func shorten(s string) string {
	r := []rune(s)
	if len(r) <= labelLength {
		return s
	}
	cut := string(r[:labelLength])
	if i := strings.LastIndex(cut, " "); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,;:") + "…"
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
