// Package web is the web adapter: the hypermedia API, its handlers and
// templates, and the app's constructor, New. It holds the vendored htmx
// and its one stylesheet, static/go-slow.css: plain CSS, dark and minimal,
// linked from the layout. The stylesheet's quiet style, the class quiet, is
// for whatever should recede; its status hues (status with
// status-supported, status-refuted, status-undecided or status-due) are for
// a verdict, the register's statuses and the same family for a call's
// outcomes, as its header says.
//
// # Body and margin
//
// Body and margin is the layout every page showing the Editor's work shares
// (an Item's page, a Span's, the Briefing, the Sunday read and a
// conversation's so far; each other page adopts it in its own change).
// Each block (<li class="block"> in a <ul class="blocks">) has its text in
// .block-body and its notes in .block-margin beside it (the quote, quiet; a
// date only where it differs from the Item's; Entity pills; the machine
// label; on every page but the Sunday read, the block's Feedback), and on a
// narrow window the notes drop below. The blocks sit in one
// <div class="margined" data-margin="open|folded"> holding the one fold
// control, {{template "margin-fold" "open|folded"}}: every page starts open
// (an Item, a Span, the Briefing) except the reading page, the Sunday read,
// which starts folded. The state lives only in data-margin, toggled by
// static/margin-fold.js (plain JS, loaded by the layout, a click listener on
// document and its button's aria-expanded synced on htmx:load), so nothing
// is remembered across pages.
//
// Its visual vocabulary, one meaning per shape, is in classes documented in
// the stylesheet beside quiet:
//   - an Entity is a .pill (its name, then its kind, or a person's office
//     where it is read so, in a .pill-tag), in one hue, --entity, teal,
//     apart from the link accent and every status hue (view model pill,
//     built by pillOf, rendered by {{template "pills"}}/{{template "pill"}});
//   - an Item is a .citation: its Source's short name, who is speaking ("No
//     10", never "GOV.UK"), looked up from the Sources list the adapter is
//     given (Deps.Sources), then its title in <cite>, a link (view model
//     itemCitation, built by app.cite, {{template "citation"}}); no act is
//     shown the short name;
//   - how something was made is a .machine label worded "<act> · call N"
//     (view model made, {{template "made"}});
//   - what is set aside (a Span Rejected) stays, its .block-body .struck
//     (struck through in the quiet colour), never the block, so its margin
//     (the reason, its pills, its Feedback) reads plainly.
//
// The pieces live in margin.go and templates/margin.html, which page parses
// into every page.
//
// # Feedback controls
//
// Every Feedback control is a small icon button shown at rest, never
// revealed on hover or focus, so nothing moves as the pointer does: one icon
// per kind, the same everywhere (wrong ✗, new to me ✦, unwanted ⊘, missed
// ⊕; plain Unicode, kindIcons in feedback.go), its full label (what it gives
// on what, "Wrong: “the city” refers to “Derby”") its aria-label and title,
// quiet at rest and the link colour on hover and focus (button.give,
// template give). Each sits beside what it judges: wrong on a look-up beside
// its Entity pill (pill.Gives, in a .judged), on a speaker beside its
// "spoken by" line, on a refers to beside a margin line naming it ("the city
// refers to Derby", spanRow.RefersTo: the body is collected text under
// hx-disable, where a control would do nothing, and an icon in the text
// would break its flow); missed once per Span (beside its number on an
// Item's page); unwanted beside each Span a Briefing statement cites; and a
// block's own (a statement's, an Assessment's, a Story's) together in its
// .actions (template block-gives). A line holding a control is a div, never
// a p: a form's start tag closes an open paragraph, which puts the control
// on a line of its own. Feedback already given shows in its control's place,
// its icon filled in its kind's hue (.given-wrong…), its label read out
// (.visually-hidden) and its title, beside its Undo; where Feedback of a
// kind already stands on a target, its button renders as given (givesOn).
//
// Feedback is given inline from the page showing each target
// (POST /items/{source}/{entry}/feedback,
// POST /items/{source}/{entry}/spans/{span}/feedback,
// POST /briefings/{day}/feedback, POST /sunday-reads/{day}/feedback), each
// answering 303 back to its page or 422 with the page re-rendered saying
// why. An Item's page and a Span's list the Feedback on their Spans and
// judgements, and a Briefing's and a Sunday read's the Feedback on them, to
// undo (template given), since what wrong took out shows nowhere else. It is
// undone from each page that lists it (PUT {page}/feedback/{id}/undone on
// the Briefing, the Sunday read, an Item's page, a Span's page, and
// PUT /feedback/{id}/undone), answering 303 back. GET /feedback lists every
// piece as given, a table newest first: When, with the Briefing it is about;
// Kind, a filled label in its own hue from the stylesheet's kind hues, the
// table's one hued cell; About, linked to its page with what it is as a
// quiet prefix (a Sunday read's by the Assessment's statement or "the
// Sunday read", linking the read's conversation); How it was made, a machine
// label per call behind it, or "plain code" for a look-up; Undo in the row's
// actions, an undone row struck through and quiet.
//
// # Pages
//
// An Item's page shows its text as the numbered Spans of its newest Cut
// (Deps.Cuts, #span-N, with the machine label "cut · <cutter> v<version>"),
// as blocks read as its newest reading judged them (spanOf): each Span
// hydrated (editor.Reading.Hydrated: a stretch that refers to another shown
// as the words of what it refers to, followed as far as it goes, in an <ins>
// titled with the words it was), its words read as they would be written in
// place by inPlace (in_place.go: capitalised at a sentence's start where the
// stretch was, a leading "The" lowered elsewhere, made possessive where the
// stretch was; Hydrated itself, relating's input, is left as it is); its
// margin whose words it is ("spoken by …", the speaker's Span hydrated too),
// the Entities it mentions (editor.Reading.Entities, pills built by pillOf:
// the Entity's name and kind, linked to its Wikidata item if it is one; a
// name not yet looked up shows as read, the office for a person or else the
// kind) and, for a Rejected Span (.struck), "rejected". The reading's call
// is the machine label "read · call N", and an Item cut but not read says it
// awaits reading. Each Span's number in its margin links the Span's own
// page.
//
// A Span's page, /items/{source}/{entry}/spans/{start}-{end} (span.go,
// templates/span.html), is a bookmarkable address for any stretch within
// the Item's text, listed by a Cut or not: the Span as its Item's newest
// reading reads it, with its Item's citation and date, its Entities and the
// reading's call, then its latest gathering (the pass that gathered it: the
// Briefing, with "gathered · call N", or the Sunday read, with "gathered ·
// conversation", linking it; the Spans it holds) and the relations judged
// since, each marked "not yet gathered" (with no gathering, every relation
// is), both ways (follows, followed by, same as, contradicts, from
// Deps.Relations), each linking the Span at its other end, citing its Item,
// and the call that made it ("related · call N").
//
// The Briefing looks as a Span's page does, margin open. It reads as its
// statements, each a block whose margin holds each Span it cites (its words
// linking the Span's page, its speaker, its Item's .citation), unwanted
// beside each Span it cites (labelled by its words, shortened), then its
// Feedback .actions (wrong and new to me on the statement), folding away
// with the margin; statements Feedback took out are left out, and below
// them is the Feedback given on the Briefing, to undo. Of one written before
// the Record kept only events only its day is kept
// (Briefings.HasBriefing), and its page says what it said went, with no
// trail. While a Briefing is being written, the day's page polls
// GET /briefings/{day}/status, a fragment that re-polls only while it is.
// A written Briefing links to its trail, "How this was written"
// (GET /briefings/{day}/trail): its totals summed over the distinct calls,
// then a table (Span, Item, Read, Related) whose first row is the call that
// wrote it, then a row for each Span it cites as written (the storage
// adapter's BriefingTrail: the call that made the reading it was shown, "not
// yet read" for an Item only cut, and the relating calls on its Item before
// the Briefing), each call a machine label with its tokens ("read · call N ·
// 2,140 tok", made with Counted); it records no open. A day with none
// written links each failed attempt; a Briefing written empty says so.
//
// The Sunday read (/sunday-reads/{day}, linked from Sunday's Briefing), the
// one reading page, is body and margin folded at rest. Its title carries
// its New to me, beside it in its .title-row; its statements are blocks,
// each margin holding what it cites (readCite: a Span, its words linking the
// Span's page, "spoken by" its speaker and its Item's .citation; an
// Assessment, linking it; a Wikidata lookup, linking the revision seen),
// statements citing an Assessment wrong withdrew left out. Its Assessments
// are blocks under their own heading, set apart by a left rule (.assessment;
// withdrawn, struck through and quiet), their margin the Spans and earlier
// Assessments each rests on, what it supersedes and the machine label
// "reached · turn N" (view model reached, linking the turn). On it Feedback
// is the frequent act, so an Assessment's Feedback controls (.actions:
// Wrong and New to me) sit in its .block-body, not its margin, which folds
// away. Below them is the Feedback given on the read and its Assessments, to
// undo. Of a read written before the Record kept only events only its day is
// kept (SundayReads.HasSundayRead), and its page says so. An Assessment's
// stable address is /assessments/{id}, its event id.
//
// The Topics page says Topics select nothing and links the counts on the
// Items page, then is a table of the Followed Topics (name and terms):
// Remove in the row's .actions, shown at rest, quiet (as Undo on the
// Feedback page is), and Take up a Topic a <details> above it, open again
// when a take-up fails.
//
// The Items page leads with exceptions only when there are any (quiet
// Sources, and failed work with its retry command, status-refuted for
// failed), otherwise one quiet line over the folded Sources, each with what
// the Owner steers by, a .machine label "Stories: N touching a Topic, N not
// · Spans: N of N rejected" (the storage adapter's SourceCounts over its
// steering window, stated on the page). Its collected Items are blocks,
// margin open, each Item's status a .machine label (itemRow.Status: the
// pipeline's own wording, not prose: "read · N names, N speakers, N
// rejected", "awaiting reading", "awaiting cutting" or "no text yet"), and
// Upcoming folds at its foot.
//
// A call's page (GET /calls/{id}, with every attempt at its work) and a
// conversation's (/conversations/{id}, the Sunday read's trail) are for
// diagnosis, and record no open. Each opens with a one-line .summary-head,
// its outcome rendered by {{template "outcome"}} (templates/outcome.html,
// view model outcome from outcomeOf: applied status-supported, refused
// status-undecided, failed status-refuted, a turn that continued no hue),
// then what is diagnosed with open (a call's Input and Output; a turn's text
// and the tools it called) and the bulk collapsed in <details>
// (instructions, schema, tools, what a turn was sent, a tool call's input,
// the raw response; a call's attempts unless there are several). A call is
// linked from an Item's reading of its Spans (found by the reading's request
// hash, the storage adapter's spansReadingCall), from each relation on a
// Span's page (by its relating's request hash, relatingCall) and from failed
// work on the Items
// page. A conversation's turns are blocks in an open margined region, each
// <li class="block" id="turn-N"> with its anchor, outcome and tokens in its
// margin, and summed; it is linked from the read, its failure and each
// Assessment (the submitting turn).
//
// # Navigation, logging and errors
//
// Every full page carries the layout's one navigation, a flat row reaching
// each index (indexes in web.go); each page template declares which index it
// is, if any (page(index, ...)), and that link is marked
// aria-current="page"; pages link at their top only to their own context (a
// trail's Briefing, a call's work), never repeating the navigation.
//
// The adapter logs each request it answers as one line (request_log.go:
// method, path without its query, status, duration_ms; never form values),
// through the Log it is given; a 5xx is logged at error level with the
// error serverError noted on the request, and static files at debug level,
// so a page load is one info line. A request htmx doesn't swap (any 4xx but
// 422, any 5xx, or no answer) is shown in the layout's #request-errors
// region by static/request-errors.js, plain JS with no build step, and
// cleared by the next request that succeeds.
package web
