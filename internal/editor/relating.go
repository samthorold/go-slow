package editor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/samthorold/go-slow/internal/cut"
)

// RelatingVersion is the version of relating's instructions and schema. A
// new version is a new Judge (system design: Every judgement carries its
// judge, and a changed judge judges forward). Change it with
// relatingInstructions or relatingSchema.
const RelatingVersion = 1

// The kinds of relation relating judges between two Spans of different Items.
const (
	// Follows is a Span moving on what an earlier-published one said.
	Follows = "follows"
	// SameAs is two Spans saying the same thing.
	SameAs = "same_as"
	// Contradicts is two Spans that cannot both hold.
	Contradicts = "contradicts"
)

var relationKinds = []string{Follows, SameAs, Contradicts}

// ToRelate is an Item awaiting relating: read, with each of its Spans that
// its reading did not reject and the candidate Spans of other Items it is
// weighed against.
type ToRelate struct {
	Item Item
	// Reading is the Item's reading the Spans are related as: what they
	// refer to, who speaks them and what they mention.
	Reading Reading
	// ReadOrder is when the Item was first read, as the Record orders its
	// readings: its candidates come from Items read before it (system
	// design: Relating a Span judges once per pair).
	ReadOrder int64
	// Spans are the Spans to relate, in order.
	Spans []SpanToRelate
}

// SpanToRelate is one Span of an Item awaiting relating.
type SpanToRelate struct {
	// N is its number in the Cut its Item was read as.
	N    int
	Span cut.Span
	// Entities are the Entities it mentions, as the Record's look-ups have
	// them.
	Entities []NamedEntity
	// Candidates are the Spans of other Items it is weighed against, best
	// first.
	Candidates []OtherSpan
}

// OtherSpan is a Span of an Item already read, as relating is shown it.
type OtherSpan struct {
	At Address
	// N is its number in the Cut its Item was read as.
	N int
	// Item is the Item it is of; its title and date are shown with it.
	Item Item
	// Words are its hydrated reading, and Speaker whose words it is, if its
	// reading judged a speaker.
	Words, Speaker string
	// Entities are the Entities it mentions.
	Entities []NamedEntity
	// Shares are the Entities it shares with the Span it is a candidate of.
	Shares []NamedEntity
}

// Relation is a relation between two Spans of different Items, from the
// later-published to the earlier: From follows To, is the same as it, or
// contradicts it.
type Relation struct {
	Kind     string
	From, To Address
}

// Corroborates is whether r is corroboration: the same as across
// independent Sources. One publisher saying a thing twice (a speech and its
// press release) corroborates nothing (system design: Corroboration is same
// as across independent Sources). It is worked out, never judged.
func (r Relation) Corroborates() bool {
	return r.Kind == SameAs && r.From.SourceID != r.To.SourceID
}

// SpanRelation is a relation as a Span's page shows it: the relation, the
// Span at its other end, and the relating that judged it, with its Judge and
// the call that made it (zero if none was kept).
type SpanRelation struct {
	Relation   Relation
	Other      OtherSpan
	RelatingID int64
	Judge      Judge
	Call       int64
}

// Relating is relating an Item's Spans:the relations it judged, with its
// Judge.
type Relating struct {
	// ID is the relating's in the Record, once it is kept there.
	ID                int64
	SourceID, EntryID string
	// ReadingID is the reading its Spans were shown as.
	ReadingID int64
	Judge     Judge
	// At is when it was judged.
	At        time.Time
	Relations []Relation
	// Usage is what the call cost, for the logs; not kept with the relating.
	Usage Usage
}

// relatingInstructions are the relating act's own instructions.
const relatingInstructions = `You are the Editor of a private slow-news briefing. You are relating the Spans of one Item to Spans of other Items already in the Record. A Span is a stretch of an Item's text as its publisher wrote it; a stretch that refers to something in other words is shown as what it refers to. The Item's Spans are numbered S1, S2 and so on. The Spans of other Items it is weighed against, its candidates, are numbered C1, C2 and so on, under the Items they are from, each with its date. Last, each Span to relate is listed with its own candidates.

Weigh each Span against each of its own candidates, and judge the pair as one of:
- follows: one of the two moves on what the other said. The later-published takes up what the earlier said and says what has happened since, what is now being done, decided or argued about it: a speech that argues what a press release said the speaker "will argue", a decision on a proposal, a response to an announcement. The later-published Span always follows the earlier; you only say the pair is one.
- same_as: the two say the same thing, in the same or other words, whoever says it.
- contradicts: the two cannot both hold.
- none of these: give nothing for the pair. Two Spans that only share a subject, a name, a place or a day are none of these, and so are most pairs.

Give each pair at most one verdict, and relate a Span only to its own candidates, by their numbers. Keep to what the Spans say; do not add background knowledge.`

// relatingSchema is the relating act's output schema for tr, shown as the
// refs ref gives. Its refs are enumerated (the Spans that have candidates,
// and the candidates), so a model answers only in refs the checks know. Its
// fields are in name order (span, to, verdict), so the model names the pair
// before it judges it.
func relatingSchema(spans, pool []string) map[string]any {
	enum := func(values []string) map[string]any { return map[string]any{"type": "string", "enum": values} }
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"relations": array(object(map[string]any{
				"span":    enum(spans),
				"to":      enum(pool),
				"verdict": enum(relationKinds),
			})),
		},
		"required":             []string{"relations"},
		"additionalProperties": false,
	}
}

// relatingOutput is the relating act's output as the model gives it.
type relatingOutput struct {
	Relations *[]struct {
		Span    string `json:"span"`
		To      string `json:"to"`
		Verdict string `json:"verdict"`
	} `json:"relations"`
}

// shown is how an Item awaiting relating is shown: its candidates, each
// once, in the order first given, numbered from C1.
type shown struct {
	pool []OtherSpan
}

// shownAs is tr's candidates, each once, in the order first given.
func shownAs(tr ToRelate) shown {
	var s shown
	for _, sp := range tr.Spans {
		for _, c := range sp.Candidates {
			if !slices.ContainsFunc(s.pool, func(o OtherSpan) bool { return o.At == c.At }) {
				s.pool = append(s.pool, c)
			}
		}
	}
	return s
}

// ref is the ref the candidate at a is shown as, or "".
func (s shown) ref(a Address) string {
	i := slices.IndexFunc(s.pool, func(o OtherSpan) bool { return o.At == a })
	if i < 0 {
		return ""
	}
	return "C" + strconv.Itoa(i+1)
}

// hasCandidates is whether any Span of tr has a candidate: one with none is
// related with no call.
func (tr ToRelate) hasCandidates() bool {
	return slices.ContainsFunc(tr.Spans, func(s SpanToRelate) bool { return len(s.Candidates) > 0 })
}

// Relate is the act of relating an Item's Spans on the model m, which is
// asked for as the model named asked: each Span judged against each of its
// candidates, as one it follows, is the same as or contradicts, or none of
// these. An Item whose Spans have no candidates is related with no call. A
// result that fails the act's checks is ErrFailedChecks, and none of it is
// returned.
func Relate(ctx context.Context, m Model, asked string, tr ToRelate) (Relating, error) {
	if !tr.hasCandidates() {
		return unshownRelating(tr), nil
	}
	req := relatingRequest(tr)
	res, err := m.Complete(ctx, req)
	if err != nil {
		return Relating{}, fmt.Errorf("relating %s: %w", tr.Item.EntryID, err)
	}
	return checkRelating(tr, req, asked, res)
}

// unshownRelating is the relating of an Item whose Spans have no
// candidates: nothing to judge, so no call and no relations.
func unshownRelating(tr ToRelate) Relating {
	return Relating{SourceID: tr.Item.SourceID, EntryID: tr.Item.EntryID, ReadingID: tr.Reading.ID,
		Judge: Judge{Act: "relating", Version: RelatingVersion}}
}

// relatingRequest is the relating act's call on a model for an Item's
// Spans and their candidates.
func relatingRequest(tr ToRelate) Request {
	s := shownAs(tr)
	var b strings.Builder
	dated := func(it Item) {
		fmt.Fprintf(&b, "Item: %s\n", it.Title)
		if !it.Published.IsZero() {
			fmt.Fprintf(&b, "Published: %s\n", it.Published.UTC().Format(time.DateOnly))
		}
	}
	dated(tr.Item)
	b.WriteString("\n")
	var spans []string
	for _, sp := range tr.Spans {
		fmt.Fprintf(&b, "S%d%s: %s\n", sp.N, spokenBy(tr.Reading.SpeakerOf(tr.Item.Content, sp.Span)),
			tr.Reading.HydratedWords(tr.Item.Content, sp.Span))
		if len(sp.Candidates) > 0 {
			spans = append(spans, "S"+strconv.Itoa(sp.N))
		}
	}
	b.WriteString("\nSpans of other Items:\n")
	var refs []string
	for i, c := range s.pool {
		if i == 0 || c.Item.SourceID != s.pool[i-1].Item.SourceID || c.Item.EntryID != s.pool[i-1].Item.EntryID {
			b.WriteString("\n")
			dated(c.Item)
		}
		ref := "C" + strconv.Itoa(i+1)
		refs = append(refs, ref)
		fmt.Fprintf(&b, "%s%s: %s\n", ref, spokenBy(c.Speaker), c.Words)
	}
	b.WriteString("\nCandidates of each Span:\n")
	for _, sp := range tr.Spans {
		if len(sp.Candidates) == 0 {
			continue
		}
		var own []string
		for _, c := range sp.Candidates {
			own = append(own, s.ref(c.At))
		}
		fmt.Fprintf(&b, "S%d: %s\n", sp.N, strings.Join(own, ", "))
	}
	return Request{Act: "relating", Instructions: relatingInstructions, Input: b.String(), Schema: relatingSchema(spans, refs)}
}

// spokenBy is how a Span's speaker is shown beside its number.
func spokenBy(who string) string {
	if who == "" {
		return ""
	}
	return " (spoken by " + who + ")"
}

// HydratedWords is the hydrated reading of the Span s of text by r, as one
// line of words.
func (r Reading) HydratedWords(text string, s cut.Span) string {
	var b strings.Builder
	for _, p := range r.Hydrated(text, s) {
		b.WriteString(p.Words)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// SpeakerOf is whose words the Span s of text is, by r: the words of the
// first speaker judged of a Span within it, or "".
func (r Reading) SpeakerOf(text string, s cut.Span) string {
	for _, sp := range r.Speakers {
		if inside(sp.Of.Span, s) {
			return r.Words(text, sp.By)
		}
	}
	return ""
}

// checkRelating decodes the model's answer to req, asked of the model named
// asked, on tr, and runs the act's checks over it. A result that fails them
// is ErrFailedChecks.
func checkRelating(tr ToRelate, req Request, asked string, res Response) (Relating, error) {
	it := tr.Item
	refuse := func(format string, args ...any) (Relating, error) {
		return Relating{}, fmt.Errorf("relating %s: %w: %s", it.EntryID, ErrFailedChecks, fmt.Sprintf(format, args...))
	}
	var out relatingOutput
	dec := json.NewDecoder(bytes.NewReader(res.Output))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return refuse("output does not match the schema: %v", err)
	}
	if out.Relations == nil {
		return refuse("output does not match the schema: no relations")
	}
	s := shownAs(tr)
	r := unshownRelating(tr)
	r.Usage = res.Usage
	r.Judge.Model, r.Judge.RequestHash = res.Model, RequestHash(req, asked)
	verdicts := map[[2]Address]string{}
	for _, rel := range *out.Relations {
		i := slices.IndexFunc(tr.Spans, func(sp SpanToRelate) bool { return "S"+strconv.Itoa(sp.N) == rel.Span })
		if i < 0 || len(tr.Spans[i].Candidates) == 0 {
			return refuse("%s is not a Span of the Item shown with candidates", rel.Span)
		}
		sp := tr.Spans[i]
		if strings.HasPrefix(rel.To, "S") {
			return refuse("%s is related to %s, a Span of the same Item", rel.Span, rel.To)
		}
		j := slices.IndexFunc(sp.Candidates, func(c OtherSpan) bool { return s.ref(c.At) == rel.To })
		if j < 0 {
			return refuse("%s is related to %s, which was not shown as one of its candidates", rel.Span, rel.To)
		}
		c := sp.Candidates[j]
		if c.At.SourceID == it.SourceID && c.At.EntryID == it.EntryID {
			return refuse("%s is related to %s, a Span of the same Item", rel.Span, rel.To)
		}
		if !slices.Contains(relationKinds, rel.Verdict) {
			return refuse("%s and %s are judged %q, which is no relation", rel.Span, rel.To, rel.Verdict)
		}
		here := Address{SourceID: it.SourceID, EntryID: it.EntryID, Span: sp.Span}
		pair := [2]Address{here, c.At}
		if was, ok := verdicts[pair]; ok {
			if was != rel.Verdict {
				return refuse("%s and %s are judged both %s and %s", rel.Span, rel.To, was, rel.Verdict)
			}
			continue
		}
		verdicts[pair] = rel.Verdict
		// From the later-published to the earlier; between two published
		// at once, from the Span being related, which was read second.
		from, to := here, c.At
		if c.Item.Published.After(it.Published) {
			from, to = c.At, here
		}
		r.Relations = append(r.Relations, Relation{Kind: rel.Verdict, From: from, To: to})
	}
	return r, nil
}
