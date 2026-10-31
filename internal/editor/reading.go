package editor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/samthorold/go-slow/internal/cut"
)

// ReadingVersion is the version of reading's instructions and schema. A new
// version is a new Judge: it reads what arrives from then on, and the
// readings made before stand, with theirs (system design: Every judgement
// carries its judge, and a changed judge judges forward). Change it with
// readingInstructions or readingSchema.
const ReadingVersion = 1

// ToRead is an Item awaiting reading, with the Cut it is shown as. The
// Item's Content is its text as kept, which the Cut's Spans address.
type ToRead struct {
	Item Item
	Cut  cut.Cut
	// Precedents are the standing Feedback it is shown as precedent, after
	// its Spans; none for a case with none.
	Precedents []Precedent
}

// Address is a Span of an Item: where it starts and ends in the Item's text
// as kept. Two judgements about the same words have equal Addresses.
type Address struct {
	SourceID, EntryID string
	Span              cut.Span
}

// Judge is what made a judgement: the act, the version of its instructions,
// the model that answered, and the hash of the request it answered
// (RequestHash, with the model asked for), as a kept call records them.
type Judge struct {
	Act         string
	Version     int
	Model       string
	RequestHash string
}

// RefersTo is the judgement that the Span From names the same thing as the
// Span To, of the same Item, in other words: "the city" refers to "Derby".
type RefersTo struct {
	// ID is its event's in the Record, once it is kept there: what
	// Feedback on it names.
	ID       int64
	From, To Address
}

// Name is the judgement that the Span At names an Entity: by the name the
// Item gives it in full, its kind, and for a person the office the Item
// gives them, in full.
type Name struct {
	At                 Address
	Name, Kind, Office string
}

// Speaker is the judgement that the Span Of is the words of whoever the
// Span By names. A Span with no Speaker is the publisher's own.
type Speaker struct {
	// ID is its event's in the Record, once it is kept there.
	ID     int64
	Of, By Address
}

// Reading is reading an Item: the judgements it made of the Item's Spans,
// with its Judge. Each judgement is kept as its own event (system design:
// Reading an Item makes construction's model judgements in one call).
type Reading struct {
	// ID is the reading's in the Record, once it is kept there.
	ID                int64
	SourceID, EntryID string
	// CutID is the Cut the Item was shown as.
	CutID int64
	Judge Judge
	// Candidate is the candidate Judge it was made as, read by no view until
	// that candidate is chosen; zero for a reading made as the live judge.
	Candidate int64
	// At is when it was judged.
	At       time.Time
	RefersTo []RefersTo
	Names    []Name
	Speakers []Speaker
	// Rejected are the Spans judged not useful for editorial work.
	Rejected []Address
	// Precedents are the ids of the Feedback it was shown as precedent, in
	// the order shown, kept with it.
	Precedents []int64
	// Usage is what the call cost, for the logs; not kept with the reading.
	Usage Usage
	// Call is the kept call that made it, as a view finds it by its Judge's
	// request hash; zero if none was kept.
	Call int64
}

// The kinds of Entity a Span can name.
var entityKinds = []string{"person", "organisation", "place", "occasion"}

// readingInstructions are the reading act's own instructions.
const readingInstructions = `You are the Editor of a private slow-news briefing. You are reading one Item: a piece of material collected from a Source, such as a government press release, speech or statement. You are shown it as its Spans, numbered S1, S2 and so on: the stretches of its text, in order, as its publisher wrote them. You never reword a Span; you judge things of it.

Cite a Span by its number. To cite a stretch within a Span (a word or a few), give the Span's number and the stretch's words, copied character for character from that Span. Leave the words empty to cite the whole Span.

Judge, of this Item alone:
- names: each stretch that names a person, organisation, place or named occasion (such as a summit or a Budget), every time one does, including by an office or title alone ("the Prime Minister", "the Chancellor"). Cite the stretch, and give the name in its fullest form in the Item and its kind. A person the Item names by name is named by that name; one it never names, by the office as the Item gives it. For a person the Item gives an office or position, give that office in full, with its country or body where the Item makes it plain ("Prime Minister of the United Kingdom", "Secretary of State for Defence"); otherwise leave the office empty. A place's name the Item also uses for its authority ("Greater Manchester") is a place. A pronoun or a description in other words ("he", "the city") is not a name: it refers to one.
- refers_to: each stretch that names something only in other words than this Item names it elsewhere: a pronoun such as "he", "she", "it" or "they", or a description such as "the city", "the region", "the department" or "the scheme". Go through the Spans in order and give every such stretch, each time one occurs, citing it in the Span where its words actually are, with the stretch of this Item that names the same thing most fully. Words that recur ("the city" in three Spans) are cited in every Span they occur in. A Span with no such words gets none: never cite words a Span does not contain. Only where the Item makes plain what it refers to. Not for "I", "we", "my", "our" and the like: who says them is the speaker's.
- rejected: the numbers of the Spans that are not useful for editorial work, saying nothing on their own of what happened or what anyone said or thinks: a bare attribution introducing a quotation ("Andy Burnham said:"), a heading ("Background"), contact details, a sign-off. A Span that is someone's words is not rejected for that.
- speakers: each Span that is someone's words rather than the publisher's own (a quotation, what a spokesperson said, a statement given in someone's voice), cited whole, with the stretch naming whose words they are: by name where this Item gives one ("Andy Burnham"), otherwise as the Item describes them ("a UK government spokesperson"). Give every Span of a quotation its speaker, however many Spans it runs to. The stretch naming the speaker may be in a Span you reject, such as "Andy Burnham said:". Give no speaker for the publisher's own words, or where no Span of this Item names who speaks.

Keep to what the Item says. Do not add background knowledge.`

// spanRef is a Span as the model cites it: a Span of the Cut by its
// number, and the words of a stretch within it, or none for the whole.
var spanRef = object(map[string]any{
	"span":  integer(),
	"words": str(),
})

// readingSchema is the reading act's output schema. Its fields are written
// in name order (names, refers_to, rejected, speakers), so the model names
// what the Spans name before it says what refers to it, and judges what a
// Span refers to before whether it is useful and whose words it is, both of
// which turn on what it refers to. Within each, what is cited comes in name
// order too: a reference's from before its to, a speaker's span before who
// speaks it.
var readingSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"names": array(object(map[string]any{
			"at":     spanRef,
			"kind":   map[string]any{"type": "string", "enum": entityKinds},
			"name":   str(),
			"office": str(),
		})),
		"refers_to": array(object(map[string]any{
			"from": spanRef,
			"to":   spanRef,
		})),
		"rejected": array(integer()),
		"speakers": array(object(map[string]any{
			"span":    spanRef,
			"speaker": spanRef,
		})),
	},
	"required":             []string{"names", "refers_to", "rejected", "speakers"},
	"additionalProperties": false,
}

// cited is a Span as the model cites it.
type cited struct {
	Span  int    `json:"span"`
	Words string `json:"words"`
}

// readingOutput is the reading act's output as the model gives it. An answer
// in the earlier reading act's schema (facts, claims, mentions) does not
// match it.
type readingOutput struct {
	Names *[]struct {
		At     cited  `json:"at"`
		Kind   string `json:"kind"`
		Name   string `json:"name"`
		Office string `json:"office"`
	} `json:"names"`
	RefersTo *[]struct {
		From cited `json:"from"`
		To   cited `json:"to"`
	} `json:"refers_to"`
	Rejected *[]int `json:"rejected"`
	Speakers *[]struct {
		Span    cited `json:"span"`
		Speaker cited `json:"speaker"`
	} `json:"speakers"`
}

// Read is the act of reading an Item on the model m, which is asked for as
// the model named asked: its judgements of the Item's Spans, with the call as
// their Judge. A result that fails the act's checks is ErrFailedChecks, and
// none of it is returned.
func Read(ctx context.Context, m Model, asked string, tr ToRead) (Reading, error) {
	req := readingRequest(tr)
	res, err := m.Complete(ctx, req)
	if err != nil {
		return Reading{}, fmt.Errorf("reading %s: %w", tr.Item.EntryID, err)
	}
	return checkReading(tr, req, asked, res)
}

// readingRequest is the reading act's call on a model for an Item, shown as
// its Cut.
func readingRequest(tr ToRead) Request {
	it := tr.Item
	var b strings.Builder
	fmt.Fprintf(&b, "Title: %s\n", it.Title)
	if !it.Published.IsZero() {
		fmt.Fprintf(&b, "Published: %s\n", it.Published.UTC().Format(time.DateOnly))
	}
	b.WriteString("\n")
	for i, s := range tr.Cut.Spans {
		fmt.Fprintf(&b, "S%d: %s\n", i+1, cut.Words(it.Content, s))
	}
	return Request{Act: "reading", Instructions: readingInstructions, Input: withPrecedents(b.String(), tr.Precedents, readingLine),
		Schema: readingSchema}
}

// checkReading decodes the model's answer to req, asked of the model named
// asked, on the Item tr, and runs the act's checks over it. A result that
// fails them is ErrFailedChecks.
func checkReading(tr ToRead, req Request, asked string, res Response) (Reading, error) {
	it := tr.Item
	refuse := func(format string, args ...any) (Reading, error) {
		return Reading{}, fmt.Errorf("reading %s: %w: %s", it.EntryID, ErrFailedChecks, fmt.Sprintf(format, args...))
	}
	var out readingOutput
	dec := json.NewDecoder(bytes.NewReader(res.Output))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return refuse("output does not match the schema: %v", err)
	}
	if out.Names == nil || out.RefersTo == nil || out.Rejected == nil || out.Speakers == nil {
		return refuse("output does not match the schema: a kind of judgement is missing")
	}
	r := Reading{
		SourceID: it.SourceID, EntryID: it.EntryID, CutID: tr.Cut.ID, Usage: res.Usage, Precedents: precedentIDs(tr.Precedents),
		Judge: Judge{Act: req.Act, Version: ReadingVersion, Model: res.Model, RequestHash: RequestHash(req, asked)},
	}
	// address is the Address the model's citation stands for: the Span of
	// the Cut it numbers, or the stretch of it its words are.
	address := func(c cited) (Address, error) {
		if c.Span < 1 || c.Span > len(tr.Cut.Spans) {
			return Address{}, fmt.Errorf("the Item has no S%d: its Spans are S1 to S%d", c.Span, len(tr.Cut.Spans))
		}
		s, ok := cut.Within(it.Content, tr.Cut.Spans[c.Span-1], c.Words)
		if !ok {
			return Address{}, fmt.Errorf("the words %q are not in S%d", c.Words, c.Span)
		}
		return Address{SourceID: it.SourceID, EntryID: it.EntryID, Span: s}, nil
	}
	for _, n := range *out.Names {
		at, err := address(n.At)
		if err != nil {
			return refuse("a name: %v", err)
		}
		name, office := strings.TrimSpace(n.Name), strings.TrimSpace(n.Office)
		if name == "" || !slices.Contains(entityKinds, n.Kind) {
			return refuse("a name with no name or kind at S%d: %q, %q", n.At.Span, n.Name, n.Kind)
		}
		if n.Kind != "person" {
			// Only a person has an office, to find them by among its holders.
			// The model fills the field for other kinds whenever anything
			// near it describes what an Entity does, so any other kind's
			// office is dropped here, whatever it writes.
			office = ""
		}
		named := Name{At: at, Name: name, Kind: n.Kind, Office: office}
		if !slices.Contains(r.Names, named) {
			r.Names = append(r.Names, named)
		}
	}
	for _, ref := range *out.RefersTo {
		from, err := address(ref.From)
		if err != nil {
			return refuse("a refers to: %v", err)
		}
		to, err := address(ref.To)
		if err != nil {
			return refuse("a refers to: %v", err)
		}
		// A Span referring to itself says nothing.
		if refers := (RefersTo{From: from, To: to}); from != to && !slices.Contains(r.RefersTo, refers) {
			r.RefersTo = append(r.RefersTo, refers)
		}
	}
	for _, n := range *out.Rejected {
		a, err := address(cited{Span: n})
		if err != nil {
			return refuse("a rejected Span: %v", err)
		}
		if !slices.Contains(r.Rejected, a) {
			r.Rejected = append(r.Rejected, a)
		}
	}
	for _, s := range *out.Speakers {
		of, err := address(s.Span)
		if err != nil {
			return refuse("a speaker's Span: %v", err)
		}
		by, err := address(s.Speaker)
		if err != nil {
			return refuse("a speaker: %v", err)
		}
		if spoken := (Speaker{Of: of, By: by}); !slices.Contains(r.Speakers, spoken) {
			r.Speakers = append(r.Speakers, spoken)
		}
	}
	return r, nil
}
