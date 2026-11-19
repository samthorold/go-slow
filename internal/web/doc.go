// Package web is the web adapter: the hypermedia API, its handlers and
// templates, and the app's constructor, New. It holds the vendored htmx
// and its one stylesheet, static/go-slow.css: plain CSS, dark and minimal,
// linked from the layout. The stylesheet's quiet style, the class quiet, is
// for whatever should recede; its status hues (status with
// status-supported, status-refuted or status-undecided) are for
// a verdict, such as a call's outcome, as its header says.
//
// # Body and margin
//
// Body and margin is the layout the pages showing the Editor's work share
// (an Item's page, a Span's, a Story's, an Editor's Span's, the Items page
// and a conversation's).
// Each block (<li class="block"> in a <ul class="blocks">) has its text in
// .block-body and its notes in .block-margin beside it (the quote, quiet; a
// date only where it differs from the Item's; Entity pills; the machine
// label), and on a narrow window the notes drop below. The blocks sit in one
// <div class="margined" data-margin="open|folded"> holding the one fold
// control, {{template "margin-fold" "open|folded"}}: every page starts
// open. The state lives only in data-margin, toggled by
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
//     (the reason, its pills) reads plainly.
//
// The pieces live in margin.go and templates/margin.html, which page parses
// into every page.
//
// # Pages
//
// An Item's page shows its text as the numbered Spans of its newest Cut
// (Deps.Cuts, #span-N, with the machine label "cut · <cutter> v<version>"),
// as blocks read as its newest reading judged them (spanOf): each Span
// hydrated (editor.Reading.Hydrated: a stretch that refers to another shown
// as the words of what it refers to, followed as far as it goes, made
// possessive where the stretch was, in an <ins> titled with the words it
// was), its words cased as they would be written in place by inPlace
// (in_place.go: capitalised at a sentence's start where the stretch was, a
// leading "The" lowered elsewhere; Hydrated itself is left as it is); its
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
// reading's call, then the Stories holding it and the Editor's Spans citing
// it (below).
//
// The Stories page (/stories, stories.go) lists the Stories the Editor made,
// newest first, each by its Name, or until it has one by its first Editor's
// Span's opening words (shortened), as every page that names a Story the
// Editor made knows it (storyTitle), linking a Story's page, /stories/{id}
// (its event's id), headed the same way: its Editor's Spans as blocks in the
// order written, each margin saying it is the Editor's (linking the Editor's
// Span's page, /stories/{id}/spans/{n}) and holding the Spans it cites
// (words linking the Span's page, with its Item's .citation; an earlier
// Editor's Span's words linking the Story that wrote them), then its
// Timeline (the Spans it cites, each once, by publication, an earlier
// Editor's Span at its Judgement's date, said to be the Editor's and linking
// its own page), then how it was made, "woven · call N" and its Judge, and
// once named, "named · call N" and its Name's Judge. Wherever a Story is
// listed by an Editor's Span's words or its Name, the list says "by the
// Editor". An Editor's Span's page shows its words, its Story, what it
// cites, and the Editor's Spans citing it, so a wrong one's page shows its
// corrections. The Story a Cut made is reached only from its Item's page
// ("The Story its Cut made", /stories/{cut's event id}), and shows its Item
// and its Timeline, the Item's Spans. A Span's page lists the Stories holding
// it and the Editor's Spans citing it, each the Editor's. A failed naming is
// listed on the Items page by its Story's opening words, linking its page.
//
// The Topics page says Topics select nothing and links the counts on the
// Items page, then is a table of the Followed Topics (name, linking the
// Topic's page, and terms): Remove in the row's .actions, shown at rest,
// quiet, and Take up a Topic a <details>
// above it, open again when a take-up fails. A Topic's page, /topics/{id}
// (topics.go), lists the Stories the Editor made that touch it, newest
// first, as the Stories page lists them.
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
// conversation's (/conversations/{id}, the retired Sunday read's) are for
// diagnosis. Each opens with a one-line .summary-head,
// its outcome rendered by {{template "outcome"}} (templates/outcome.html,
// view model outcome from outcomeOf: applied status-supported, refused
// status-undecided, failed status-refuted, a turn that continued no hue),
// then what is diagnosed with open (a call's Input and Output; a turn's text
// and the tools it called) and the bulk collapsed in <details>
// (instructions, schema, tools, what a turn was sent, a tool call's input,
// the raw response; a call's attempts unless there are several). A call is
// linked from an Item's reading of its Spans (found by the reading's request
// hash, the storage adapter's spansReadingCall), from a Story's making and
// naming, and from failed work on the Items
// page. A conversation's turns are blocks in an open margined region, each
// <li class="block" id="turn-N"> with its anchor, outcome and tokens in its
// margin, and summed. Only the retired Sunday read held conversations, so
// none is linked from a page now: each stays at its address.
//
// # Navigation, logging and errors
//
// Every full page carries the layout's one navigation, a flat row reaching
// each index (indexes in web.go); each page template declares which index it
// is, if any (page(index, ...)), and that link is marked
// aria-current="page"; pages link at their top only to their own context (a
// call's work), never repeating the navigation. The root, /, lands on the
// Stories page (303): the Owner reads Stories.
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
