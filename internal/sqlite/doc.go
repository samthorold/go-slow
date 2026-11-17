// Package sqlite is the storage adapter: the Record kept in a SQLite file,
// with its migrations (migrations/, applied in name order by Open).
//
// # The event store
//
// Its event store (0023_events.sql, cuts.go) is the Record, beside the Items
// and their text, the Sources and Topics, and the side records (kept calls
// and conversations, pending batches and work attempts; and, read by
// nothing now, the days a Briefing or Sunday read was written or failed,
// opens and commits). Each event is a row
// of events (its identifier id, its kind, the version of its shape shape,
// and at), with its detail at its own grain in its kind's tables keyed by
// the event's id (a Cut: cuts, naming the Item and its cutter, cutter and
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
// Store.Switch (switch.go: when it was, At, and ItemsThrough).
//
// 0036_stories_only.sql retires the old design's acts (relating, the
// Briefing, the Sunday read, gatherings, Feedback): their tables and events
// stay, read by no view, and their kept calls and conversations stay
// readable (Call, Conversation). It keeps the switch to Stories, an event of
// kind switch, shape 2 (story_switch.through_cut: the newest Cut then, or a
// Record already woven, its last run's), which StoriesToWeave reads; none
// for a Record that held no Cut. The Record's switch to Stories is its newest
// (storySwitch in switch.go), so KeepStorySwitch moves it back, through
// CutBefore a day, by keeping another, rewriting none.
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
// reading, or weaving's that answered a run written, which a run's estimate
// scales from.
//
// # Look-ups
//
// A look-up is an event of kind lookup (lookups); with no match it may make
// the Editor's own Entity (an entity event, editor_entities), and with a
// match a merge event (entity_merges). NamesToLookUp is every reading's
// names with no look-up; EntitiesNamed each key's newest look-up.
//
// # Weaving
//
// A weaving run whose result was written (0033_weaving.sql, weavings.go) is
// an event of kind weaving (weavings: its day as work_key, through_cut, the
// newest Cut it was shown, and its Judge), and each Story it made an event of
// kind story (woven_stories), holding its Editor's Spans (editor_spans, with
// their words) and what each cites: a Source's Span (editor_span_cites, an
// address into its Item's text) or an earlier Editor's Span
// (editor_span_cites_editor, 0034: its Story and place, its words read from
// the Story that wrote it), the two sharing the order cited, m.
// StoriesToWeave is every Cut after the newest through_cut and the switch
// to Stories, of Items collected after the switch (StoriesCutAfter, after a
// Cut given); Woven whether any run is written; StoriesMade and StoryMade the Stories made,
// each with its call (the applied weaving call on its run's day answering
// its request hash). A kept call and a pending request also keep the effort
// asked for (effort, ” for none).
//
// A Story's Name (0035_naming.sql, namings.go) is an event of kind
// story_name (story_names: the Story, its words and its Judge; kind name is
// a reading's). StoriesMade and StoryMade give each Story its newest Name and
// the applied naming call on the Story's work (keyed by the Story's event)
// answering its request hash. StoriesToName is every Story made with none,
// whose naming is not marked failed; FailedWork gives a failed naming its
// Story and first Editor's Span.
//
// # Steering
//
// SourceCounts
// (steering.go) is what the Owner steers by, per Source over steeringWindow,
// the last 28 days: the Stories the Editor made holding a Span of one of the
// Source's Items collected in it, touching a Topic when one of their Spans
// does (storyTouches: an Editor's Span, by its own words, or a Span it
// cites), and the Spans of its Items collected in it, by their newest live
// readings. StoriesTouching (touching.go) is a Topic's Page, by the same
// test.
package sqlite
