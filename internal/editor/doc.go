// Package editor is the Editor: the editorial acts done on the Record. An act
// that needs judgement calls a model through the port here; the act owns its
// instructions and output schema, and checks the result before anything is
// written back (system design: The Editor's work is a set of Commands; The
// model is an adapter).
//
// The ports are provider-neutral: Model (one call, at once) and Batcher (a
// provider's batch interface); a conversation's turn is a Request like any
// other, carrying its tools and the conversation so far, and its Response
// the tools the model called. Each act has its own instructions, output
// schema and checks.
//
// # Hydrating an Item
//
// Hydration (hydrate.go, HydrationVersion 3) is shown the Item as its newest
// Cut, numbered S1, S2…, and one call judges what its Spans refer to, what
// they name (a name, its kind, and for a person the office in full; code
// drops an office given to anything else), who speaks them and which are
// Rejected. Each stretch is cited as a Span's number and words quoted within
// it, which code turns into an Address (cut.Within), refusing a Span not in
// the Cut or words not in their Span as whole words, naming both. Each side
// of a refers to also gives words around it that locate it (within, empty
// where the stretch's words occur once in the Span), found in the Span and
// the stretch within them; only the stretch's address is kept, and locating
// words not in the Span or not holding the stretch are refused, naming both.
// The Hydration carries its Judge (act, HydrationVersion, which covers how code
// addresses the words as well as the instructions and schema, the model
// reported, the request's hash with the model asked for). Every Item
// collected after the switch (sqlite.Store.Switch), cut and not yet hydrated is
// hydrated, with no Topic check. A hydration that fails its checks writes nothing
// and is tried again. Hydration refuses an answer in the old design's schema.
//
// # Looking up names
//
// Lookups.LookUpWaiting (lookup.go), run by Collection after hydration
// (collect.Deps.LookUp), looks up in Wikidata, in plain code, each LookupKey
// no look-up has looked up (the storage adapter's NamesToLookUp, over every
// hydration's names, a candidate's included: a look-up is keyed on the name
// and shows nothing, so a chosen candidate's names are already looked up),
// once. A key is the name and kind, and for a person named with an office,
// the office and the Item's date in UTC (KeyOf), so "Derby" in any number of
// Items is one look-up. LookUp matches by match (an office's holders on the
// date first, else the best ranked item of the right kind whose label or
// alias is the name) and returns a NameLookup with its judge (Looker,
// wikidata v1). A look-up Wikidata refuses, after the adapter's own backoff,
// is logged and kept nowhere, so the next run tries it again; it holds up no
// hydration, which is kept already. KeepLookup keeps it as a lookup event,
// with what it makes in one transaction: with no match, the Editor's own
// Entity for its name and kind if there is none (an entity event); with a
// match, a merge event for each unmerged Editor's Entity of its kind made
// for the key's name or the item's label. Which Entities a Span mentions is
// a view, never stored: Hydration.Mentions (the names inside it, or reached by
// following refers to from a stretch inside it), and Hydration.Entities over
// the storage adapter's EntitiesNamed (each key's newest look-up: its item,
// or the Editor's Entity, or the item that was merged into), each a
// NamedEntity (equal exactly when the same Entity).
//
// # Batches
//
// The acts that call a model run in batches through Batches: an act is a BatchAct (what
// awaits it, each piece of work with its request and how its result is
// checked and written), so weaving (Editor.Weaving) and naming
// (Editor.Naming) plug in beside Editor.Hydration. Work that needs no call
// (Job.Request empty) is applied at once. One batch carries every
// act's requests, and the Claude adapter sends each to its act's model.
// After each Collection run, Batches applies the results of batches that
// have ended (matched by request ID), resubmits each failed request alone,
// and submits what awaits as one batch; pending batches are kept in the
// Record. Each piece of work gets MaxAttempts attempts, and a batch not
// ended or not pollable by BatchDeadline counts one for each of its pieces;
// the attempts are kept with each batch's end (work_attempts).
//
// A result that fails to apply for a reason other than the act's checks (its
// Apply errs, not with ErrFailedChecks) is no attempt and stops only itself:
// the batch's other results are applied, and the batch is kept (same ID and
// submission time) holding only the requests still to apply, so their work
// stays in a batch and isn't submitted again, and each run applies them
// again from the kept results until they apply; one still failing at
// BatchDeadline counts an attempt, as a lost batch does. A result for work
// whose request has changed since it was sent (RequestHash of the request
// kept with the batch against the one the work builds now) is refused,
// counting an attempt, and the work asked again as it stands. Work marked
// failed is left out of what awaits its act by the storage adapter, listed
// on the Items page, and taken up again only by the retry admin command.
// Editor.RunWaiting is the synchronous path, for when no batch interface is
// available.
//
// # Weaving
//
// Weaving (weaving.go, weaving_asks.go, WeavingVersion; system design:
// Weaving asks for an editor's opinion, and need get none) is a BatchAct
// whose work is one run a day, keyed by the day (time.DateOnly in now's
// location): its Waiting, told now, gives a job only at or after the time of
// day it was built with (DueAt), when that day's run has been neither
// written nor marked failed (WeavingStore.WeavingRan) and something has been
// cut since the last run written; while a run is under way (its turn in a
// pending batch, or its conversation kept between turns,
// WeavingStore.WeavingInProgress), that run's next turn alone.
// Editor.WeaveToday is the same day's first turn asked for by the Owner at
// any time of day, refused where Waiting would give none (ErrWovenToday,
// ErrWeavingPending, ErrNothingToWeave), and Batches.Submit submits it alone,
// pending, for the batch runner to apply as it applies its own; it is priced
// by EstimateOf at batch rates (Price.AtBatchRates).
//
// A run is a conversation, each turn a request at EffortMedium carrying the
// tools find and open_story (weavingTools) and the conversation so far
// (Request.Messages; system design: A weaving run is shown what is new, and
// finds for itself what bears on it). Its first turn is shown only every
// Story cut since the last run written (the storage adapter's
// StoriesToWeave: Cuts after its through_cut, of Items collected after the
// switch, and since the switch to Stories, the newest story_switch), each
// under its Item's title and date, its Spans numbered S1, S2… (weavingInput,
// woven). A Span is shown hydrated once its Item has a hydration
// (WeavingStore.NewestHydration; system design: Construction judges a Span
// from the Story its Cut made): "spoken by" its Speaker, and each stretch
// that refers to another read between ⟦ and ⟧, marks no publisher writes, as
// what it refers to, its possessive where the stretch says whose something
// is ("⟦Derby’s⟧ role", Hydration.Hydrated, as the Pages read it), while a
// publisher's own square brackets are shown as written; a Span of a Story
// cut that is Rejected is left out and keeps its number, so a Span's number
// never depends on its hydration. A Span with no hydration is shown as
// written: nothing waits for hydration.
//
// A turn that calls tools is answered, when its result is applied, from the
// Record as it stands (earlierRecord: every Story cut through the run's
// newest new one but its new ones, WeavingStore.StoriesCutThrough, and every
// Story the Editor made, WeavingStore.StoriesMade, each Span read as hydrated
// now): find matches words case-insensitively as whole words in each Span
// as hydrated (marks dropped; a Rejected Span matches nothing) and in the
// Editor's Spans, and answers with a catalogue, newest first, at most
// catalogueSize Stories: each by its number (its event), a Cut's by its
// Item's title, date and Source, the Editor's by its date and first Editor's
// Span, with how many later Editor's Spans cite it, then its first
// matchesShown matched Spans, each numbered on from what the run has been
// shown, between the Spans around it unnumbered (·); open_story shows an
// earlier Story whole, a Cut's each Span numbered (a Rejected one left out),
// the Editor's each Editor's Span after the Spans it cites, read hydrated
// whether Rejected or not, marked "the Editor's". A Span keeps its number
// however often it is shown. The turn is kept as the run's conversation
// (WeavingTurns: the opening as sent, the messages since, every Span shown
// by number and those left out; WeavingStore.KeepWeavingTurns), its call
// kept Continued (ErrContinued), and the next turn is the run's work from
// then on, submitted on the same turn of the batch runner. A run the Owner
// capped (WeavingStore.WeavingCap, kept by the weave command) is given up
// (ErrGiveUp), marked failed, when its next turn would take what it has
// spent over its cap, priced by Deps.PriceOf at batch rates.
//
// A turn that calls no tool is the answer: stories, each editor_spans, each
// body then cites (numbers); a citation of an earlier Editor's Span is held
// by its address (Cited.Of, an EditorSpanAt), never copied. The checks
// refuse a citation of a Span not shown (left out included), wherever shown
// (as new, in a catalogue or an opened Story), an Editor's Span with no
// words or no citation, and a Story with none; an empty stories is a run
// that made nothing, kept all the same. A result is applied through
// Job.ApplySent to the request as sent, never refused as stale: on the
// first turn, the Stories it showed are the first of those cut now
// awaiting, found by their headings in the request as sent, and the Spans
// left out are read from it too (shownIn), since a Span hydrated after the
// request went is shown otherwise now; on a later turn, to the conversation
// kept, which it must answer. KeepWeaving keeps the run (its Judge, the final
// turn's request hash, and the newest Cut shown as new) and each Story as
// its own event. A run failing MaxAttempts (over its turns) is listed on the
// Items page by its day, and its new Stories are shown to the next day's
// run; the retry admin command takes it up again from its last kept turn.
//
// # Naming
//
// Naming (naming.go, NamingVersion; system design: Every Story the Editor
// makes has a Name, and nothing reads it but the Owner) is a BatchAct whose
// work is each Story the Editor made with no Name (NamingStore.StoriesToName),
// keyed by the Story's event. Its request, at EffortLow, shows the Story's
// Editor's Spans numbered E1, E2…, each followed by the Spans it cites under
// their Items' titles (an earlier Editor's Span marked as the Editor's), and
// answers name. The checks refuse a name that is
// empty, runs to more than one line or has more than MaxNameWords words.
// A Story never changes once made, so a result is applied through
// Job.Apply as any other. KeepName keeps it as a Judgement of its own;
// Story.Name is the newest. No act is shown a Name: weaving's input shows
// no Name, its catalogues and opened Stories the Editor's by their date and
// Spans alone, and naming's a Story's Editor's Spans and what they cite.
// Naming follows weaving in the batch runner's acts, so a run's Stories are
// named in the batch submitted on the turn its result is applied.
//
// # Kept calls
//
// Every model call hydration, weaving and naming make is kept
// (calls.go), in a batch and through the synchronous paths alike
// (Editor.callNow): a Call holds the act and work, the request as sent (a
// batch's request is kept when it is submitted, with the pending batch), the
// model asked for (Deps.ModelFor/BatchDeps.ModelFor, the adapter's
// claude.Model.ModelFor), the response as returned (Response.Raw, the
// provider's own form, beside its reported model, usage and stop reason; nil
// when none came), the outcome (applied, refused by the checks, or failed)
// with its reason, and RequestHash, the one hash of a request (instructions,
// schema, input, model, effort, and a conversation's tools and messages)
// that recorded runs share. A conversation's call keeps its whole
// conversation in its request, and the tools its response called; a turn
// that asked is kept Continued. Applied calls go through
// KeepCall, failed attempts with their WorkFailure in EndBatch. The storage
// adapter keeps each distinct text once (call_texts) and refuses to rewrite
// or remove a kept call. The calls the old designs' acts made (placing,
// connecting, the earlier reading act that drew Facts; relating, the Briefing, the
// Sunday read's conversations, KeptConversation, whose shapes weaving's
// conversation reuses: Tool, Message, Turn, ToolCall, ToolResult) stay kept, as every call
// does, and readable on their pages; no act is shown them. Call.Precedents
// is the Feedback a retired act's call showed as precedent, read from the
// Record; no act shows any now, so nothing that makes a call carries them.
//
// # Re-judging
//
// Rejudge (rejudge.go, system design: Every judgement carries its judge, and
// a changed judge judges forward) judges a range of the Record again with
// one judge, for the rejudge admin command: an act plugs in as a RejudgeAct
// (its name, Version, and Work over a Range, each RejudgeJob a request and
// an Apply that keeps its result as the live judge's or as a Candidate's),
// hydration as Rehydrating. Rejudge.Price is the dry run, calling no model:
// input tokens from each request's size at the tokens per byte of the act's
// past applied calls, output at their mean per call (or 3 bytes a token and
// 4000 out with none), in dollars from the adapter's PriceOf at real-time
// rates. Rejudge.Run prices first, refuses an estimate over MaxUSD
// (ErrOverCap), then calls in real time, a piece at a time with MaxAttempts
// attempts each, keeping every call, and stops before an attempt whose
// estimate would take the spent over MaxUSD. CompareHydrations counts, per
// kind of judgement, what a live and a candidate hydration each made and both
// made.
//
// The old designs (Facts and Claims drawn from each Item, Stories,
// Developments and Connections; then relating, the Briefing, the Sunday read,
// the gatherings they made and the Feedback given on them) are gone from the
// code. The first's judgements were dropped from the Record (the storage
// adapter's 0032_events_only.sql); the second's stay in it, read by no
// projection (system design: The Record is recreated when the design of its
// Judgements changes, not translated).
package editor
