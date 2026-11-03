// Package sqlite is the storage adapter: the Record kept in a SQLite file,
// with its migrations (migrations/, applied in name order by Open).
//
// # The event store
//
// Its event store (0023_events.sql, cuts.go) is the Record, beside the Items
// and their text, the Sources and Topics, and the side records (kept calls
// and conversations, pending batches and work attempts, the days a Briefing
// or Sunday read was written or failed, opens, commits). Each event is a row
// of events (its identifier id, its kind, the version of its shape shape,
// and at), with its detail at its own grain in its kind's tables keyed by
// the event's id (a Cut: cuts, naming the Item and its Judge, cutter and
// cutter_version, and cut_spans, the Spans numbered n from 1 as
// span_start/span_end). Triggers refuse every update and delete on them:
// events are append-only, and a view reads every shape it has ever been
// given. Events hold no text: they address the Item's text, which stays in
// items, so it can be forgotten without rewriting an event. A later kind of
// event adds its own tables, keyed the same way, and its own triggers.
//
// 0032_events_only.sql dropped the old design's tables and keeps the switch,
// an event of kind switch (record_switch.items_through, the newest Item's
// collected_at then; none for a Record that held no Items), read by
// Store.Switch (switch.go: when it was, At, and ItemsThrough), and by the
// register, whose falling parts read only windows wholly after it.
//
// # Readings and judges
//
// A reading (0024_reading_spans.sql, readings.go) is an event of kind
// reading (readings: its Item, the Cut it was shown, and its Judge, act,
// version, model, request_hash), and each judgement it made an event of its
// own tied to it by reading_id (kinds refers_to, name, speaker, rejected;
// tables refers_to, span_names, speakers, rejections), every address byte
// offsets into the reading's own Item, so KeepReading refuses a judgement
// addressing another Item and keeps nothing. ItemsToRead is every Item
// collected after the switch, cut and not yet read by a live judge, with its
// newest Cut (those collected through it are read only by rejudge);
// NewestReading the reading of an Item a view reads, with its judgements:
// the newest live judge's (newestLiveReading in judges.go, the one subquery
// every view of readings goes through, the Items list's included).
//
// Candidate judges (0026_candidates.sql, judges.go) are events of kind
// candidate (candidates: act, version, model and the range), each reading a
// candidate made tied to it in candidate_readings, written with the reading
// (Reading.Candidate); choosing one is an event of kind choice
// (candidate_choices), rewriting nothing. A reading is live if no candidate
// made it or its candidate was chosen, and a chosen candidate's readings
// count as made at the choice, so the choice outranks every live reading
// before it and a live reading after it outranks the choice. ItemsIn is the
// Items cut and published in a range, read or not, for judging again;
// PastUsage the size and tokens of reading's applied calls that answered a
// reading, which a run's estimate scales from. judges (register.go) lists
// each live Judge as it began judging, for the register.
//
// # Look-ups
//
// A look-up is an event of kind lookup (lookups); with no match it may make
// the Editor's own Entity (an entity event, editor_entities), and with a
// match a merge event (entity_merges). NamesToLookUp is every reading's
// names with no look-up; EntitiesNamed each key's newest look-up not under
// wrong.
//
// # Relatings
//
// A relating (0027_relating.sql, relating.go) is an event of kind relating
// (relatings: its Item, the reading its Spans were shown as, and its Judge;
// model and request hash empty for one made with no call), and each relation
// it judged an event of kind relation (relations: its kind, follows,
// same_as or contradicts, and two addresses, from the later-published Span
// to the earlier, of two different Items, one of them the relating's own,
// which KeepRelating checks). ItemsToRelate is every Item read, not yet
// related and not failed whose reading's names all have a look-up, in the
// order Items were first read (firstRead), each with its Spans not Rejected
// and the Entities each mentions; SpansMentioning is the Spans not Rejected
// of Items first read before a given reading that mention one of the
// Entities given (the Record-wide "Spans mentioning an Entity", worked out
// from readings and look-ups, never stored), each with its Item, hydrated
// words, speaker and Entities; ItemsMentioning how many Items read by then
// mention each; RelationsOf a Span's relations either way, each with the
// Span at its other end, its Judge and its call.
//
// # Briefings and Sunday reads
//
// A Briefing over Spans (0028_briefing_spans.sql) is kept as events: the
// Briefing with its Judge (told_briefings, its day also in briefings, which
// lists the days of either shape), each statement (told_statements) with the
// Spans it cites and the reading each was shown as (told_citations), and
// each Story gathered (gatherings, gathered_spans). LatestGathering is a
// Span's latest gathering; BriefingTrail a Briefing's calls, each cited
// Span's reading call (told_citations.reading_id) and the relating calls on
// its Item before it. A Sunday read over Spans (0031_sunday_read_spans.sql)
// is kept the same way: the read with its Judge (told_reads, its day, turns
// and tokens also in sunday_reads, which lists the days of either shape),
// each Assessment (told_assessments, what it rests on in
// assessment_grounds), each statement (told_read_statements, what it cites
// in told_read_citations) and each Story it gathered (gatherings with
// read_id).
//
// # Feedback and steering
//
// Feedback is kept as events by pieces.go (0029_feedback_events.sql); every
// view works out what it reads from the Feedback standing (wrongStanding),
// and givenFeedback gives the register each piece by what it was given on.
// precedents.go selects what a judge is shown as precedent
// (StandingWrongOnRelations, StandingTaste) and judgement_precedents
// (0030_judgement_precedents.sql) keeps which it was shown. SourceCounts
// (steering.go) is what the Owner steers by, per Source over steeringWindow,
// the last 28 days: the Stories gathered in it holding a Span of one of the
// Source's Items (latest gathering per Span gathered from), touching a Topic
// when one of their Spans does, and the Spans of its Items collected in it,
// by their newest live readings.
package sqlite
