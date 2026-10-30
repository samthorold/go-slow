-- The Record as events (system design: The Record as events). What was
-- observed or judged is kept as an event, never changed: one row in events,
-- giving it its identifier, its kind and the version of its shape, and its
-- detail at its own grain in the kind's own tables. A view reads every shape
-- of an event it has ever been given, so a shape that changes is a new
-- version beside the old, never a rewrite. Text is not kept in events: they
-- address an Item's text, which stays in items, so it can be forgotten
-- without rewriting an event.
CREATE TABLE events (
    id INTEGER PRIMARY KEY,
    kind TEXT NOT NULL,
    shape INTEGER NOT NULL,
    at TEXT NOT NULL -- when it was observed or judged, RFC 3339 UTC
) STRICT;

-- A Cut (kind 'cut'): the Judgement listing an Item's Spans, with its Judge,
-- the cutter and its version.
CREATE TABLE cuts (
    event_id INTEGER PRIMARY KEY REFERENCES events (id),
    source_id TEXT NOT NULL,
    entry_id TEXT NOT NULL,
    cutter TEXT NOT NULL,
    cutter_version INTEGER NOT NULL,
    FOREIGN KEY (source_id, entry_id) REFERENCES items (source_id, entry_id)
) STRICT;

CREATE INDEX cuts_by_item ON cuts (source_id, entry_id, event_id);

-- The Spans a Cut lists, numbered from 1 in order. A Span is an address:
-- byte offsets into its Item's text as kept, start inclusive and end
-- exclusive, with no identity of its own.
CREATE TABLE cut_spans (
    cut_id INTEGER NOT NULL REFERENCES cuts (event_id),
    n INTEGER NOT NULL,
    span_start INTEGER NOT NULL,
    span_end INTEGER NOT NULL,
    PRIMARY KEY (cut_id, n),
    CHECK (0 <= span_start AND span_start < span_end)
) STRICT;

CREATE TRIGGER events_not_rewritten BEFORE UPDATE ON events
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER events_not_removed BEFORE DELETE ON events
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

CREATE TRIGGER cuts_not_rewritten BEFORE UPDATE ON cuts
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER cuts_not_removed BEFORE DELETE ON cuts
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

CREATE TRIGGER cut_spans_not_rewritten BEFORE UPDATE ON cut_spans
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER cut_spans_not_removed BEFORE DELETE ON cut_spans
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;
