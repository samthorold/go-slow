// Package editor is the Editor: the editorial acts done on the Record. An act
// that needs judgement calls a model through the port here; the act owns its
// instructions and output schema, and checks the result before anything is
// written back (system design: The Editor's daily work is a sequence of
// editorial acts; The model is an adapter).
//
// The ports are provider-neutral: Model (one call, at once), Batcher (a
// provider's batch interface) and Conversation (one turn at a time, with
// tool definitions, calls and results, the provider's own form of each turn
// passed back opaque as Turn.Raw). Each act has its own instructions, output
// schema and checks.
//
// # Reading an Item
//
// Reading (reading.go, ReadingVersion 3) is shown the Item as its newest
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
// The Reading carries its Judge (act, ReadingVersion, which covers how code
// addresses the words as well as the instructions and schema, the model
// reported, the request's hash with the model asked for). Every Item
// collected after the switch (sqlite.Store.Switch), cut and not yet read is
// read, with no Topic check. A reading that fails its checks writes nothing
// and is tried again. Reading refuses an answer in the old design's schema.
//
// # Looking up names
//
// Lookups.LookUpWaiting (lookup.go), run by Collection after reading
// (collect.Deps.LookUp), looks up in Wikidata, in plain code, each LookupKey
// no look-up has looked up (the storage adapter's NamesToLookUp, over every
// reading's names, a candidate's included: a look-up is keyed on the name
// and shows nothing, so a chosen candidate's names are already looked up),
// once. A key is the name and kind, and for a person named with an office,
// the office and the Item's date in UTC (KeyOf), so "Derby" in any number of
// Items is one look-up. LookUp matches by match (an office's holders on the
// date first, else the best ranked item of the right kind whose label or
// alias is the name) and returns a NameLookup with its judge (Looker,
// wikidata v1). A look-up Wikidata refuses, after the adapter's own backoff,
// is logged and kept nowhere, so the next run tries it again; it holds up no
// reading, which is kept already. KeepLookup keeps it as a lookup event,
// with what it makes in one transaction: with no match, the Editor's own
// Entity for its name and kind if there is none (an entity event); with a
// match, a merge event for each unmerged Editor's Entity of its kind made
// for the key's name or the item's label. Which Entities a Span mentions is
// a view, never stored: Reading.Mentions (the names inside it, or reached by
// following refers to from a stretch inside it), and Reading.Entities over
// the storage adapter's EntitiesNamed (each key's newest look-up: its item,
// or the Editor's Entity, or the item that was merged into), each a
// NamedEntity (equal exactly when the same Entity).
//
// # Relating a Span
//
// Relating (relating.go): once an Item is read and what its reading names
// looked up, its Spans not Rejected are shown, hydrated and with their
// speakers, beside its candidates: Spans not Rejected of Items first read
// before it (ToRelate.ReadOrder, the Item's first reading) that mention an
// Entity one of its Spans mentions (relate_waiting.go, candidatesOf: per
// Span ranked by the Entities each shares, each weighted by one over how
// many Items read by then mention it, then the most recent, at most
// candidatesPerSpan per Span and candidatesPerItem for the Item, not
// narrowed by recency), pooled and numbered C1, C2… under their Items, every
// Span weighed against the whole pool. One call judges each pair follows,
// same_as or contradicts, or none, refusing a relation to anything not shown
// (a Span of the same Item included) or two verdicts on one pair. Code turns
// each into a Relation from the later-published Span to the earlier (follows
// from the one that moves on what the other said); Relation.Corroborates
// (same as across different Sources) is worked out, never judged. An Item
// none of whose Spans finds a candidate (the first read, for one) is related
// with no call. Each pair is judged once, by the Item read second: of two
// read in one batch, the later-published, since a batch's results are
// applied in the order its requests were submitted (PendingBatches, by
// rowid), reading's oldest Item first.
//
// # Batches
//
// The daily acts run in batches through Batches: an act is a BatchAct (what
// awaits it, each piece of work with its request and how its result is
// checked and written), so relating (Editor.Relating) plugs in beside
// Editor.Reading. Work that needs no call (Job.Request empty: an Item
// related with no candidates) is applied at once. One batch carries every
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
// failed is left out of ItemsToRead and ItemsToRelate by the storage
// adapter, listed on the Items page, and taken up again only by the retry
// admin command. Editor.ReadWaiting and Editor.RelateWaiting are the
// synchronous paths, for when no batch interface is available.
//
// # Precedents
//
// Relating and the Briefing are shown precedents (precedent.go, system
// design: Feedback becomes precedent, shown as cases); reading is shown none
// (its recorded case was refuted; the design says why). Relating is shown
// the wrong standing on relations whose two Spans mention an Entity its
// Spans mention (relatingPrecedents), those sharing the most first, then the
// newest, since wrong holds at any age; the Briefing the unwanted and missed
// standing, newest first (briefingPrecedents), so the oldest drop out first;
// each at most precedentsShown. The storage adapter gives them
// (StandingWrongOnRelations, StandingTaste), never undone ones. They follow
// the act's input as one JSON object per line under one heading
// (withPrecedents), each saying what its kind of Feedback means (said), and
// nothing is added when there are none, so a case with none sends exactly
// the request it would without them. Their ids are kept with the call
// (Call.Precedents, shown on the call's page) and with the judgement
// (Relating.Precedents, Briefing.Precedents; table judgement_precedents).
// Re-judging is not shown precedents yet.
//
// # Kept calls
//
// Every model call reading, relating and writing the Briefing make is kept
// (calls.go), in a batch and through the synchronous paths alike
// (Editor.callNow): a Call holds the act and work, the request as sent (a
// batch's request is kept when it is submitted, with the pending batch), the
// model asked for (Deps.ModelFor/BatchDeps.ModelFor, the adapter's
// claude.Model.ModelFor), the response as returned (Response.Raw, the
// provider's own form, beside its reported model, usage and stop reason; nil
// when none came), the outcome (applied, refused by the checks, or failed)
// with its reason, and RequestHash, the one hash of a request (instructions,
// schema, input, model) that recorded runs share. Applied calls go through
// KeepCall, failed attempts with their WorkFailure in EndBatch. The storage
// adapter keeps each distinct text once (call_texts) and refuses to rewrite
// or remove a kept call. The calls the old design's acts made (placing,
// connecting, the reading that drew Facts) stay kept, as every call does;
// no act is shown them.
//
// # Re-judging
//
// Rejudge (rejudge.go, system design: Every judgement carries its judge, and
// a changed judge judges forward) judges a range of the Record again with
// one judge, for the rejudge admin command: an act plugs in as a RejudgeAct
// (its name, Version, and Work over a Range, each RejudgeJob a request and
// an Apply that keeps its result as the live judge's or as a Candidate's),
// reading as Rereading. Rejudge.Price is the dry run, calling no model:
// input tokens from each request's size at the tokens per byte of the act's
// past applied calls, output at their mean per call (or 3 bytes a token and
// 4000 out with none), in dollars from the adapter's PriceOf at real-time
// rates. Rejudge.Run prices first, refuses an estimate over MaxUSD
// (ErrOverCap), then calls in real time, a piece at a time with MaxAttempts
// attempts each, keeping every call, and stops before an attempt whose
// estimate would take the spent over MaxUSD. CompareReadings counts, per
// kind of judgement, what a live and a candidate reading each made and both
// made.
//
// # The Briefing
//
// The Briefing is written in real time (briefing.go, schedule.go): Schedule
// is its own clock in the app process, writing the day's Briefing at
// BRIEFING_TIME through Model.Complete, not the batch runner, over what the
// Record holds then, never waiting for reading or relating (system design:
// The Briefing doesn't wait for construction); main starts it only once the
// start's first Collection run has ended. It covers the Items collected
// after the switch and first cut since the last Briefing was written (the
// storage adapter's SpansToTell, by the Cut event's time), each with its
// Spans not Rejected by its newest reading, hydrated and with their speakers
// (an Item only cut: every Span of its newest Cut, as written, marked "(not
// yet read)"), and each Span's relations either way so far; with none, it is
// written empty, with no call.
//
// The act (WriteBriefing, BriefingVersion; there is no Topic or Depth in it)
// shows today's Spans numbered S1, S2… under their Items, each relation
// beneath its Span ("follows E1", "same as S7, corroborated"), and the Spans
// of earlier Items they reach as E1, E2…. Its answer is statements, each
// spans (the refs whose words it tells), then speakers (who speaks them, as
// shown; the model's own note, not kept, written so the text names them),
// then stories (beside it, the Story of each of today's Spans it tells: refs
// its relations reach, empty for none), then text. Gathering beside the
// statement, rather than in a list of its own, and noting the speakers
// before the text are what the recordings needed: a separate list left told
// Spans ungathered, and without the note the text named speakers otherwise
// than shown, or told a spokesperson's line in its own voice. The checks
// refuse the whole Briefing for a statement citing nothing or a ref not
// shown, a Story of a Span its statement doesn't tell, not one of today's or
// holding a Span its relations don't reach, or a Span told with relations
// whose Story is not gathered; a Span gathered beside two statements keeps
// the first Story, and a Span told with no relations gets an empty Story
// with no judgement asked. It is kept (KeepBriefing) as events: the Briefing
// with its Judge, each statement with the Spans it cites and the reading
// each was shown as, and each Story gathered. The Briefing's calls are keyed
// by its day (act briefing), each attempt kept, refused or failed ones
// before the failure; a Briefing written empty makes and keeps no call. A
// failure is kept, shown on the day's page, and retried after RetryBriefing.
//
// # The Sunday read
//
// The Sunday read (sunday.go, sunday_tools.go, SundayReadVersion) is the one
// act that is an agent with tools, through the Conversation port
// (conversation.go). It is shown what the Briefings over Spans written since
// the last read told (the storage adapter's BriefingsWritten; the first
// read, the week before it): each Briefing's statements standing, each with
// the Spans it cites, each Span shown once by a ref (P1, P2… numbered as
// shown, across the run), its Item's title and date, its speaker and the
// Entities it names; with nothing told, it is written empty, with no call.
// Its tools only read: story (a Span's latest gathering, by the Briefing or
// a read, and its relations judged since, or all of them; LatestGathering,
// RelationsOf), spans_naming (the Spans naming an Entity a Span shown
// names, the latest Items first, back through time with before, from
// SpansMentioning), assessments (earlier Assessments, A and the event id,
// each with the Spans and Assessments it rests on, none under wrong),
// wikidata_search and wikidata_item (each lookup L1, L2…). The agent submits
// through submit_read: assessments (N1, N2…, each resting on Spans shown, at
// least one, and earlier Assessments shown, superseding at most one current
// one shown) and statements (each citing P, A, N and L refs, and the stories
// it ties together: a cited Span's Story, of Spans shown its relations
// reach, gathered as the Briefing gathers them, kept as gatherings with
// read_id).
//
// The act runs its own loop (bounded by SundayReadMaxTurns and
// SundayReadTokenBudget), and the check refuses a statement citing nothing,
// a ref the run was not shown and the read does not make, an Assessment
// citing no Span shown, a supersession of one not shown or already
// superseded, or a Story holding a Span its relations don't reach, and tells
// the agent why. A read is kept (KeepSundayRead) as events: the read with
// its Judge, each Assessment with what it rests on, each statement with what
// it cites, and each Story it gathered; a read written before it told Spans
// keeps only its day. Schedule writes it
// on Sundays just after the Briefing (ScheduleDeps.SundayRead), retried
// RetrySundayRead apart at most SundayReadTries times. Each attempt's
// conversation is kept turn by turn (KeptConversation, with the read or its
// failure, in one transaction): each turn keeps only the messages new with
// its request (the conversation's prefix isn't kept again), the turn as
// returned (a refusal's too), its usage and what came of it (continued,
// applied, refused with the reason told back, failed), and
// ConversationRequestHash of the whole request, the conversation counterpart
// of RequestHash. Feedback on the read (new to me) and its Assessments
// (wrong, new to me) is kept as judgements (internal/feedback).
//
// The old design (Facts and Claims drawn from each Item, Stories,
// Developments and Connections, the Briefings and Sunday reads over them and
// the Feedback on them) is gone from the code and the Record (system design:
// The Record became events by dropping the old design's judgements).
package editor
